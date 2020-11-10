package boltdb

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/pkg/errors"
	bolt "go.etcd.io/bbolt"
)

// MarshalIndexKey marshals key of index database into []byte.
// We do not use named type because that added 1.5 ns/op overhead.
func MarshalIndexKey(f float64) []byte {
	var out = make([]byte, 8)
	// We multiply f by 1000 to move the fractional part up 3 places.
	// Then we round, and increment max int32 to get rid of negative
	// numbers, so we would start at 0 with f equal to min int32.
	binary.BigEndian.PutUint64(out, uint64(int64(f*1000)+math.MaxInt32))
	return out
}

// UnmarshalIndexKey unmarshals key from index database into float64.
// We do not use named type because that added 1.5 ns/op overhead.
func UnmarshalIndexKey(b []byte) float64 {
	ui := binary.BigEndian.Uint64(b)
	i := int64(ui - math.MaxInt32)
	return float64(i) / 1000
}

func MarshalIndexValue(s []System) []byte {
	var buf = bytes.NewBuffer(nil)

	// Write length of i to the start of the buf.
	err := binary.Write(buf, binary.BigEndian, uint64(len(s)))
	if err != nil {
		panic(err)
	}

	err = binary.Write(buf, binary.BigEndian, s)
	if err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func MarshalIndexValueSingle(s System) []byte {
	var buf = bytes.NewBuffer(nil)
	err := binary.Write(buf, binary.BigEndian, s)
	if err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func UnmarshalIndexValueSingle(b []byte) System {
	var s System

	s.ID64 = binary.BigEndian.Uint64(b)
	b = b[8:]
	s.X = math.Float64frombits(binary.BigEndian.Uint64(b))
	b = b[8:]
	s.Y = math.Float64frombits(binary.BigEndian.Uint64(b))
	b = b[8:]
	s.Z = math.Float64frombits(binary.BigEndian.Uint64(b))
	b = b[8:]
	s.IsNeutron = b[0] != 0
	b = b[1:]
	s.IsScoopable = b[0] != 0

	return s
}

func UnmarshalIndexValue(b []byte) []System {
	size := binary.BigEndian.Uint64(b)
	b = b[8:]

	var out = make([]System, size)
	for i := range out {
		out[i].ID64 = binary.BigEndian.Uint64(b)
		b = b[8:]
		out[i].X = math.Float64frombits(binary.BigEndian.Uint64(b))
		b = b[8:]
		out[i].Y = math.Float64frombits(binary.BigEndian.Uint64(b))
		b = b[8:]
		out[i].Z = math.Float64frombits(binary.BigEndian.Uint64(b))
		b = b[8:]
		out[i].IsNeutron = b[0] != 0
		b = b[1:]
		out[i].IsScoopable = b[0] != 0
		b = b[1:]
	}
	return out
}

type System struct {
	ID64        uint64
	X, Y, Z     float64
	IsNeutron   bool
	IsScoopable bool
}

func (db *DB) PointsWithin(minX, maxX, minY, maxY, minZ, maxZ float64) ([]System, error) {
	var (
		out  []System
		mapX = make(map[uint64]System)
		mapY = make(map[uint64]System)
		mapZ = make(map[uint64]System)

		minXB = MarshalIndexKey(minX)
		maxXB = MarshalIndexKey(maxX)
		minYB = MarshalIndexKey(minY)
		maxYB = MarshalIndexKey(maxY)
		minZB = MarshalIndexKey(minZ)
		maxZB = MarshalIndexKey(maxZ)
	)

	err := db.index.View(func(tx *bolt.Tx) error {
		cX := tx.Bucket(bucketX).Cursor()

		for k, v := cX.Seek(minXB); k != nil && bytes.Compare(k, maxXB) <= 0; k, v = cX.Next() {
			for _, xSystem := range UnmarshalIndexValue(v) {
				if systemWithinBounds(xSystem, minX, maxX, minY, maxY, minZ, maxZ) {
					mapX[xSystem.ID64] = xSystem
				}
			}
		}

		cY := tx.Bucket(bucketY).Cursor()
		for k, v := cY.Seek(minYB); k != nil && bytes.Compare(k, maxYB) <= 0; k, v = cY.Next() {
			for _, ySystem := range UnmarshalIndexValue(v) {
				if systemWithinBounds(ySystem, minX, maxX, minY, maxY, minZ, maxZ) {
					mapY[ySystem.ID64] = ySystem
				}
			}
		}

		cZ := tx.Bucket(bucketZ).Cursor()
		for k, v := cZ.Seek(minZB); k != nil && bytes.Compare(k, maxZB) <= 0; k, v = cZ.Next() {
			for _, zSystem := range UnmarshalIndexValue(v) {
				if systemWithinBounds(zSystem, minX, maxX, minY, maxY, minZ, maxZ) {
					mapZ[zSystem.ID64] = zSystem
				}
			}
		}
		return nil
	})
	if err != nil {
		return out, errors.Wrap(err, "error selecting points")
	}

	for id64, system := range mapX {
		_, ok := mapY[id64]
		if !ok {
			continue
		}
		_, ok = mapZ[id64]
		if !ok {
			continue
		}
		out = append(out, system)
	}
	return out, err
}

func (db *DB) PointsWithinXYZBuckets(minX, maxX, minY, maxY, minZ, maxZ float64) ([]System, error) {
	var (
		out []System

		minXB = MarshalIndexKey(minX)
		maxXB = MarshalIndexKey(maxX)
		minYB = MarshalIndexKey(minY)
		maxYB = MarshalIndexKey(maxY)
		minZB = MarshalIndexKey(minZ)
		maxZB = MarshalIndexKey(maxZ)
	)

	err := db.index.View(func(tx *bolt.Tx) error {
		rootBucket := tx.Bucket([]byte("root"))
		root := rootBucket.Cursor()

		// Iterate X buckets from root (1st known bucket).
		for rootK, rootV := root.Seek(minXB); rootK != nil && rootV == nil && bytes.Compare(rootK, maxXB) <= 0; rootK, rootV = root.Next() {
			xBucket := rootBucket.Bucket(rootK)
			if xBucket == nil {
				continue
			}
			xCur := xBucket.Cursor()

			for xK, xV := xCur.Seek(minYB); xK != nil && xV == nil && bytes.Compare(xK, maxYB) <= 0; xK, xV = xCur.Next() {
				yBucket := xBucket.Bucket(xK)
				if yBucket == nil {
					continue
				}
				yCur := yBucket.Cursor()

				for yK, yV := yCur.Seek(minZB); yK != nil && yV != nil && bytes.Compare(yK, maxZB) <= 0; yK, yV = yCur.Next() {
					out = append(out, UnmarshalIndexValueSingle(yV))
				}
			}
		}
		return nil
	})
	if err != nil {
		return out, errors.Wrap(err, "error selecting points")
	}

	return out, err
}

func (db *DB) PointsWithinXYZBucketsChan(minX, maxX, minY, maxY, minZ, maxZ float64, out chan System) error {
	var (
		minXB = MarshalIndexKey(minX)
		maxXB = MarshalIndexKey(maxX)
		minYB = MarshalIndexKey(minY)
		maxYB = MarshalIndexKey(maxY)
		minZB = MarshalIndexKey(minZ)
		maxZB = MarshalIndexKey(maxZ)
	)

	err := db.index.View(func(tx *bolt.Tx) error {
		rootBucket := tx.Bucket([]byte("root"))
		root := rootBucket.Cursor()

		// Iterate X buckets from root (1st known bucket).
		for rootK, rootV := root.Seek(minXB); rootK != nil && rootV == nil && bytes.Compare(rootK, maxXB) <= 0; rootK, rootV = root.Next() {
			xBucket := rootBucket.Bucket(rootK)
			if xBucket == nil {
				continue
			}
			xCur := xBucket.Cursor()

			for xK, xV := xCur.Seek(minYB); xK != nil && xV == nil && bytes.Compare(xK, maxYB) <= 0; xK, xV = xCur.Next() {
				yBucket := xBucket.Bucket(xK)
				if yBucket == nil {
					continue
				}
				yCur := yBucket.Cursor()

				for yK, yV := yCur.Seek(minZB); yK != nil && yV != nil && bytes.Compare(yK, maxZB) <= 0; yK, yV = yCur.Next() {
					out <- UnmarshalIndexValueSingle(yV)
				}
			}
		}
		return nil
	})
	close(out)
	if err != nil {
		return errors.Wrap(err, "error selecting points")
	}

	return err
}

func (db *DB) PointsWithinConcurrent(minX, maxX, minY, maxY, minZ, maxZ float64) ([]System, error) {
	var (
		out     []System
		errs    []error
		errChan chan error
		wg      sync.WaitGroup
		mapX    = make(map[uint64]System)
		mapY    = make(map[uint64]System)
		mapZ    = make(map[uint64]System)

		minXB = MarshalIndexKey(minX)
		maxXB = MarshalIndexKey(maxX)
		minYB = MarshalIndexKey(minY)
		maxYB = MarshalIndexKey(maxY)
		minZB = MarshalIndexKey(minZ)
		maxZB = MarshalIndexKey(maxZ)
	)

	go func() {
		for err := range errChan {
			errs = append(errs, err)
		}
	}()

	db.concurrentQuery(&wg, errChan, func(tx *bolt.Tx) error {
		cX := tx.Bucket(bucketX).Cursor()
		for k, v := cX.Seek(minXB); k != nil && bytes.Compare(k, maxXB) <= 0; k, v = cX.Next() {
			for _, xSystem := range UnmarshalIndexValue(v) {
				if systemWithinBounds(xSystem, minX, maxX, minY, maxY, minZ, maxZ) {
					mapX[xSystem.ID64] = xSystem
				}
			}
		}
		return nil
	})

	db.concurrentQuery(&wg, errChan, func(tx *bolt.Tx) error {
		cY := tx.Bucket(bucketY).Cursor()
		for k, v := cY.Seek(minYB); k != nil && bytes.Compare(k, maxYB) <= 0; k, v = cY.Next() {
			for _, ySystem := range UnmarshalIndexValue(v) {
				if systemWithinBounds(ySystem, minX, maxX, minY, maxY, minZ, maxZ) {
					mapY[ySystem.ID64] = ySystem
				}
			}
		}
		return nil
	})

	db.concurrentQuery(&wg, errChan, func(tx *bolt.Tx) error {
		cZ := tx.Bucket(bucketZ).Cursor()
		for k, v := cZ.Seek(minZB); k != nil && bytes.Compare(k, maxZB) <= 0; k, v = cZ.Next() {
			for _, zSystem := range UnmarshalIndexValue(v) {
				if systemWithinBounds(zSystem, minX, maxX, minY, maxY, minZ, maxZ) {
					mapZ[zSystem.ID64] = zSystem
				}
			}
		}
		return nil
	})

	wg.Wait()

	if len(errs) > 0 {
		errsOut := []string{"Errors:"}

		for i, err := range errs {
			errsOut = append(errsOut, fmt.Sprintf("[%d] %+v", i, err))
		}
		return out, errors.New(strings.Join(errsOut, "\n"))
	}

	for id64, system := range mapX {
		_, ok := mapY[id64]
		if !ok {
			continue
		}
		_, ok = mapZ[id64]
		if !ok {
			continue
		}
		out = append(out, system)
	}
	return out, nil
}

func (db *DB) concurrentQuery(wg *sync.WaitGroup, errChan chan error, queryFunc func(tx *bolt.Tx) error) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := db.index.View(queryFunc)
		if err != nil {
			errChan <- errors.Wrap(err, "error selecting Z systems")
		}
	}()
}

