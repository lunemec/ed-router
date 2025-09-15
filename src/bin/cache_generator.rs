use indicatif::{ProgressBar, ProgressStyle};
use rkyv::ser::{
    serializers::{AllocSerializer, CompositeSerializer, WriteSerializer},
    Serializer,
};
use rusqlite::{Connection, Result};
use std::env;
use std::fs::File;
use std::io::{BufWriter, Write};
use std::process;

// Use the data structures from our shared library, 'ed_router'.
use ed_router::cache::{CacheStats, FullCacheData, SecondaryStar, StarFlags, SystemPoint};

// Represents a single row from the database LEFT JOIN query.
struct SystemAndStarRow {
    system_id: i64,
    x: f64,
    y: f64,
    z: f64,
    sub_type: Option<String>,
    distance_ls: Option<f64>,
}

// Holds the result of processing the raw database data.
#[derive(Clone)]
struct ProcessedData {
    points: Vec<SystemPoint>,
    scoopable_count: u64,
    neutron_count: u64,
    wd_count: u64,
}

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let args: Vec<String> = env::args().collect();
    if args.len() < 3 {
        eprintln!("Error: Missing file path arguments.");
        eprintln!(
            "Usage: {} <input_db_path.sqlite> <output_cache_path.rkyv>",
            args[0]
        );
        process::exit(1);
    }
    let db_path = &args[1];
    let cache_path = &args[2];

    let start_time = std::time::Instant::now();

    let conn = Connection::open(db_path)?;

    println!("[Cache Gen] Loading and processing star data...");
    let processed_data = load_and_process_data(&conn)?;

    println!(
        "[Cache Gen] Loaded and processed a total of {} systems (Scoopable: {}, Neutron: {}, White Dwarf: {}).",
        processed_data.points.len(),
        processed_data.scoopable_count,
        processed_data.neutron_count,
        processed_data.wd_count
    );

    write_cache(cache_path, processed_data)?;

    println!("[Cache Gen] Total time: {:?}", start_time.elapsed());

    Ok(())
}

/// Queries the database and passes the results to the processor.
fn load_and_process_data(conn: &Connection) -> Result<ProcessedData, rusqlite::Error> {
    let total_systems: u64 =
        conn.query_row("SELECT COUNT(*) FROM systems", [], |row| row.get(0))?;

    let mut stmt = conn.prepare(
        "SELECT s.id, s.x, s.y, s.z, st.sub_type, st.distance_ls
         FROM systems s
         LEFT JOIN stars st ON s.id = st.system_id
         ORDER BY s.id",
    )?;

    let rows_iterator = stmt.query_map([], |row| {
        Ok(SystemAndStarRow {
            system_id: row.get(0)?,
            x: row.get(1)?,
            y: row.get(2)?,
            z: row.get(3)?,
            sub_type: row.get(4)?,
            distance_ls: row.get(5)?,
        })
    })?;

    process_data(rows_iterator, total_systems)
}

