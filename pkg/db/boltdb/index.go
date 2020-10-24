package boltdb

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"

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
	fmt.Printf("Searching: %f %f %f %f %f %f \n", minX, maxX, minY, maxY, minZ, maxZ)

	err := db.index.View(func(tx *bolt.Tx) error {
		cX := tx.Bucket(bucketX).Cursor()
		err := cX.Bucket().ForEach(func(k, v []byte) error {
			for _, xSystem := range UnmarshalIndexValue(v) {
				if xSystem.ID64 == 10477373803 {
					fmt.Printf("FOUND! %+v %+v \n", UnmarshalIndexKey(k), xSystem)
				}
			}
			return nil
		})
		if err != nil {
			fmt.Printf("ERR %+v \n", err)
		}

		for k, v := cX.Seek(minXB); k != nil && bytes.Compare(k, maxXB) <= 0; k, v = cX.Next() {
			for _, xSystem := range UnmarshalIndexValue(v) {
				fmt.Printf("System in X range: %+v \n", xSystem)
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