func systemWithinBounds(s System, minX, maxX, minY, maxY, minZ, maxZ float64) bool {
	return s.X >= minX && s.X <= maxX &&
		s.Y >= minY && s.Y <= maxY &&
		s.Z >= minZ && s.Z <= maxZ
}

func IndexBatchWriter(db *bolt.DB, batch []interface{}) error {
	tx, err := db.Begin(true)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	xBucket := tx.Bucket(bucketX)
	yBucket := tx.Bucket(bucketY)
	zBucket := tx.Bucket(bucketZ)

	xSystems := make(map[float64][]System)
	ySystems := make(map[float64][]System)
	zSystems := make(map[float64][]System)

	for _, untypedItem := range batch {
		item := untypedItem.(System)
		upsertToMap(xSystems, item.X, item)
		upsertToMap(ySystems, item.Y, item)
		upsertToMap(zSystems, item.Z, item)
	}

	for coord, systems := range xSystems {
		err = insertSystemToDimension(xBucket, coord, systems)
		if err != nil {
			return errors.Wrap(err, "unable to insert to X bucket")
		}
	}
	for coord, systems := range ySystems {
		err = insertSystemToDimension(yBucket, coord, systems)
		if err != nil {
			return errors.Wrap(err, "unable to insert to Y bucket")
		}
	}
	for coord, systems := range zSystems {
		err = insertSystemToDimension(zBucket, coord, systems)
		if err != nil {
			return errors.Wrap(err, "unable to insert to Z bucket")
		}
	}

	return tx.Commit()
}

