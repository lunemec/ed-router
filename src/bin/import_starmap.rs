use indicatif::{ProgressBar, ProgressStyle};
use rayon::prelude::*;
use rusqlite::Connection;
use serde::Deserialize;
use std::env;
use std::fs::File;
use std::io::{BufRead, BufReader};
use std::process;
use std::sync::mpsc::{sync_channel, Receiver, SyncSender};
use std::thread;
use std::time::Instant;

// --- Configuration ---
const BATCH_SIZE: usize = 50000; // Number of lines to read and parse at a time

// --- JSON Data Structures ---

#[derive(Deserialize, Debug)]
struct SystemCoords {
    x: f64,
    y: f64,
    z: f64,
}

#[derive(Deserialize, Debug)]
#[serde(rename_all = "camelCase")]
struct Body {
    r#type: String,
    sub_type: Option<String>,
    // If distanceToArrival is missing, serde will use the default value (0.0).
    #[serde(default)]
    distance_to_arrival: f64,
    // If mainStar is missing, serde will use the default value (false).
    #[serde(default)]
    main_star: bool,
}

#[derive(Deserialize, Debug)]
struct FullSystemData {
    id64: i64,
    name: String,
    coords: SystemCoords,
    bodies: Vec<Body>,
}

// --- Intermediate Structs for the Pipeline (maps directly to DB schema) ---
#[derive(Debug)]
struct StarToWrite {
    sub_type: Option<String>,
    distance_ls: f64,
}

#[derive(Debug)]
struct SystemToWrite {
    id: i64,
    name: String,
    x: f64,
    y: f64,
    z: f64,
    stars: Vec<StarToWrite>,
}

#[derive(Debug, Default)]
struct Stats {
    total_systems: u64,
    scoopable_count: u64,
    neutron_count: u64,
    wd_count: u64,
}

// --- Main Application Logic ---

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let args: Vec<String> = env::args().collect();
    if args.len() < 3 {
        eprintln!("Error: Missing file path arguments.");
        eprintln!(
            "Usage: {} <path_to_galaxy.json.gz> <output_db_path.sqlite>",
            args[0]
        );
        process::exit(1);
    }
    let edsm_dump_filename = args[1].clone();
    let db_filename = &args[2].clone();

    if std::path::Path::new(db_filename).exists() {
        println!(
            "Database '{}' already exists. Skipping import.",
            db_filename
        );
        return Ok(());
    }

    let db = setup_database(db_filename)?;
    let start_time = Instant::now();

    // Create channels for our pipeline:
    // 1. Reader thread sends batches of lines to the parser.
    // 2. Parser threads send batches of processed structs to the writer.
    let (line_batch_tx, line_batch_rx) = sync_channel::<Vec<String>>(4);
    let (struct_batch_tx, struct_batch_rx) = sync_channel::<Vec<SystemToWrite>>(4);

    let reader_handle = thread::spawn(move || {
        read_file_in_batches(edsm_dump_filename, line_batch_tx).expect("File reader failed");
    });

    let parser_handle = thread::spawn(move || {
        for line_batch in line_batch_rx {
            let parsed_batch: Vec<SystemToWrite> = line_batch
                .par_iter()
                .filter_map(|line| parse_line(line))
                .collect();

            if !parsed_batch.is_empty() {
                struct_batch_tx.send(parsed_batch).unwrap();
            }
        }
        drop(struct_batch_tx);
    });

    let writer_handle =
        thread::spawn(move || write_to_db(struct_batch_rx, db).expect("DB writer failed"));

    // Wait for all stages to complete
    reader_handle.join().unwrap();
    parser_handle.join().unwrap();
    let final_stats = writer_handle.join().unwrap();

    println!("\nData import phase complete. Now creating database indexes...");
    // Print the final statistics summary.
    println!(
            "\n[Importer] Loaded and processed a total of {} systems (Scoopable: {}, Neutron: {}, White Dwarf: {}).",
            final_stats.total_systems,
            final_stats.scoopable_count,
            final_stats.neutron_count,
            final_stats.wd_count
        );
    let index_start_time = Instant::now();
    let conn = Connection::open(db_filename)?;
    create_indexes(&conn)?;

    println!(
        "Indexes created successfully in {:.2?}.",
        index_start_time.elapsed()
    );

    println!(
        "\nImport process fully complete in {:.2?}.",
        start_time.elapsed()
    );
    Ok(())
}

