package boltdb

import (
	"fmt"
	"strings"
	"sync"

	"github.com/lunemec/ed-router/pkg/models/dump"

	"github.com/pkg/errors"
	bolt "go.etcd.io/bbolt"
)

var (
	bucketX       = []byte("x")
	bucketY       = []byte("y")
	bucketZ       = []byte("z")
	bucketRoot    = []byte("root")
	bucketSystems = []byte("systems")
	bucketNames   = []byte("names")
)

type DB struct {
	index  *bolt.DB
	galaxy *bolt.DB

	// once is used to start import goroutines only once.
	// used only while importing.
	once   sync.Once
	input  chan dump.System
	wg     sync.WaitGroup
	errors []error
}

func Open(indexFile, galaxyFile string, readOnly bool) (*DB, error) {
	var (
		indexErrMsg  = fmt.Sprintf("unable to open index DB: %s", indexFile)
		galaxyErrMsg = fmt.Sprintf("unable to open galaxy DB: %s", galaxyFile)
	)

	var options bolt.Options
	if readOnly {
		options.ReadOnly = true
	}

	index, err := bolt.Open(indexFile, 0666, &options)
	if err != nil {
		return nil, errors.Wrap(err, indexErrMsg)
	}
	galaxy, err := bolt.Open(galaxyFile, 0666, &options)
	if err != nil {
		return nil, errors.Wrapf(err, galaxyErrMsg)
	}

	if !readOnly {
		err = prepareIndexDB(index)
		if err != nil {
			return nil, errors.Wrap(err, indexErrMsg)
		}
		err = prepareGalaxyDB(galaxy)
		if err != nil {
			return nil, errors.Wrap(err, galaxyErrMsg)
		}
	}

	return &DB{index: index, galaxy: galaxy, input: make(chan dump.System)}, nil
}

func prepareIndexDB(db *bolt.DB) error {
	err := db.Update(func(tx *bolt.Tx) error {
		// Create bucket for X coordinates
		_, err := tx.CreateBucketIfNotExists(bucketX)
		if err != nil {
			return errors.Wrap(err, "unable to create X bucket")
		}
		// Create bucket for Y coordinates
		_, err = tx.CreateBucketIfNotExists(bucketY)
		if err != nil {
			return errors.Wrap(err, "unable to create Y bucket")
		}
		// Create bucket for Z coordinates
		_, err = tx.CreateBucketIfNotExists(bucketZ)
		if err != nil {
			return errors.Wrap(err, "unable to create Z bucket")
		}
		_, err = tx.CreateBucketIfNotExists(bucketRoot)
		if err != nil {
			return errors.Wrap(err, "unable to create root bucket")
		}
		return nil
	})
	if err != nil {
		return errors.Wrap(err, "error preparing index DB buckets")
	}
	return nil
}

func prepareGalaxyDB(db *bolt.DB) error {
	err := db.Update(func(tx *bolt.Tx) error {
		// Main bucket for ID64 -> System JSON
		_, err := tx.CreateBucketIfNotExists(bucketSystems)
		if err != nil {
			return errors.Wrap(err, "unable to create systems bucket")
		}
		// Bucket to translate name -> id64
		_, err = tx.CreateBucketIfNotExists(bucketNames)
		if err != nil {
			return errors.Wrap(err, "unable to create names bucket")
		}
		return nil
	})
	if err != nil {
		return errors.Wrap(err, "error preparing galaxy DB buckets")
	}
	return nil
}

func (db *DB) InsertSystem(s dump.System) error {
	db.once.Do(func() { go db.consumer() })
	db.input <- s
	return nil
}

func (db *DB) StopInsert() error {
	close(db.input)
	db.wg.Wait()

	if len(db.errors) != 0 {
		msg := "Errors during import: \n %s"
		var errs []string
		for i, err := range db.errors {
			errs = append(errs, fmt.Sprintf("  [%d] %+v", i, err))
		}
		return errors.Errorf(msg, strings.Join(errs, "\n"))
	}
	return nil
}

func (db *DB) Close() error {
	errI := db.index.Close()
	errG := db.galaxy.Close()

	if errI != nil || errG != nil {
		return errors.Errorf("error closing database: index: %+v galaxy : %+v", errI, errG)
	}
	return nil
}

func (db *DB) consumer() {
	var (
		indexChan  = make(chan interface{})
		galaxyChan = make(chan interface{})
		errChan    = make(chan error)
	)

	go func() {
		for err := range errChan {
			db.errors = append(db.errors, err)
		}
	}()
	db.wg.Add(1)
	go batchWriter(&db.wg, db.index, errChan, indexChan, IndexBatchWriterXYZBuckets)
	db.wg.Add(1)
	go batchWriter(&db.wg, db.galaxy, errChan, galaxyChan, GalaxyBatchWriter)

	for inputSystem := range db.input {
		indexChan <- System{
			ID64:        inputSystem.ID64,
			X:           inputSystem.Coordinates.X,
			Y:           inputSystem.Coordinates.Y,
			Z:           inputSystem.Coordinates.Z,
			IsNeutron:   NeutronInRange(inputSystem.Bodies),
			IsScoopable: ScoopableInRange(inputSystem.Bodies),
		}
		galaxyChan <- inputSystem
	}

	close(indexChan)
	close(galaxyChan)
	db.wg.Wait()
	close(errChan)
}

type batchWriterFunc func(db *bolt.DB, batch []interface{}) error

func batchWriter(wg *sync.WaitGroup, db *bolt.DB, errChan chan error, data chan interface{}, writerFunc batchWriterFunc) {
	defer wg.Done()
	var (
		batchSize = 10000
		batch     []interface{}
	)

	consumed := 0
	for inSystem := range data {
		batch = append(batch, inSystem)
		consumed++

		if consumed == batchSize-1 {
			err := writerFunc(db, batch)
			if err != nil {
				errChan <- err
			}
			batch = nil
			consumed = 0
		}
	}
	if len(batch) > 0 {
		err := writerFunc(db, batch)
		if err != nil {
			errChan <- err
		}
	}
}

func NeutronInRange(bodies []dump.Body) bool {
	// Anything within 1000ls is considered OK.
	var maxDistance float64 = 1000
	if bodies == nil || len(bodies) == 0 {
		return false
	}

	for _, body := range bodies {
		if body.Type != "Star" {
			continue
		}
		// White dwarfs are not checked since they are considered "not worth it".
		if body.SubType == "Neutron Star" {
			if body.DistanceToArrival <= maxDistance {
				return true
			}
		}
	}

	return false
}

func ScoopableInRange(bodies []dump.Body) bool {
	// Anything within 1000ls is considered OK.
	var maxDistance float64 = 1000
	if bodies == nil || len(bodies) == 0 {
		return false
	}

	for _, body := range bodies {
		if body.Type != "Star" {
			continue
		}
		switch body.SubType {
		case
			"A (Blue-White super giant) Star",
			"A (Blue-White) Star",
			"B (Blue-White super giant) Star",
			"B (Blue-White) Star",
			"F (White super giant) Star",
			"F (White) Star",
			"G (White-Yellow super giant) Star",
			"G (White-Yellow) Star",
			"K (Yellow-Orange giant) Star",
			"K (Yellow-Orange) Star",
			"M (Red dwarf) Star",
			"M (Red giant) Star",
			"M (Red super giant) Star",
			"O (Blue-White) Star":
			if body.DistanceToArrival <= maxDistance {
				return true
			}
		}
	}

	return false
}
