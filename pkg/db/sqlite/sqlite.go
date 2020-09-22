package sqlite

import (
	"database/sql"
	"fmt"
	"strings"

	dumpModels "github.com/lunemec/ed-router/pkg/models/dump"
	"github.com/lunemec/ed-router/pkg/pather"
	"gonum.org/v1/gonum/spatial/r3"

	_ "github.com/mattn/go-sqlite3"
	"github.com/pkg/errors"
)

type sqliteDB struct {
	db *sql.DB
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
CREATE INDEX IF NOT EXISTS systems_name_idx ON systems (name COLLATE NOCASE);

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
CREATE INDEX IF NOT EXISTS bodies_system_id64_idx ON bodies (system_id64);

CREATE VIRTUAL TABLE IF NOT EXISTS system_coordinates USING rtree(
	id64,            -- Integer primary key
	minX, maxX,      -- Minimum and maximum X coordinate
	minY, maxY,      -- Minimum and maximum Y coordinate
	minZ, maxZ       -- Minimum and maximum Z coordinate
);
`
	insertSystemSQL = `
INSERT OR IGNORE INTO systems 
	(id64, name, x, y, z)
VALUES
	(?, ?, ?, ?, ?);
`
	insertSystemCoordinatesSQL = `
INSERT OR IGNORE INTO system_coordinates VALUES (?, ?, ?, ?, ?, ?, ?);
	`
	insertBodySQL = `
INSERT OR IGNORE INTO bodies
	(id64, name, type, sub_type, system_id64, distance_to_arrival)
VALUES
	%s;
`
	selectByID64SQL = `
SELECT
	systems.id64, systems.name, systems.x, systems.y, systems.z
FROM
	systems
WHERE
	systems.id64=?
`
	selectBodiesBySystemID64SQL = `
SELECT 
	bodies.id64, bodies.name, bodies.sub_type, bodies.distance_to_arrival
FROM
	bodies
WHERE
	bodies.system_id64=?
`
	selectByNameSQL = `
SELECT systems.id64, systems.name, systems.x, systems.y, systems.z
FROM 
	systems
WHERE 
	systems.name=? 
COLLATE NOCASE
`
	selectSystemID64sWithinDistance = `
SELECT id64 
FROM 
	system_coordinates
WHERE
	minX >= ? AND maxX <= ? AND
	minY >= ? AND maxY <= ? AND
	minZ >= ? AND maxZ <= ?
`
)

// Open opens SQLite or creates new database.
func Open(file string) (*sqliteDB, error) {
	var sqliteDB sqliteDB
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?cache=shared&_sync=0", file))
	if err != nil {
		return nil, errors.Wrap(err, "unable to open database file")
	}

	sqliteDB.db = db
	_, err = db.Exec(createTablesSQL)
	if err != nil {
		return nil, errors.Wrap(err, "unable to create tables")
	}
	return &sqliteDB, nil
}

// InsertSystem inserts system including bodies to db.
func (db *sqliteDB) InsertSystem(s *dumpModels.System) (err error) {
	tx, err := db.db.Begin()
	if err != nil {
		return errors.Wrap(err, "unable to begin transaction")
	}
	defer func() {
		if err != nil {
			errRollback := tx.Rollback()
			if errRollback != nil {
				panic(errRollback)
			}
		}
	}()
	_, err = tx.Exec(insertSystemSQL,
		s.ID64,
		s.Name,
		s.Coordinates.X,
		s.Coordinates.Y,
		s.Coordinates.Z,
	)
	if err != nil {
		return errors.Wrapf(err, "error inserting system: %+v", s)
	}
	_, err = tx.Exec(
		insertSystemCoordinatesSQL,
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
	err = db.insertBodies(tx, s)
	if err != nil {
		return errors.Wrap(err, "error inserting bodies")
	}
	return errors.Wrap(tx.Commit(), "error commiting transaction")
}

func (db *sqliteDB) insertBodies(tx *sql.Tx, s *dumpModels.System) error {
	var insertBodies []dumpModels.Body

	for _, body := range s.Bodies {
		if body.Type != "Star" {
			continue
		}
		insertBodies = append(insertBodies, body)
	}
	if len(insertBodies) == 0 {
		return nil
	}

	var (
		valueStrings []string
		valueArgs    []interface{}
	)
	for _, insertBody := range insertBodies {
		valueStrings = append(valueStrings, "(?, ?, ?, ?, ?, ?)")
		valueArgs = append(valueArgs,
			insertBody.ID64,
			insertBody.Name,
			insertBody.Type,
			insertBody.SubType,
			s.ID64,
			insertBody.DistanceToArrival)
	}
	stmt := fmt.Sprintf(insertBodySQL, strings.Join(valueStrings, ","))
	_, err := tx.Exec(stmt, valueArgs...)
	return err
}

func (db *sqliteDB) SystemByID64(id64 int64) (*pather.System, error) {
	rows := db.db.QueryRow(selectByID64SQL, id64)
	err := rows.Err()
	if err != nil {
		return nil, errors.Wrap(err, "unable to select system by name")
	}

	s, err := db.scanSystemToModel(rows)
	if err != nil {
		return nil, err
	}
	stars, err := db.SystemStars(s.ID64)
	if err != nil {
		return nil, errors.Wrap(err, "error getting system stars")
	}
	s.Stars = stars
	return s, nil
}

func (db *sqliteDB) SystemByName(name string) (*pather.System, error) {
	rows := db.db.QueryRow(selectByNameSQL, name)
	err := rows.Err()
	if err != nil {
		return nil, errors.Wrap(err, "unable to select system by name")
	}

	s, err := db.scanSystemToModel(rows)
	if err != nil {
		return nil, errors.Wrap(err, "unable to scan to model")
	}
	stars, err := db.SystemStars(s.ID64)
	if err != nil {
		return nil, errors.Wrap(err, "error getting system stars")
	}
	s.Stars = stars
	return s, nil
}

func (db *sqliteDB) SystemID64sAround(point r3.Vec, distance float64) ([]int64, error) {
	rows, err := db.db.Query(
		selectSystemID64sWithinDistance,
		point.X-distance, point.X+distance,
		point.Y-distance, point.Y+distance,
		point.Z-distance, point.Z+distance,
	)
	if err != nil {
		return nil, errors.Wrap(err, "error selecting systems within distance")
	}

	var systemID64s []int64
	for rows.Next() {
		var id64 int64
		err = rows.Scan(&id64)
		if err != nil {
			return nil, errors.Wrap(err, "unable to scan system id64")
		}
		systemID64s = append(systemID64s, id64)
	}

	return systemID64s, nil
}

func (db *sqliteDB) SystemStars(systemID64 int64) ([]pather.Star, error) {
	rows, err := db.db.Query(selectBodiesBySystemID64SQL, systemID64)
	defer rows.Close()
	if err != nil {
		return nil, errors.Wrap(err, "unable to select bodies by system_id64")
	}
	stars, err := db.scanBodiesToModel(rows)
	if err != nil {
		return nil, errors.Wrap(err, "error scanning to model")
	}
	return stars, nil
}

func (db *sqliteDB) scanSystemToModel(row *sql.Row) (*pather.System, error) {
	var (
		err    error
		system pather.System
	)
	err = row.Scan(
		&system.ID64,
		&system.Name,
		&system.Coordinates.X,
		&system.Coordinates.Y,
		&system.Coordinates.Z,
	)
	if err != nil {
		return nil, errors.Wrap(err, "unable to scan into pather.System")
	}
	return &system, nil
}

func (db *sqliteDB) scanBodiesToModel(rows *sql.Rows) ([]pather.Star, error) {
	var (
		stars []pather.Star
		err   error
	)
	for rows.Next() {
		var star pather.Star
		err = rows.Scan(
			&star.ID64,
			&star.Name,
			&star.Type,
			&star.Distance,
		)
		if err != nil {
			return nil, errors.Wrap(err, "unable to scan into pather.System")
		}
		stars = append(stars, star)
	}
	return stars, err
}
