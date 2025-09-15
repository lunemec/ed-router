use r2d2_sqlite::SqliteConnectionManager;
use rusqlite::Result; // Import the shared API struct
use std::collections::HashMap;

use ed_router::cache::SystemSearchResult;

// Define a type alias for the connection pool for convenience.
pub type Pool = r2d2::Pool<SqliteConnectionManager>;

/// Creates a new connection pool.
pub fn create_pool(db_path: &String) -> Pool {
    let manager = SqliteConnectionManager::file(db_path);
    // Create a pool with a max size of your number of CPU cores for efficient parallel queries.
    r2d2::Pool::builder()
        .max_size(num_cpus::get() as u32)
        .build(manager)
        .expect("Failed to create DB pool.")
}

/// Searches the database for systems matching a given name using a pooled connection.
pub fn search_systems_by_name(pool: &Pool, name: &str) -> Result<Vec<SystemSearchResult>> {
    let conn = pool
        .get()
        .map_err(|_| rusqlite::Error::QueryReturnedNoRows)?;

    let mut stmt = conn.prepare(
        "SELECT s.id, s.name FROM systems_fts f
         JOIN systems s ON f.rowid = s.id
         WHERE f.name MATCH ?
         ORDER BY f.rank
         LIMIT 20",
    )?;

    let search_query = format!("{}*", name);

    let system_iter = stmt.query_map([&search_query], |row| {
        Ok(SystemSearchResult {
            id: row.get(0)?,
            name: row.get(1)?,
        })
    })?;

    let mut systems = Vec::new();
    for system in system_iter {
        systems.push(system?);
    }

    Ok(systems)
}

/// Fetches the names for a given list of system IDs.
pub fn get_system_names_by_ids(pool: &Pool, ids: &[i64]) -> Result<HashMap<i64, String>> {
    if ids.is_empty() {
        return Ok(HashMap::new());
    }

    let conn = pool
        .get()
        .map_err(|_| rusqlite::Error::QueryReturnedNoRows)?;
    let mut all_names = HashMap::new();

    // Process IDs in chunks to avoid exceeding SQLite's variable limit.
    for chunk in ids.chunks(900) {
        let placeholders: Vec<&str> = chunk.iter().map(|_| "?").collect();
        let sql = format!(
            "SELECT id, name FROM systems WHERE id IN ({})",
            placeholders.join(", ")
        );

        let params: Vec<&dyn rusqlite::ToSql> =
            chunk.iter().map(|id| id as &dyn rusqlite::ToSql).collect();

        let mut stmt = conn.prepare(&sql)?;
        let mut rows = stmt.query(&params[..])?;

        while let Some(row) = rows.next()? {
            all_names.insert(row.get(0)?, row.get(1)?);
        }
    }

    Ok(all_names)
}
