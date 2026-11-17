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

package service

import (
	"errors"
	"fmt"
	"uuid"

	"github.com/ks-tool/vpn-admin/models"
	"github.com/ks-tool/vpn-admin/storage"
	"github.com/rs/zerolog/log"
)

// SyncServer re-applies desired resources. Missing remote resources are created;
// remote-only resources are never treated as deletion intent during a resync.
func (s *Service) SyncServer(serverID string) error {
	unlock := s.lockResourceSync(serverID)
	defer unlock()
	log.Info().Str("server_id", serverID).Msg("syncing server configuration")
	if err := s.syncServer(serverID); err != nil {
		return err
	}
	log.Info().Str("server_id", serverID).Msg("server configuration synchronized")
	return nil
}

func (s *Service) syncServer(serverID string) error {
	id, err := uuid.Parse(serverID)
	if err != nil {
		return err
	}
	resources, err := s.store.Resources(id).List()
	if err != nil {
		return err
	}
	providers := make(map[string]bool)
	for _, resource := range resources {
		providers[providerFromAPIVersion(resource.APIVersion)] = true
	}
	for provider := range providers {
		if _, err := s.listProviderResources(serverID, provider, false); err != nil {
			return fmt.Errorf("sync provider %s: %w", provider, err)
		}
	}
	s.syncMonitoringState(id)
	s.syncProfilingState(id)
	return nil
}

func (s *Service) syncServerAndLinks(serverID string) error {
	unlock := s.lockResourceSync(serverID)
	defer unlock()
	server, err := s.agentServer(serverID)
	if err != nil {
		return err
	}
	if server.Unmanaged {
		return s.completeServerMigrationLocked(server)
	}
	log.Info().Str("server_id", serverID).Msg("syncing server configuration")
	if err := s.syncServer(serverID); err != nil {
		return err
	}
	log.Info().Str("server_id", serverID).Msg("server configuration synchronized")
	id, err := uuid.Parse(serverID)
	if err != nil {
		return err
	}
	resources, err := s.store.Resources(id).List()
	if err != nil {
		return err
	}
	providers := make(map[string]bool)
	for _, resource := range resources {
		providers[providerFromAPIVersion(resource.APIVersion)] = true
	}
	for provider := range providers {
		remote, err := s.listProviderResources(serverID, provider, false)
		if err != nil {
			return err
		}
		if err := s.waitProviderResourcesReady(serverID, provider, remote); err != nil {
			return err
		}
	}
	links, err := s.store.Links().List()
	if err != nil {
		return err
	}
	for _, link := range links {
		if link.Source.ServerID != serverID && link.Target.ServerID != serverID {
			continue
		}
		if _, err := s.ApplyProviderLink(link.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) syncTunnelledServers(servers []models.Server, tunnelErrs map[uuid.UUID]error) {
	for index := range servers {
		server := &servers[index]
		if !server.Agent.TLS.IsZero() {
			continue
		}
		if _, failed := tunnelErrs[server.ID]; failed {
			continue
		}
		if err := s.SyncServer(server.ID.String()); err != nil {
			log.Warn().Err(err).Str("server_id", server.ID.String()).Msg("failed to sync server after opening tunnel")
		}
	}
}

func (s *Service) findServerByResource(resourceID string) (*models.Server, error) {
	servers, err := s.store.Servers().List()
	if err != nil {
		return nil, err
	}
	for index := range servers {
		_, err := s.store.Resources(servers[index].ID).Get(resourceID)
		if err == nil {
			return &servers[index], nil
		}
		if !storage.IsNotFound(err) {
			return nil, err
		}
	}
	return nil, errors.New("no server owns this resource")
}
