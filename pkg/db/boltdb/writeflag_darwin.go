package boltdb

import (
	"syscall"

	bolt "go.etcd.io/bbolt"
)

// setWriteFlag sets tx.WriteFlag on certain OSs.
func setWriteFlag(tx *bolt.Tx) {
	tx.WriteFlag = syscall.F_NOCACHE
}
