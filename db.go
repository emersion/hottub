package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"go.etcd.io/bbolt"
)

var installationsBucket = []byte("installations")

var ErrNotFound = fmt.Errorf("resource not found in DB")

type Installation struct {
	ID        int64     `json:"-"`
	Owner     string    `json:"owner"`
	Org       string    `json:"org,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	SrhtToken string    `json:"srht_token,omitempty"`
}

type DB struct {
	*bbolt.DB
}

func createDB(filename string) *DB {
	db, err := bbolt.Open(filename, 0600, nil)
	if err != nil {
		log.Fatalf("failed to open DB: %v", err)
	}

	err = db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(installationsBucket)
		return err
	})
	if err != nil {
		log.Fatalf("failed to init DB: %v", err)
	}

	return &DB{DB: db}
}

func (db *DB) GetInstallation(id int64) (*Installation, error) {
	var installation *Installation
	err := db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(installationsBucket).Get(marshalID(id))
		if b == nil {
			return ErrNotFound
		}

		installation = &Installation{ID: id}
		return json.Unmarshal(b, installation)
	})
	return installation, err
}

func (db *DB) StoreInstallation(installation *Installation) error {
	b, err := json.Marshal(installation)
	if err != nil {
		return err
	}
	return db.DB.Batch(func(tx *bbolt.Tx) error {
		return tx.Bucket(installationsBucket).Put(marshalID(installation.ID), b)
	})
}

func (db *DB) DeleteInstallation(id int64) error {
	return db.DB.Batch(func(tx *bbolt.Tx) error {
		return tx.Bucket(installationsBucket).Delete(marshalID(id))
	})
}

func marshalID(id int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(id))
	return b
}