func IndexBatchWriterXYZBuckets(db *bolt.DB, batch []interface{}) error {
	tx, err := db.Begin(true)
	if err != nil {
		return err
	}
	setWriteFlag(tx)
	defer tx.Rollback()

	rootBucket := tx.Bucket(bucketRoot)

	for _, untypedItem := range batch {
		system := untypedItem.(System)

		xbucket, err := rootBucket.CreateBucketIfNotExists(MarshalIndexKey(system.X))
		if err != nil {
			return errors.Wrap(err, "error creating X coord bucket under root")
		}
		ybucket, err := xbucket.CreateBucketIfNotExists(MarshalIndexKey(system.Y))
		if err != nil {
			return errors.Wrap(err, "error creating Y coord bucket under X bucket")
		}
		err = ybucket.Put(MarshalIndexKey(system.Z), MarshalIndexValueSingle(system))
		if err != nil {
			return errors.Wrap(err, "error creating Z key under Y bucket")
		}
	}

	return tx.Commit()
}

func upsertToMap(m map[float64][]System, key float64, val System) {
	m[key] = append(m[key], val)
}

func insertSystemToDimension(bucket *bolt.Bucket, dimCoord float64, systems []System) error {
	key := MarshalIndexKey(dimCoord)
	previousBatch := bucket.Get(key)
	if previousBatch != nil {
		systems = append(systems, UnmarshalIndexValue(previousBatch)...)
	}
	err := bucket.Put(key, MarshalIndexValue(systems))
	if err != nil {
		return errors.Wrap(err, "unable to insert dimension coordinates to bucket")
	}
	return nil
}
