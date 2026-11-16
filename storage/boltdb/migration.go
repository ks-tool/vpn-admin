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
	"errors"
	"fmt"
	"strconv"
	"uuid"

	"github.com/ks-tool/vpn-admin/models"
	bolt "go.etcd.io/bbolt"
	"golang.org/x/crypto/bcrypt"
)

const currentSchemaVersion = 2

var (
	bktResources                 = []byte("resources")
	bktLinks                     = []byte("links")
	bktProviders                 = []byte("providers")
	bktSecrets                   = []byte("secrets")
	bktMeta                      = []byte("meta")
	schemaVersionKey             = []byte("schemaVersion")
	registrySourcesInitializeKey = []byte("registrySourcesInitialized")
)

func (db *BoltDB) initialize(path string) error {
	version, legacy, current, err := readSchemaVersion(db.db)
	if err != nil {
		return err
	}
	if version > currentSchemaVersion {
		return fmt.Errorf("database schema version %d is newer than supported version %d", version, currentSchemaVersion)
	}
	if legacy {
		return fmt.Errorf("database %s contains the legacy schema; run awg-migrate v2 with the original v1 database before starting vpn-admin", path)
	}
	if !current && version != 0 {
		return fmt.Errorf("database %s has unsupported schema version %d", path, version)
	}
	return db.db.Update(initializeSchema)
}

func readSchemaVersion(db *bolt.DB) (int, bool, bool, error) {
	version := 0
	legacy := false
	current := false
	err := db.View(func(tx *bolt.Tx) error {
		meta := tx.Bucket(bktMeta)
		if meta != nil && meta.Get(schemaVersionKey) != nil {
			parsed, err := strconv.Atoi(string(meta.Get(schemaVersionKey)))
			if err != nil {
				return err
			}
			version = parsed
		}
		legacy = tx.Bucket([]byte("interfaces")) != nil || tx.Bucket(bktServers) != nil && tx.Bucket(bktResources) == nil && tx.Bucket(bktLinks) == nil
		current = tx.Bucket(bktResources) != nil && tx.Bucket(bktLinks) != nil && tx.Bucket(bktSecrets) != nil
		return nil
	})
	return version, legacy, current, err
}

func initializeSchema(tx *bolt.Tx) error {
	for _, bucket := range [][]byte{bktServers, bktUsers, bktAgentSources, bktRegistrySources, bktTopology, bktAuth, bktResources, bktLinks, bktProviders, bktSecrets, bktMeta} {
		if _, err := tx.CreateBucketIfNotExists(bucket); err != nil {
			return err
		}
	}
	authBucket := tx.Bucket(bktAuth)
	if authBucket.Get(authRecordKey) == nil {
		hash, err := bcrypt.GenerateFromPassword([]byte(defaultAdminPassword), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		if err := put(authBucket, authRecordKey, &models.AuthCredentials{Username: defaultAdminUsername, PasswordHash: string(hash)}); err != nil {
			return err
		}
	}
	meta := tx.Bucket(bktMeta)
	if meta.Get(registrySourcesInitializeKey) == nil {
		sources := []*models.RegistrySource{
			{ID: uuid.New(), URL: "oci://ghcr.io/ks-tool/vpn-agent", Type: models.RegistrySourceAgent},
			{ID: uuid.New(), URL: "oci://ghcr.io/ks-tool/vpn-providers", Type: models.RegistrySourceProvider},
		}
		for _, source := range sources {
			if err := put(tx.Bucket(bktRegistrySources), idKey(source.ID), source); err != nil {
				return err
			}
		}
		if err := meta.Put(registrySourcesInitializeKey, []byte("1")); err != nil {
			return err
		}
	}
	return meta.Put(schemaVersionKey, []byte(strconv.Itoa(currentSchemaVersion)))
}

func (db *BoltDB) SchemaVersion() (int, error) {
	version := 0
	err := db.db.View(func(tx *bolt.Tx) error {
		value := tx.Bucket(bktMeta).Get(schemaVersionKey)
		if value == nil {
			return errors.New("schema version is missing")
		}
		parsed, err := strconv.Atoi(string(value))
		version = parsed
		return err
	})
	return version, err
}