fn setup_database(path: &str) -> Result<Connection, rusqlite::Error> {
    println!("Creating SQLite database: {}", path);
    let conn = Connection::open(path)?;
    conn.execute_batch(
        "PRAGMA journal_mode = WAL;
         PRAGMA synchronous = OFF;",
    )?;

    conn.execute_batch(
        "CREATE TABLE systems (
            id INTEGER PRIMARY KEY,
            name TEXT NOT NULL,
            x REAL NOT NULL,
            y REAL NOT NULL,
            z REAL NOT NULL
        );
        CREATE TABLE stars (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            system_id INTEGER NOT NULL,
            sub_type TEXT,
            distance_ls REAL NOT NULL,
            FOREIGN KEY (system_id) REFERENCES systems (id)
        );",
    )?;
    Ok(conn)
}

fn create_indexes(conn: &Connection) -> Result<(), rusqlite::Error> {
    println!("Step 1/2: Creating standard indexes...");
    conn.execute_batch(
        "CREATE INDEX idx_systems_name ON systems (name);
         CREATE INDEX idx_stars_system_id ON stars (system_id);
         CREATE INDEX idx_stars_sub_type ON stars (sub_type);",
    )?;

    println!("Step 2/2: Creating FTS index for fast name search...");
    conn.execute_batch(
        "CREATE VIRTUAL TABLE systems_fts USING fts5(name, content='systems', content_rowid='id');
         INSERT INTO systems_fts(rowid, name) SELECT id, name FROM systems;",
    )?;
    Ok(())
}

fn read_file_in_batches(file_path: String, tx: SyncSender<Vec<String>>) -> std::io::Result<()> {
    let file = File::open(&file_path)?;
    let file_size = file.metadata()?.len();

    // The progress bar now tracks the total size of the compressed file.
    let pb = ProgressBar::new(file_size);
    pb.set_style(
          ProgressStyle::default_bar()
              .template(
                  "{spinner:.green} [{elapsed_precise}] [{bar:40.cyan/blue}] {bytes}/{total_bytes} ({eta})",
              )
              .expect("Progress bar template is invalid"),
      );

    let progress_reader = pb.wrap_read(file);
    let gz = flate2::read::GzDecoder::new(progress_reader);
    let reader = BufReader::new(gz);

    let mut batch = Vec::with_capacity(BATCH_SIZE);
    for line in reader.lines() {
        batch.push(line?);
        if batch.len() >= BATCH_SIZE {
            tx.send(batch).unwrap();
            batch = Vec::with_capacity(BATCH_SIZE);
        }
    }

    if !batch.is_empty() {
        tx.send(batch).unwrap();
    }

    // finish() is better here than finish_with_message() as the bar will already be at 100%.
    pb.finish();
    println!("File reading complete.");

    Ok(())
}

