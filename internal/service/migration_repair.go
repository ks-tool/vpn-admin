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
	"encoding/json"
	"strings"

	"github.com/ks-tool/vpn-admin/models"
	"github.com/ks-tool/vpn-admin/storage"
)

type migratedPeerIdentity struct {
	interfaceRef string
	publicKey    string
	userRef      string
}

func (s *Service) repairMigratedTunnelPeers(serverID string) error {
	local, err := s.localProviderResources(serverID)
	if err != nil {
		return err
	}
	duplicates := make([]models.Resource, 0)
	for _, item := range local {
		if item.Kind == "Peer" && providerFromAPIVersion(item.APIVersion) == "awg" && strings.HasPrefix(item.Name, "Imported peer-") {
			duplicates = append(duplicates, item)
		}
	}
	if len(duplicates) == 0 {
		return nil
	}
	remote, err := s.remoteProviderResources(serverID, "awg")
	if err != nil {
		return err
	}
	for _, duplicate := range duplicates {
		identity, ok := decodeMigratedPeerIdentity(duplicate)
		if !ok || len(identity.userRef) > 0 {
			continue
		}
		canonical, ok := findRemoteTunnelPeer(remote, identity)
		if !ok {
			continue
		}
		secret, err := s.store.Secrets().Get(duplicate.ID)
		if err != nil && !storage.IsNotFound(err) {
			return err
		}
		if err == nil {
			moved := *secret
			moved.ResourceID = canonical.ID
			moved.ServerID = serverID
			if err := s.store.Secrets().Set(&moved); err != nil {
				return err
			}
		}
		if err := s.saveProviderResource(serverID, canonical); err != nil {
			return err
		}
		if err == nil {
			definition, schemaErr := s.GetProviderSchema(serverID, "awg")
			if schemaErr != nil {
				return schemaErr
			}
			if err := s.pushProviderResourceSecret(serverID, definition, canonical); err != nil {
				return err
			}
		}
		if err := s.DeleteProviderResource(serverID, "awg", duplicate.Kind, duplicate.Name); err != nil {
			return err
		}
	}
	return nil
}

func decodeMigratedPeerIdentity(item models.Resource) (migratedPeerIdentity, bool) {
	var spec struct {
		InterfaceRef string `json:"interfaceRef"`
		PublicKey    string `json:"publicKey"`
		UserRef      string `json:"userRef"`
	}
	if err := json.Unmarshal(item.Spec, &spec); err != nil || len(spec.InterfaceRef) == 0 || len(spec.PublicKey) == 0 {
		return migratedPeerIdentity{}, false
	}
	return migratedPeerIdentity{interfaceRef: spec.InterfaceRef, publicKey: spec.PublicKey, userRef: spec.UserRef}, true
}

func findRemoteTunnelPeer(resources []models.Resource, identity migratedPeerIdentity) (models.Resource, bool) {
	for _, item := range resources {
		if item.Kind != "Peer" || providerFromAPIVersion(item.APIVersion) != "awg" || !strings.HasPrefix(item.Name, "tunnel-") {
			continue
		}
		candidate, ok := decodeMigratedPeerIdentity(item)
		if ok && candidate.interfaceRef == identity.interfaceRef && candidate.publicKey == identity.publicKey {
			return item, true
		}
	}
	return models.Resource{}, false
}
