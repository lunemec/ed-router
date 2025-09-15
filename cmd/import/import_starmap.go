package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// --- Configuration ---
const (
	EDSM_DUMP_FILENAME = `C:\Users\lunem\Downloads\galaxy.json.gz`
	DB_FILENAME        = "starmap.db"
	BATCH_SIZE         = 50000
)

var bufferPool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}

// --- JSON Data Structures ---

type SystemCoords struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

type Body struct {
	Type              string  `json:"type"`
	SubType           string  `json:"subType"`
	DistanceToArrival float64 `json:"distanceToArrival"`
}

type FullSystemData struct {
	ID64   int64        `json:"id64"`
	Name   string       `json:"name"`
	Coords SystemCoords `json:"coords"`
	Bodies []Body       `json:"bodies"`
}

// --- Database Structs ---

type SystemForDB struct {
	ID64   int64
	Name   string
	Coords SystemCoords
}

type StarForDB struct {
	SystemID   int64
	SubType    string
	DistanceLS float64
}

type SystemAndStars struct {
	System SystemForDB
	Stars  []StarForDB
}

type progressTracker struct {
	bytesRead atomic.Int64
	fileSize  int64
}

func main() {
	if _, err := os.Stat(EDSM_DUMP_FILENAME); os.IsNotExist(err) {
		log.Fatalf("Error: EDSM data dump '%s' not found.", EDSM_DUMP_FILENAME)
	}
	if _, err := os.Stat(DB_FILENAME); err == nil {
		log.Printf("Database '%s' already exists. Skipping import.", DB_FILENAME)
		return
	}

	db, err := setupDatabase()
	if err != nil {
		log.Fatalf("Failed to set up database: %v", err)
	}
	defer db.Close()

	linesChan := make(chan *bytes.Buffer, 1000)
	systemsChan := make(chan SystemAndStars, 1000)
	var parserWg, writerWg sync.WaitGroup

	fileInfo, _ := os.Stat(EDSM_DUMP_FILENAME)
	tracker := &progressTracker{fileSize: fileInfo.Size()}

	log.Printf("Starting import from '%s'. This will take a long time...", EDSM_DUMP_FILENAME)
	startTime := time.Now()

	go readFile(linesChan, tracker)

	numParsers := runtime.NumCPU()
	parserWg.Add(numParsers)
	for i := 0; i < numParsers; i++ {
		go parseLines(linesChan, systemsChan, &parserWg)
	}

	go func() {
		parserWg.Wait()
		close(systemsChan)
	}()

	writerWg.Add(1)
	go writeToDB(systemsChan, &writerWg, db, tracker, startTime)
	writerWg.Wait()

	log.Println("\nData import phase complete. Now creating database indexes...")
	indexStartTime := time.Now()
	if err := createIndexes(db); err != nil {
		log.Fatalf("Failed to create indexes: %v", err)
	}
	log.Printf("Indexes created successfully in %v.", time.Since(indexStartTime))

	log.Println("\nImport process fully complete.")
	log.Println("You can now run the Flask server with 'python app.py'")
}

func setupDatabase() (*sql.DB, error) {
	log.Printf("Creating SQLite database: %s", DB_FILENAME)
	dbURL := fmt.Sprintf("file:%s?_journal_mode=WAL&_synchronous=OFF", DB_FILENAME)
	db, err := sql.Open("sqlite3", dbURL)
	if err != nil {
		return nil, err
	}

	log.Println("Creating tables...")
	systemsTableSQL := `
        CREATE TABLE systems (
            id INTEGER PRIMARY KEY,
            name TEXT NOT NULL,
            x REAL NOT NULL,
            y REAL NOT NULL,
            z REAL NOT NULL
        );
    `
	if _, err := db.Exec(systemsTableSQL); err != nil {
		return nil, fmt.Errorf("failed to create systems table: %w", err)
	}

	starsTableSQL := `
        CREATE TABLE stars (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            system_id INTEGER NOT NULL,
            sub_type TEXT,
            distance_ls REAL NOT NULL,
            FOREIGN KEY (system_id) REFERENCES systems (id)
        );
    `
	if _, err := db.Exec(starsTableSQL); err != nil {
		return nil, fmt.Errorf("failed to create stars table: %w", err)
	}

	return db, nil
}

