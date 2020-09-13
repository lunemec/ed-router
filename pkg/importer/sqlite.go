package importer

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
	"github.com/pkg/errors"
)

type sqliteDB struct {
	db *sql.DB

	preparedInsertSystem            *sql.Stmt
	preparedInsertSystemCoordinates *sql.Stmt
	preparedInsertBody              *sql.Stmt
}

const (
	createTablesSQL = `
CREATE TABLE IF NOT EXISTS systems (
	id64 BIGINT PRIMARY KEY,
	name TEXT NOT NULL,
	x double NOT NULL, -- coordinates have to be stored here as double
	y double NOT NULL, -- because system_coordinates are only 32bit floats
	z double NOT NULL
);
CREATE INDEX systems_name_idx ON systems (name);

CREATE TABLE IF NOT EXISTS bodies (
	id64 BIGINT PRIMARY KEY,
	name TEXT NOT NULL,
	type VARCHAR(64) NOT NULL,
	sub_type VARCHAR(255) NOT NULL,
	system_id64 BIGINT NOT NULL,
	distance_to_arrival double NOT NULL,
	FOREIGN KEY (system_id64)
		REFERENCES systems (id64) 
			ON DELETE CASCADE 
			ON UPDATE NO ACTION
);
CREATE VIRTUAL TABLE system_coordinates USING rtree(
	id64,            -- Integer primary key
	minX, maxX,      -- Minimum and maximum X coordinate
	minY, maxY,      -- Minimum and maximum Y coordinate
	minZ, maxZ       -- Minimum and maximum Z coordinate
);
`
	insertSystemSQL = `
INSERT INTO systems 
	(id64, name, x, y, z)
VALUES
	(?, ?, ?, ?, ?);
`
	insertSystemCoordinatesSQL = `
INSERT INTO system_coordinates VALUES (?, ?, ?, ?, ?, ?, ?);
	`
	insertBodySQL = `
INSERT INTO bodies
	(id64, name, type, sub_type, system_id64, distance_to_arrival)
VALUES
	(?, ?, ?, ?, ?, ?);
`
)

func newSQLiteDB(file string) (*sqliteDB, error) {
	var sqliteDB sqliteDB
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?cache=shared", file))
	if err != nil {
		return nil, errors.Wrap(err, "unable to open database file")
	}

	sqliteDB.db = db
	_, err = db.Exec(createTablesSQL)
	if err != nil {
		return nil, errors.Wrap(err, "unable to create tables")
	}

	insertSystemStmt, err := db.Prepare(insertSystemSQL)
	if err != nil {
		return nil, errors.Wrap(err, "unable to prepare insert system SQL")
	}
	sqliteDB.preparedInsertSystem = insertSystemStmt

	insertSystemCoordinatesStmt, err := db.Prepare(insertSystemCoordinatesSQL)
	if err != nil {
		return nil, errors.Wrap(err, "unable to prepare insert system SQL")
	}
	sqliteDB.preparedInsertSystemCoordinates = insertSystemCoordinatesStmt

	insertBodyStmt, err := db.Prepare(insertBodySQL)
	if err != nil {
		return nil, errors.Wrap(err, "unable to prepare insert body SQL")
	}
	sqliteDB.preparedInsertBody = insertBodyStmt

	return &sqliteDB, nil
}

func (db *sqliteDB) insertSystem(s *system) error {
	_, err := db.preparedInsertSystem.Exec(
		s.ID64,
		s.Name,
		s.Coordinates.X,
		s.Coordinates.Y,
		s.Coordinates.Z,
	)
	if err != nil {
		return errors.Wrapf(err, "error inserting system: %+v", s)
	}
	_, err = db.preparedInsertSystemCoordinates.Exec(
		s.ID64,
		s.Coordinates.X,
		s.Coordinates.X,
		s.Coordinates.Y,
		s.Coordinates.Y,
		s.Coordinates.Z,
		s.Coordinates.Z,
	)
	if err != nil {
		return errors.Wrapf(err, "error inserting system: %+v", s)
	}
	for _, body := range s.Bodies {
		if body.Type != "Star" {
			continue
		}
		_, err = db.preparedInsertBody.Exec(
			body.ID64,
			body.Name,
			body.Type,
			body.SubType,
			s.ID64,
			body.DistanceToArrival,
		)
		if err != nil {
			return errors.Wrapf(err, "error inserting body: %+v", body)
		}
	}
	return nil
}
