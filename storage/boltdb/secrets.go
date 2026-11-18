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
	"github.com/ks-tool/vpn-admin/models"
	"github.com/ks-tool/vpn-admin/storage"
	bolt "go.etcd.io/bbolt"
)

type secrets struct{ db *bolt.DB }

func (db *BoltDB) Secrets() storage.Secrets { return &secrets{db: db.db} }

func (s *secrets) Get(resourceID string) (*models.ResourceSecret, error) {
	var secret models.ResourceSecret
	err := s.db.View(func(tx *bolt.Tx) error {
		var err error
		secret, err = get[models.ResourceSecret](tx.Bucket(bktSecrets), []byte(resourceID))
		return err
	})
	return &secret, err
}

func (s *secrets) Set(secret *models.ResourceSecret) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return put(tx.Bucket(bktSecrets), []byte(secret.ResourceID), secret)
	})
}

func (s *secrets) Delete(resourceID string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bktSecrets).Delete([]byte(resourceID))
	})
}
