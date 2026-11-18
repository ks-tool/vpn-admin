/*
  Copyright © 2026 Alexey Shulutkov <github@shulutkov.ru>

  Licensed under the Apache License, Version 2.0 (the "License");
  you may not use this file except in compliance with the License.
  You may obtain a copy of the License at

  	http://www.apache.org/licenses/LICENSE-2.0

  Unless required by applicable law or agreed to in writing, software
  distributed under the License is distributed on an "AS IS" BASIS,
  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
  See the License for the specific language governing permissions and
  limitations under the License.
*/

package boltdb

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestOpenRequiresResourceOnlySchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	raw, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Update(func(tx *bolt.Tx) error {
		for _, name := range []string{"servers", "users", "interfaces"} {
			if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("legacy database was opened without offline migration")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("legacy database was modified: %v", err)
	}
}

func TestNewDatabaseHasOnlyCurrentBuckets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if version, err := db.SchemaVersion(); err != nil || version != currentSchemaVersion {
		t.Fatalf("schema version = %d, %v", version, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	if err := raw.View(func(tx *bolt.Tx) error {
		if tx.Bucket([]byte("interfaces")) != nil {
			t.Fatal("legacy interfaces bucket was created")
		}
		for _, name := range []string{"resources", "links", "secrets", "servers", "users"} {
			if tx.Bucket([]byte(name)) == nil {
				t.Fatalf("current bucket %s is missing", name)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