/// Processes a stream of raw database rows into a structured `ProcessedData` object.
fn process_data(
    data: impl Iterator<Item = Result<SystemAndStarRow, rusqlite::Error>>,
    total_systems: u64,
) -> Result<ProcessedData, rusqlite::Error> {
    let pb = ProgressBar::new(total_systems);
    pb.set_style(
        ProgressStyle::default_bar()
            .template(
                "{spinner:.green} [{elapsed_precise}] [{bar:40.cyan/blue}] {pos}/{len} systems",
            )
            .unwrap(),
    );
    let mut points = Vec::new();
    let mut idx_counter = 0;
    let mut scoopable_count = 0;
    let mut neutron_count = 0;
    let mut wd_count = 0;

    let mut last_system_id = -1;
    let mut current_point: Option<SystemPoint> = None;

    for row_result in data {
        let row = row_result?;
        let system_id = row.system_id;

        if system_id != last_system_id {
            if let Some(point) = current_point.take() {
                points.push(point);
                pb.inc(1);
            }
            last_system_id = system_id;
            current_point = Some(SystemPoint {
                id: system_id,
                idx: idx_counter,
                x: row.x,
                y: row.y,
                z: row.z,
                flags: 0,
                secondary_stars: Vec::new(),
            });
            idx_counter += 1;
        }

        if let (Some(point), Some(sub_type_str)) = (&mut current_point, &row.sub_type) {
            let distance_ls = row.distance_ls.unwrap_or(0.0) as f32;
            let mut star_flags = 0u8;

            if sub_type_str.starts_with("O ")
                || sub_type_str.starts_with("B ")
                || sub_type_str.starts_with("A ")
                || sub_type_str.starts_with("F ")
                || sub_type_str.starts_with("G ")
                || sub_type_str.starts_with("K ")
                || sub_type_str.starts_with("M ")
            {
                star_flags |= StarFlags::Scoopable as u8;
                scoopable_count += 1;
            }
            if sub_type_str.contains("Neutron Star") {
                star_flags |= StarFlags::Neutron as u8;
                neutron_count += 1;
            }
            if sub_type_str.contains("White Dwarf") {
                star_flags |= StarFlags::WhiteDwarf as u8;
                wd_count += 1;
            }

            if star_flags != 0 {
                if distance_ls == 0.0 {
                    // This is a primary star. OR its flags with the main flags.
                    point.flags |= star_flags;
                } else {
                    // This is a secondary star. Add it to the vector.
                    point.secondary_stars.push(SecondaryStar {
                        distance_ls,
                        flags: star_flags,
                    });
                }
            }
        }
    }

    if let Some(point) = current_point.take() {
        points.push(point);
        pb.inc(1);
    }
    pb.finish_with_message("System processing complete.");
    Ok(ProcessedData {
        points,
        scoopable_count,
        neutron_count,
        wd_count,
    })
}

