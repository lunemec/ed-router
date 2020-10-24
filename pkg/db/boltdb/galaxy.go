package boltdb

import (
	"encoding/binary"
	"strings"

	jsoniter "github.com/json-iterator/go"
	"github.com/lunemec/ed-router/pkg/models/dump"
	"github.com/pkg/errors"
	bolt "go.etcd.io/bbolt"
)

var json = jsoniter.ConfigCompatibleWithStandardLibrary

func MarshalGalaxyKey(id64 uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, id64)
	return b
}

func UnmarshalGalaxyKey(b []byte) uint64 {
	return binary.BigEndian.Uint64(b)
}

func MarshalGalaxyValue(s dump.System) ([]byte, error) {
	return json.Marshal(s)
}

func UnmarshalGalaxyValue(b []byte) (dump.System, error) {
	var system dump.System
	err := json.Unmarshal(b, &system)
	return system, err
}

func MarshalName(s string) []byte {
	return []byte(strings.ToUpper(s))
}

func UnmarshalName(b []byte) string {
	return string(b)
}

func GalaxyBatchWriter(db *bolt.DB, batch []interface{}) error {
	tx, err := db.Begin(true)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	systemsBucket := tx.Bucket(bucketSystems)
	namesBucket := tx.Bucket(bucketNames)

	for _, untypedItem := range batch {
		item := untypedItem.(dump.System)

		err = insertSystem(systemsBucket, item.ID64, item)
		if err != nil {
			return errors.Wrap(err, "error inserting system")
		}
		err = insertName(namesBucket, item.Name, item.ID64)
		if err != nil {
			return errors.Wrap(err, "error inserting name")
		}
	}

	return tx.Commit()
}

func insertSystem(bucket *bolt.Bucket, id64 uint64, system dump.System) error {
	val, err := MarshalGalaxyValue(system)
	if err != nil {
		return errors.Wrap(err, "unable to marshal system to JSON")
	}
	err = bucket.Put(MarshalGalaxyKey(id64), val)
	if err != nil {
		return errors.Wrap(err, "unable to insert dimension coordinates to bucket")
	}
	return nil
}

func insertName(bucket *bolt.Bucket, name string, id64 uint64) error {
	err := bucket.Put(MarshalName(name), MarshalGalaxyKey(id64))
	if err != nil {
		return errors.Wrap(err, "unable to insert dimension coordinates to bucket")
	}
	return nil
}

func (db *DB) SystemByName(name string) (dump.System, error) {
	var (
		err    error
		system dump.System
	)
	err = db.galaxy.View(func(tx *bolt.Tx) error {
		var err error

		k, id64Bytes := tx.Bucket(bucketNames).Cursor().Seek(MarshalName(name))
		if k == nil {
			return errors.Errorf("unable to find name: %s", name)
		}
		k, value := tx.Bucket(bucketSystems).Cursor().Seek(id64Bytes)
		if k == nil {
			return errors.Errorf("unable to find system by ID64: %d", UnmarshalGalaxyKey(id64Bytes))
		}
		system, err = UnmarshalGalaxyValue(value)
		if err != nil {
			return errors.Wrapf(err, "unable to unmarshal galaxy data for ID64: %d", UnmarshalGalaxyKey(id64Bytes))
		}
		return nil
	})
	if err != nil {
		return system, errors.Wrap(err, "unable to get system by name")
	}
	return system, nil
}
