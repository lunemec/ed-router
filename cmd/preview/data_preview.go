package main

import (
	"bufio"
	"compress/gzip"
	"io"
	"log"
	"os"

	_ "github.com/mattn/go-sqlite3"
)

// --- Configuration ---
const (
	// EDSM_DUMP_FILENAME is the name of the gzipped EDSM data dump file.
	// Download from: https://www.edsm.net/en/dumps
	EDSM_DUMP_FILENAME = `C:\Users\lunem\Downloads\galaxy.HRr7Bw8a.json.gz.part`
)

func main() {
	// --- Pre-flight Checks ---
	if _, err := os.Stat(EDSM_DUMP_FILENAME); os.IsNotExist(err) {
		log.Fatalf("Error: EDSM data dump '%s' not found.\nPlease download it from https://www.edsm.net/en/dumps and place it in this directory.", EDSM_DUMP_FILENAME)
	}

	log.Printf("Preview of %s", EDSM_DUMP_FILENAME)

	// --- Start Goroutine Pipeline ---

	// Stage 1: Reader
	readFile()
}

// readFile opens the gzipped file and streams lines into reusable buffers via linesChan.
func readFile() {
	file, err := os.Open(EDSM_DUMP_FILENAME)
	if err != nil {
		log.Fatalf("Error opening file: %v", err)
	}
	defer file.Close()

	gz, err := gzip.NewReader(file)
	if err != nil {
		log.Fatalf("Error creating gzip reader: %v", err)
	}
	defer gz.Close()

	printLines := 10
	reader := bufio.NewReader(gz)
	for range printLines {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Printf("Error reading from gzip stream: %v", err)
			break
		}
		log.Println(string(line))

	}
}
