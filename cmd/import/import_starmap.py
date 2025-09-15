import sqlite3
import gzip
import json
import os
import sys
import time

# --- Configuration ---
# The name of the gzipped EDSM data dump file.
# Download from: https://www.edsm.net/en/dumps
EDSM_DUMP_FILENAME = r"C:\Users\lunem\Downloads\systemsWithCoordinates.json.gz"

# The name of the SQLite database file to be created.
DB_FILENAME = "starmap.db"

# How many records to batch together before committing to the database.
BATCH_SIZE = 50000

def import_starmap():
    """
    Reads the gzipped EDSM star map dump and imports it into an SQLite database.
    This is a one-time process that can take a very long time.
    """
    if not os.path.exists(EDSM_DUMP_FILENAME):
        print(f"Error: EDSM data dump '{EDSM_DUMP_FILENAME}' not found.")
        print("Please download it from https://www.edsm.net/en/dumps and place it in the same directory.")
        return

    if os.path.exists(DB_FILENAME):
        print(f"Database '{DB_FILENAME}' already exists. Skipping import.")
        print("To re-import, please delete the existing database file.")
        return

    print(f"Creating SQLite database: {DB_FILENAME}")
    conn = sqlite3.connect(DB_FILENAME)
    cursor = conn.cursor()

    # Create the main table for star systems
    cursor.execute('''
        CREATE TABLE systems (
            id INTEGER PRIMARY KEY,
            name TEXT NOT NULL,
            x REAL NOT NULL,
            y REAL NOT NULL,
            z REAL NOT NULL
        )
    ''')

    # Create indices for faster coordinate lookups (critical for performance)
    print("Creating table and indices...")
    cursor.execute('CREATE UNIQUE INDEX idx_name ON systems (name)')
    cursor.execute('CREATE INDEX idx_x ON systems (x)')
    cursor.execute('CREATE INDEX idx_y ON systems (y)')
    cursor.execute('CREATE INDEX idx_z ON systems (z)')
    conn.commit()

    print(f"Starting import from '{EDSM_DUMP_FILENAME}'. This will take a long time...")
    
    systems_batch = []
    count = 0
    
    # --- Progress tracking variables ---
    total_size = os.path.getsize(EDSM_DUMP_FILENAME)
    start_time = time.time()

    with gzip.open(EDSM_DUMP_FILENAME, 'rt', encoding='utf-8') as f:
        for line in f:
            try:
                print(line, flush=True)
                system_data = json.loads(line)
                
                # We only need systems with coordinates
                if 'name' in system_data and 'coords' in system_data:
                    name = system_data['name']
                    coords = system_data['coords']
                    
                    if all(k in coords for k in ('x', 'y', 'z')):
                        systems_batch.append((name, coords['x'], coords['y'], coords['z']))
                        count += 1

                # When the batch is full, insert it into the database
                if len(systems_batch) >= BATCH_SIZE:
                    cursor.executemany('INSERT INTO systems (name, x, y, z) VALUES (?, ?, ?, ?)', systems_batch)
                    conn.commit()
                    systems_batch = []
                    
                    # --- Enhanced progress reporting ---
                    current_pos = f.fileobj.tell()
                    progress_percent = (current_pos / total_size) * 100
                    elapsed_time = time.time() - start_time
                    
                    eta_str = "Calculating..."
                    if elapsed_time > 1: # Avoid division by zero and erratic early estimates
                        bytes_per_second = current_pos / elapsed_time
                        remaining_bytes = total_size - current_pos
                        eta_seconds = remaining_bytes / bytes_per_second
                        eta_str = time.strftime('%H:%M:%S', time.gmtime(eta_seconds))
                    
                    bar_length = 40
                    filled_length = int(bar_length * current_pos // total_size)
                    bar = '█' * filled_length + '-' * (bar_length - filled_length)
                    
                    progress_text = f"\rProgress: |{bar}| {progress_percent:.1f}% ({count:,} systems) ETA: {eta_str} "
                    sys.stdout.write(progress_text)
                    sys.stdout.flush()

            except (json.JSONDecodeError, sqlite3.IntegrityError):
                # Some lines might be malformed or have duplicate system names, skip them
                continue
    
    # Insert any remaining systems in the last batch
    if systems_batch:
        cursor.executemany('INSERT INTO systems (name, x, y, z) VALUES (?, ?, ?, ?)', systems_batch)
        conn.commit()

    print(f"\n\nImport complete. A total of {count:,} systems were imported into '{DB_FILENAME}'.")
    print("You can now run the Flask server with 'python app.py'")
    conn.close()

if __name__ == "__main__":
    import_starmap()