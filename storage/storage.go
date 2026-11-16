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

package storage

import (
	"io"
	"uuid"

	"github.com/ks-tool/vpn-admin/models"
)

type Storage interface {
	io.Closer
	Users() Users
	Servers() Servers
	Auth() Auth
	AgentSources() AgentSources
	RegistrySources() RegistrySources
	Topology() Topology
	Resources(serverID uuid.UUID) Resources
	Links() Links
	Providers() Providers
	Secrets() Secrets
	Backup(w io.Writer) error
}

type AgentSources interface {
	List() ([]models.AgentSource, error)
	Get(id uuid.UUID) (*models.AgentSource, error)
	Set(src *models.AgentSource) error
	Delete(id uuid.UUID) error
}

type RegistrySources interface {
	List() ([]models.RegistrySource, error)
	Get(id uuid.UUID) (*models.RegistrySource, error)
	Set(src *models.RegistrySource) error
	Delete(id uuid.UUID) error
}

type Auth interface {
	Get() (*models.AuthCredentials, error)
	Set(creds *models.AuthCredentials) error
}

type Secrets interface {
	Get(resourceID string) (*models.ResourceSecret, error)
	Set(secret *models.ResourceSecret) error
	Delete(resourceID string) error
}

type Servers interface {
	List() ([]models.Server, error)
	Get(id uuid.UUID) (*models.Server, error)
	Set(srv *models.Server) error
	Delete(id uuid.UUID) error
}

type Users interface {
	List() ([]models.User, error)
	Get(id uuid.UUID) (*models.User, error)
	Set(user *models.User) error
	Delete(id uuid.UUID) error
}