func createIndexes(db *sql.DB) error {
	log.Println("Step 1/3: Creating standard indexes...")
	standardIndexes := `
        CREATE INDEX idx_systems_name ON systems (name);
        CREATE INDEX idx_stars_system_id ON stars (system_id);
        CREATE INDEX idx_stars_sub_type ON stars (sub_type);
    `
	if _, err := db.Exec(standardIndexes); err != nil {
		return fmt.Errorf("failed to create standard indexes: %w", err)
	}

	log.Println("Step 2/3: Creating FTS index for fast name search...")
	ftsIndex := `
		CREATE VIRTUAL TABLE systems_fts USING fts5(name, content='systems', content_rowid='id');
		INSERT INTO systems_fts(rowid, name) SELECT id, name FROM systems;
	`
	if _, err := db.Exec(ftsIndex); err != nil {
		return fmt.Errorf("failed to create FTS index: %w", err)
	}

	log.Println("Step 3/3: Creating R-Tree index for fast spatial search...")
	rtreeIndex := `
		CREATE VIRTUAL TABLE systems_rtree USING rtree(id, minX, maxX, minY, maxY, minZ, maxZ);
		INSERT INTO systems_rtree(id, minX, maxX, minY, maxY, minZ, maxZ) SELECT id, x, x, y, y, z, z FROM systems;
	`
	if _, err := db.Exec(rtreeIndex); err != nil {
		return fmt.Errorf("failed to create R-Tree index: %w", err)
	}

	return nil
}

func readFile(linesChan chan<- *bytes.Buffer, tracker *progressTracker) {
	defer close(linesChan)

	file, err := os.Open(EDSM_DUMP_FILENAME)
	if err != nil {
		log.Fatalf("Error opening file: %v", err)
	}
	defer file.Close()

	progressReader := io.TeeReader(file, &progressWriter{tracker: tracker})
	gz, err := gzip.NewReader(progressReader)
	if err != nil {
		log.Fatalf("Error creating gzip reader: %v", err)
	}
	defer gz.Close()

	reader := bufio.NewReader(gz)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Printf("Error reading from gzip stream: %v", err)
			break
		}
		buf := bufferPool.Get().(*bytes.Buffer)
		buf.Reset()
		buf.Write(line)
		linesChan <- buf
	}
}

func parseLines(linesChan <-chan *bytes.Buffer, systemsChan chan<- SystemAndStars, wg *sync.WaitGroup) {
	defer wg.Done()
	for buf := range linesChan {
		var s FullSystemData
		lineBytes := buf.Bytes()
		trimmedBytes := bytes.TrimSpace(lineBytes)

		if len(trimmedBytes) > 0 && trimmedBytes[len(trimmedBytes)-1] == ',' {
			trimmedBytes = trimmedBytes[:len(trimmedBytes)-1]
		}

		if err := json.Unmarshal(trimmedBytes, &s); err == nil {
			if s.ID64 != 0 && s.Name != "" {
				systemDB := SystemForDB{
					ID64:   s.ID64,
					Name:   s.Name,
					Coords: s.Coords,
				}

				starsDB := make([]StarForDB, 0)
				for _, body := range s.Bodies {
					if body.Type == "Star" {
						star := StarForDB{
							SystemID:   s.ID64,
							SubType:    body.SubType,
							DistanceLS: body.DistanceToArrival,
						}
						starsDB = append(starsDB, star)
					}
				}

				if len(starsDB) > 0 {
					systemsChan <- SystemAndStars{System: systemDB, Stars: starsDB}
				}
			}
		}
		bufferPool.Put(buf)
	}
}