fn parse_line(line: &str) -> Option<SystemToWrite> {
    let trimmed_line = line.trim();
    // This check robustly handles the junk lines at the start and end of the file.
    if trimmed_line == "[" || trimmed_line == "]" || trimmed_line.is_empty() {
        return None;
    }
    let trimmed_json = trimmed_line.trim_end_matches(',');

    match serde_json::from_str::<FullSystemData>(trimmed_json) {
        Ok(data) => {
            if data.id64 == 0 || data.name.is_empty() {
                return None;
            }

            let mut stars_to_write = Vec::new();

            // First, try to find the primary star using the `mainStar` flag.
            let primary_star_index = data.bodies.iter().position(|b| b.main_star);

            // If no star has the flag, fall back to finding the star at 0.0 ls.
            let primary_star_index = primary_star_index.or_else(|| {
                data.bodies
                    .iter()
                    .position(|b| b.r#type == "Star" && b.distance_to_arrival == 0.0)
            });

            // Process all star bodies.
            for (i, body) in data
                .bodies
                .iter()
                .filter(|b| b.r#type == "Star")
                .enumerate()
            {
                // If this star is the primary star we identified, add it with 0.0 ls distance.
                if Some(i) == primary_star_index {
                    stars_to_write.push(StarToWrite {
                        sub_type: body.sub_type.clone(),
                        distance_ls: 0.0,
                    });
                } else {
                    // Otherwise, add it as a secondary star with its given distance.
                    stars_to_write.push(StarToWrite {
                        sub_type: body.sub_type.clone(),
                        distance_ls: body.distance_to_arrival,
                    });
                }
            }
            if stars_to_write.len() == 0 {
                return None;
            }

            Some(SystemToWrite {
                id: data.id64,
                name: data.name,
                x: data.coords.x,
                y: data.coords.y,
                z: data.coords.z,
                stars: stars_to_write,
            })
        }
        Err(e) => {
            // If parsing fails, check if this is a line we care about for debugging.
            eprintln!("\n--- FAILED TO PARSE IMPORTANT SYSTEM ---");
            eprintln!("Serde Error: {}", e);
            eprintln!("Original Line (first 200 chars): {:.200}", line);
            eprintln!("-----------------------------------------\n");
            None
        }
    }
}

fn write_to_db(
    rx: Receiver<Vec<SystemToWrite>>,
    mut conn: Connection,
) -> Result<Stats, rusqlite::Error> {
    let mut stats = Stats::default();
    let tx = conn.transaction()?;
    {
        let mut system_stmt =
            tx.prepare("INSERT OR IGNORE INTO systems (id, name, x, y, z) VALUES (?, ?, ?, ?, ?)")?;
        let mut star_stmt =
            tx.prepare("INSERT INTO stars (system_id, sub_type, distance_ls) VALUES (?, ?, ?)")?;

        for batch in rx {
            for system in &batch {
                system_stmt.execute((system.id, &system.name, system.x, system.y, system.z))?;

                for star in &system.stars {
                    star_stmt.execute((system.id, &star.sub_type, star.distance_ls))?;

                    // Tally the stats for this star
                    if let Some(sub_type) = &star.sub_type {
                        if sub_type.contains("Neutron Star") {
                            stats.neutron_count += 1;
                        }
                        if sub_type.contains("White Dwarf") {
                            stats.wd_count += 1;
                        }
                        if is_scoopable(sub_type) {
                            stats.scoopable_count += 1;
                        }
                    }
                }
            }
            stats.total_systems += batch.len() as u64;
        }
    }
    tx.commit()?;
    Ok(stats)
}

fn is_scoopable(sub_type: &str) -> bool {
    sub_type.starts_with('O')
        || sub_type.starts_with('B')
        || sub_type.starts_with('A')
        || sub_type.starts_with('F')
        || sub_type.starts_with('G')
        || sub_type.starts_with('K')
        || sub_type.starts_with('M')
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_line_valid() {
        let json_line = r#"{"id64":128,"name":"Sol","coords":{"x":0,"y":0,"z":0},"bodies":[{"type":"Star","subType":"G 2 V","distanceToArrival":0},{"type":"Planet","subType":null,"distanceToArrival":1500}]}"#;
        let result = parse_line(json_line).unwrap();
        assert_eq!(result.id, 128);
        assert_eq!(result.name, "Sol");
        assert_eq!(result.x, 0.0);
        assert_eq!(result.stars.len(), 1);
        assert_eq!(result.stars[0].sub_type, Some("G 2 V".to_string()));
        assert_eq!(result.stars[0].distance_ls, 0.0);
    }

    #[test]
    fn test_parse_line_no_stars() {
        let json_line = r#"{"id64":129,"name":"Proxima Centauri","coords":{"x":1,"y":1,"z":1},"bodies":[{"type":"Planet","subType":null,"distanceToArrival":1500}]}"#;
        let result = parse_line(json_line);
        assert!(result.is_none());
    }

    #[test]
    fn test_parse_robustness_with_null_subtype() {
        let json_line = r#"{"id64":130,"name":"Sirius","coords":{"x":2,"y":2,"z":2},"bodies":[{"type":"Star","subType":null,"distanceToArrival":0}]}"#;
        let result = parse_line(json_line).unwrap();
        assert_eq!(result.stars.len(), 1);
        assert!(result.stars[0].sub_type.is_none());
    }

    #[test]
    fn test_main_star_flag_is_primary() {
        let json_line = r#"{"id64":123,"name":"MainStarTest","coords":{"x":0,"y":0,"z":0},"bodies":[{"type":"Star","subType":"Secondary Star","distanceToArrival":10.0,"mainStar":false},{"type":"Star","subType":"Primary Star","distanceToArrival":0.0,"mainStar":true}]}"#;
        let result = parse_line(json_line).unwrap();

        assert_eq!(result.stars.len(), 2);
        // Find the star that was designated primary
        let primary = result.stars.iter().find(|s| s.distance_ls == 0.0).unwrap();
        assert_eq!(
            primary.sub_type,
            Some("Primary Star".to_string()),
            "The star with mainStar:true should be the primary, with distance 0.0"
        );
    }

    #[test]
    fn test_parse_body_missing_distance() {
        let json_line = r#"{"id64":456,"name":"MissingDistanceTest","coords":{"x":1,"y":1,"z":1},"bodies":[{"type":"Star","subType":"A Star","mainStar":true},{"type":"Planet","subType":"Rocky body"}]}"#;
        let result = parse_line(json_line);
        assert!(
            result.is_some(),
            "System should parse even if a body is missing distanceToArrival"
        );
        let system = result.unwrap();
        assert_eq!(system.stars.len(), 1);
        assert_eq!(system.stars[0].distance_ls, 0.0);
        assert_eq!(system.stars[0].sub_type, Some("A Star".to_string()));
    }

    #[test]
    fn test_db_writing_logic() -> Result<(), Box<dyn std::error::Error>> {
        let temp_dir = std::env::temp_dir();
        let db_path = temp_dir.join("test_db_writing_logic.sqlite");

        if db_path.exists() {
            std::fs::remove_file(&db_path)?;
        }

        let batch = vec![SystemToWrite {
            id: 101,
            name: "Test DB System".to_string(),
            x: 10.0,
            y: 20.0,
            z: 30.0,
            stars: vec![
                StarToWrite {
                    sub_type: Some("K star".to_string()),
                    distance_ls: 0.0,
                },
                StarToWrite {
                    sub_type: Some("Neutron Star".to_string()),
                    distance_ls: 150.0,
                },
            ],
        }];

        {
            let conn = setup_database(db_path.to_str().unwrap())?;
            let (tx, rx) = sync_channel(1);
            tx.send(batch).unwrap();
            drop(tx);
            write_to_db(rx, conn)?;
        }

        {
            let verify_conn = Connection::open(&db_path)?;

            let mut stmt = verify_conn.prepare("SELECT name, x FROM systems WHERE id = 101")?;
            let mut rows = stmt.query([])?;
            if let Some(row) = rows.next()? {
                let name: String = row.get(0)?;
                let x: f64 = row.get(1)?;
                assert_eq!(name, "Test DB System");
                assert_eq!(x, 10.0);
            } else {
                panic!("System with ID 101 not found");
            }

            let mut stmt = verify_conn
                .prepare("SELECT sub_type, distance_ls FROM stars WHERE system_id = 101 ORDER BY distance_ls")?;
            let stars: Vec<(String, f64)> = stmt
                .query_map([], |row| Ok((row.get(0)?, row.get(1)?)))?
                .collect::<Result<Vec<_>, _>>()?;

            assert_eq!(stars.len(), 2);
            assert_eq!(stars[0].0, "K star");
            assert_eq!(stars[0].1, 0.0);
            assert_eq!(stars[1].0, "Neutron Star");
            assert_eq!(stars[1].1, 150.0);
        }

        std::fs::remove_file(&db_path)?;
        Ok(())
    }
}
