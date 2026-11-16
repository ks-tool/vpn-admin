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
	"uuid"

	"github.com/ks-tool/vpn-admin/models"
	"github.com/ks-tool/vpn-admin/storage"
	bolt "go.etcd.io/bbolt"
)

type servers struct{ db *bolt.DB }

func (s *servers) List() ([]models.Server, error) {
	return list[models.Server](s.db, bktServers)
}

func (s *servers) Get(id uuid.UUID) (*models.Server, error) {
	var server models.Server
	err := s.db.View(func(tx *bolt.Tx) error {
		var err error
		server, err = get[models.Server](tx.Bucket(bktServers), idKey(id))
		return err
	})
	return &server, err
}

func (s *servers) Set(server *models.Server) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return put(tx.Bucket(bktServers), idKey(server.ID), server)
	})
}

func (s *servers) Delete(id uuid.UUID) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bktServers)
		if bucket.Get(idKey(id)) == nil {
			return storage.ErrNotFound
		}
		return bucket.Delete(idKey(id))
	})
}