func writeToDB(systemsChan <-chan SystemAndStars, wg *sync.WaitGroup, db *sql.DB, tracker *progressTracker, startTime time.Time) {
	defer wg.Done()

	batch := make([]SystemAndStars, 0, BATCH_SIZE)
	var totalCount int64
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	running := true
	for running {
		select {
		case systemGroup, ok := <-systemsChan:
			if !ok {
				running = false
				break
			}
			batch = append(batch, systemGroup)
			if len(batch) >= BATCH_SIZE {
				if err := insertBatch(db, batch); err != nil {
					log.Printf("Error inserting batch: %v", err)
				}
				totalCount += int64(len(batch))
				batch = batch[:0]
			}
		case <-ticker.C:
			printProgress(totalCount, tracker, startTime)
		}
	}

	if len(batch) > 0 {
		if err := insertBatch(db, batch); err != nil {
			log.Printf("Error inserting final batch: %v", err)
		}
		totalCount += int64(len(batch))
	}
	printProgress(totalCount, tracker, startTime)
	fmt.Println()
	log.Printf("\nImport phase complete. A total of %d systems were read.", totalCount)
}

func insertBatch(db *sql.DB, batch []SystemAndStars) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}

	systemStmt, err := tx.Prepare("INSERT OR IGNORE INTO systems (id, name, x, y, z) VALUES (?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer systemStmt.Close()

	starStmt, err := tx.Prepare("INSERT INTO stars (system_id, sub_type, distance_ls) VALUES (?, ?, ?)")
	if err != nil {
		return err
	}
	defer starStmt.Close()

	for _, systemGroup := range batch {
		_, err := systemStmt.Exec(systemGroup.System.ID64, systemGroup.System.Name, systemGroup.System.Coords.X, systemGroup.System.Coords.Y, systemGroup.System.Coords.Z)
		if err != nil {
			continue // Ignore system insert errors (like duplicates if any)
		}

		for _, star := range systemGroup.Stars {
			_, err := starStmt.Exec(star.SystemID, star.SubType, star.DistanceLS)
			if err != nil {
				// Log star insert errors but continue
				log.Printf("Error inserting star for system ID %d: %v", star.SystemID, err)
			}
		}
	}

	return tx.Commit()
}

// --- Progress Bar Utilities ---

type progressWriter struct {
	tracker *progressTracker
}

func (pw *progressWriter) Write(p []byte) (n int, err error) {
	pw.tracker.bytesRead.Add(int64(len(p)))
	return len(p), nil
}

func printProgress(count int64, tracker *progressTracker, startTime time.Time) {
	bytesRead := tracker.bytesRead.Load()
	fileSize := tracker.fileSize
	if fileSize == 0 {
		return
	}
	progressPercent := float64(bytesRead) / float64(fileSize) * 100
	if progressPercent > 100.0 {
		progressPercent = 100.0
	}
	elapsed := time.Since(startTime)

	etaStr := "Calculating..."
	if elapsed > 2*time.Second && bytesRead > 0 && bytesRead < fileSize {
		bytesPerSecond := float64(bytesRead) / elapsed.Seconds()
		remainingBytes := float64(fileSize - bytesRead)
		etaSeconds := remainingBytes / bytesPerSecond
		eta := time.Duration(etaSeconds) * time.Second
		etaStr = fmt.Sprintf("%02d:%02d:%02d", int(eta.Hours())%24, int(eta.Minutes())%60, int(eta.Seconds())%60)
	} else if bytesRead >= fileSize {
		etaStr = "Complete"
	}

	barLength := 40
	filledLength := int(float64(barLength) * progressPercent / 100)
	bar := strings.Repeat("█", filledLength) + strings.Repeat("-", barLength-filledLength)

	fmt.Printf("\rImporting: |%s| %.1f%% (%s systems) ETA: %s ", bar, progressPercent, formatCount(count), etaStr)
}

func formatCount(n int64) string {
	s := fmt.Sprintf("%d", n)
	if n < 0 {
		s = s[1:]
	}
	le := len(s)
	for i := le - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if n < 0 {
		return "-" + s
	}
	return s
}
