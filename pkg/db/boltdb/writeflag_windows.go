package boltdb

import (
	bolt "go.etcd.io/bbolt"
)

// setWriteFlag sets tx.WriteFlag on certain OSs.
func setWriteFlag(tx *bolt.Tx) {
}