/// Serializes the processed points to the rkyv cache file.
fn write_cache(path: &str, data: ProcessedData) -> Result<(), Box<dyn std::error::Error>> {
    let stats = CacheStats {
        scoopable_count: data.scoopable_count,
        neutron_count: data.neutron_count,
        wd_count: data.wd_count,
    };
    let cache_data = FullCacheData {
        points: data.points,
        stats,
    };
    println!("[Cache Gen] Data prepared. Serializing to disk...");

    let file = File::create(path)?;
    let mut writer = BufWriter::new(file);
    let mut serializer = CompositeSerializer::new(
        WriteSerializer::new(&mut writer),
        AllocSerializer::<536870912>::default(), // 512 MB scratch space
        rkyv::Infallible,
    );

    serializer
        .serialize_value(&cache_data)
        .map_err(|e| format!("Serialization error: {}", e))?;

    writer.flush()?;

    println!(
        "[Cache Gen] Successfully created R-Tree cache file at '{}'",
        path
    );
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use rusqlite::Connection;
    use std::io::Read;

    #[test]
    fn test_process_data() {
        // 1. Create test data (includes a primary star at 0.0 ls for system 1)
        let test_data = vec![
            // System 1: primary is scoopable
            Ok(SystemAndStarRow {
                system_id: 1,
                x: 1.0,
                y: 1.0,
                z: 1.0,
                sub_type: Some("G 2 V".to_string()),
                distance_ls: Some(0.0),
            }),
            // System 2: one neutron, one white dwarf (both secondary)
            Ok(SystemAndStarRow {
                system_id: 2,
                x: 2.0,
                y: 2.0,
                z: 2.0,
                sub_type: Some("Neutron Star".to_string()),
                distance_ls: Some(200.0),
            }),
            Ok(SystemAndStarRow {
                system_id: 2,
                x: 2.0,
                y: 2.0,
                z: 2.0,
                sub_type: Some("DA White Dwarf".to_string()),
                distance_ls: Some(300.0),
            }),
            // System 3: no stars
            Ok(SystemAndStarRow {
                system_id: 3,
                x: 3.0,
                y: 3.0,
                z: 3.0,
                sub_type: None,
                distance_ls: None,
            }),
            // System 4: two scoopable stars (both secondary)
            Ok(SystemAndStarRow {
                system_id: 4,
                x: 4.0,
                y: 4.0,
                z: 4.0,
                sub_type: Some("M 5 VI".to_string()),
                distance_ls: Some(50.0),
            }),
            Ok(SystemAndStarRow {
                system_id: 4,
                x: 4.0,
                y: 4.0,
                z: 4.0,
                sub_type: Some("K 1 IV".to_string()),
                distance_ls: Some(60.0),
            }),
        ];

        // 2. Run the function
        let count = test_data.len() as u64;
        let result = process_data(test_data.into_iter(), count).unwrap();

        // 3. Assert top-level results
        assert_eq!(result.points.len(), 4);
        assert_eq!(result.scoopable_count, 3);
        assert_eq!(result.neutron_count, 1);
        assert_eq!(result.wd_count, 1);

        // --- UPDATED ASSERTIONS ---

        // Assert details for system 2
        let point2 = result.points.iter().find(|p| p.id == 2).unwrap();
        assert_eq!(
            point2.flags, 0,
            "System 2's primary star should have no flags"
        );
        assert_eq!(point2.secondary_stars.len(), 2);

        let neutron_star = point2
            .secondary_stars
            .iter()
            .find(|s| s.flags == StarFlags::Neutron as u8)
            .unwrap();
        assert_eq!(neutron_star.distance_ls, 200.0);

        let wd_star = point2
            .secondary_stars
            .iter()
            .find(|s| s.flags == StarFlags::WhiteDwarf as u8)
            .unwrap();
        assert_eq!(wd_star.distance_ls, 300.0);

        // Assert details for system 4
        let point4 = result.points.iter().find(|p| p.id == 4).unwrap();
        assert_eq!(
            point4.flags, 0,
            "System 4's primary star should have no flags"
        );
        assert_eq!(point4.secondary_stars.len(), 2);
        // You could assert more details here if needed, like finding the star with distance 50.0
    }

    #[test]
    fn test_load_and_process_data() {
        // 1. Setup in-memory DB
        let conn = Connection::open_in_memory().unwrap();

        // 2. Create schema
        conn.execute(
            "CREATE TABLE systems (id INTEGER PRIMARY KEY, x REAL, y REAL, z REAL)",
            [],
        )
        .unwrap();
        conn.execute(
            "CREATE TABLE stars (id INTEGER PRIMARY KEY, system_id INTEGER, sub_type TEXT, distance_ls REAL)",
            [],
        ).unwrap();

        // 3. Insert test data
        // System 1: Primary star is scoopable at 0.0 ls
        conn.execute(
            "INSERT INTO systems (id, x, y, z) VALUES (1, 1.0, 1.0, 1.0)",
            [],
        )
        .unwrap();
        conn.execute(
            "INSERT INTO stars (system_id, sub_type, distance_ls) VALUES (1, 'G 2 V', 0.0)",
            [],
        )
        .unwrap();

        // System 2: Primary is non-special, has secondary Neutron and WD
        conn.execute(
            "INSERT INTO systems (id, x, y, z) VALUES (2, 2.0, 2.0, 2.0)",
            [],
        )
        .unwrap();
        conn.execute(
            "INSERT INTO stars (system_id, sub_type, distance_ls) VALUES (2, 'L T Tauri', 0.0)",
            [],
        )
        .unwrap();
        conn.execute("INSERT INTO stars (system_id, sub_type, distance_ls) VALUES (2, 'Neutron Star', 200.0)", []).unwrap();
        conn.execute("INSERT INTO stars (system_id, sub_type, distance_ls) VALUES (2, 'DA White Dwarf', 300.0)", []).unwrap();

        // System 3: System exists but has no stars listed
        conn.execute(
            "INSERT INTO systems (id, x, y, z) VALUES (3, 3.0, 3.0, 3.0)",
            [],
        )
        .unwrap();

        // System 4: Primary is scoopable, also has a secondary scoopable
        conn.execute(
            "INSERT INTO systems (id, x, y, z) VALUES (4, 4.0, 4.0, 4.0)",
            [],
        )
        .unwrap();
        conn.execute(
            "INSERT INTO stars (system_id, sub_type, distance_ls) VALUES (4, 'M 5 VI', 0.0)",
            [],
        )
        .unwrap();
        conn.execute(
            "INSERT INTO stars (system_id, sub_type, distance_ls) VALUES (4, 'K 1 IV', 60.0)",
            [],
        )
        .unwrap();

        // 4. Run the function
        let result = load_and_process_data(&conn).unwrap();

        // 5. Assert results
        assert_eq!(result.points.len(), 4);
        assert_eq!(result.scoopable_count, 3);
        assert_eq!(result.neutron_count, 1);
        assert_eq!(result.wd_count, 1);

        // --- UPDATED ASSERTIONS ---

        // Assert details for system 2
        let point2 = result.points.iter().find(|p| p.id == 2).unwrap();
        assert_eq!(
            point2.flags, 0,
            "System 2's primary star should have no special flags"
        );
        assert_eq!(point2.secondary_stars.len(), 2);

        let neutron_star = point2
            .secondary_stars
            .iter()
            .find(|s| s.flags == StarFlags::Neutron as u8)
            .unwrap();
        assert_eq!(neutron_star.distance_ls, 200.0);

        let wd_star = point2
            .secondary_stars
            .iter()
            .find(|s| s.flags == StarFlags::WhiteDwarf as u8)
            .unwrap();
        assert_eq!(wd_star.distance_ls, 300.0);

        // Assert details for system 4
        let point4 = result.points.iter().find(|p| p.id == 4).unwrap();
        assert_eq!(
            point4.flags,
            StarFlags::Scoopable as u8,
            "System 4's primary star should be scoopable"
        );
        assert_eq!(point4.secondary_stars.len(), 1);
        assert_eq!(point4.secondary_stars[0].distance_ls, 60.0);
        assert_eq!(point4.secondary_stars[0].flags, StarFlags::Scoopable as u8);
    }

    #[test]
    fn test_write_cache() {
        // 1. Create sample data using the NEW SystemPoint structure
        let processed_data = ProcessedData {
            points: vec![
                SystemPoint {
                    id: 1,
                    idx: 0,
                    x: 1.0,
                    y: 2.0,
                    z: 3.0,
                    flags: StarFlags::Scoopable as u8,
                    secondary_stars: vec![], // System 1's primary is scoopable
                },
                SystemPoint {
                    id: 2,
                    idx: 0,
                    x: 4.0,
                    y: 5.0,
                    z: 6.0,
                    flags: 0, // System 2's primary is not special
                    secondary_stars: vec![SecondaryStar {
                        distance_ls: 200.0,
                        flags: StarFlags::Neutron as u8,
                    }],
                },
            ],
            scoopable_count: 1,
            neutron_count: 1,
            wd_count: 0,
        };

        // 2. Create a temporary file path
        let mut temp_path = std::env::temp_dir();
        temp_path.push("test_cache_gen.rkyv");
        let path_str = temp_path.to_str().unwrap();

        // 3. Run the write_cache function
        let write_result = write_cache(path_str, processed_data.clone());
        assert!(write_result.is_ok());

        // 4. Read the file back and get an archived view
        let mut file = File::open(path_str).unwrap();
        let mut buffer = Vec::new();
        file.read_to_end(&mut buffer).unwrap();
        let archived = unsafe { rkyv::archived_root::<FullCacheData>(&buffer[..]) };

        // 5. Assert the deserialized data is correct using the NEW structure
        assert_eq!(archived.points.len(), 2);
        assert_eq!(archived.stats.neutron_count, 1);

        // Check system 1
        assert_eq!(archived.points[0].id, 1);
        assert_eq!(archived.points[0].flags, StarFlags::Scoopable as u8);
        assert!(archived.points[0].secondary_stars.is_empty());

        // Check system 2
        assert_eq!(archived.points[1].id, 2);
        assert_eq!(archived.points[1].flags, 0);
        assert_eq!(archived.points[1].secondary_stars.len(), 1);
        assert_eq!(archived.points[1].secondary_stars[0].distance_ls, 200.0);
        assert_eq!(
            archived.points[1].secondary_stars[0].flags,
            StarFlags::Neutron as u8
        );

        // 6. Clean up the file
        std::fs::remove_file(path_str).unwrap();
    }
}
