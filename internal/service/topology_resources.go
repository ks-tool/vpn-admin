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
	"sort"
	"uuid"

	"github.com/ks-tool/vpn-admin/models"
	"github.com/ks-tool/vpn-admin/storage"
)

func (s *Service) loadTopology() (*models.Topology, error) {
	stored, err := s.store.Topology().Get()
	if err != nil {
		return nil, err
	}
	servers, err := s.store.Servers().List()
	if err != nil {
		return nil, err
	}
	result := &models.Topology{Servers: make([]models.TopologyServer, 0, len(servers)), Interfaces: []models.NetworkInterface{}}
	for _, item := range servers {
		server := models.TopologyServer{ID: item.ID, Name: item.Name, Address: item.SSH.Host, Description: item.Info.Description, Location: item.Info.Location, X: 80, Y: 80 + float64(len(result.Servers))*150}
		for _, previous := range stored.Servers {
			if previous.ID == item.ID {
				server.X, server.Y = previous.X, previous.Y
				if previous.Location == item.Info.Location {
					server.Latitude, server.Longitude, server.CoordinatesManual = previous.Latitude, previous.Longitude, previous.CoordinatesManual
					server.LocationError = previous.LocationError
				}
				break
			}
		}
		result.Servers = append(result.Servers, server)
		resources, err := s.store.Resources(item.ID).List()
		if err != nil {
			return nil, err
		}
		for _, resource := range resources {
			if resource.Kind != "Interface" {
				continue
			}
			id, err := uuid.Parse(resource.ID)
			if err != nil {
				return nil, err
			}
			iface := models.NetworkInterface{ID: id, ServerID: item.ID, Name: resource.Name, Address: resourceAddress(resource), X: 420, Y: 80 + float64(len(result.Interfaces))*120}
			for _, previous := range stored.Interfaces {
				if previous.ID == id {
					iface.X, iface.Y = previous.X, previous.Y
					break
				}
			}
			result.Interfaces = append(result.Interfaces, iface)
		}
	}
	return result, nil
}

func resourceAddress(resource models.Resource) string {
	var spec map[string]any
	if json.Unmarshal(resource.Spec, &spec) != nil {
		return ""
	}
	for _, field := range []string{"addr", "address", "listen"} {
		if value, ok := spec[field].(string); ok {
			return value
		}
	}
	return ""
}

func (s *Service) updateInventoryServer(id uuid.UUID, in TopologyServerInput) (bool, error) {
	server, err := s.store.Servers().Get(id)
	if storage.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	server.Name = in.Name
	server.SSH.Host = in.Address
	server.Info.Description = in.Description
	server.Info.Location = in.Location
	return true, s.store.Servers().Set(server)
}

func (s *Service) deleteInventoryServer(id uuid.UUID) (bool, error) {
	_, err := s.store.Servers().Get(id)
	if storage.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	return true, s.DeleteServer(id.String())
}

func (s *Service) createResourceInterface(serverID uuid.UUID, in NetworkInterfaceInput) (*models.Resource, bool, error) {
	if _, err := s.store.Servers().Get(serverID); storage.IsNotFound(err) {
		return nil, false, nil
	} else if err != nil {
		return nil, true, err
	}
	manifest, err := s.GetProviderManifest(serverID.String())
	if err != nil {
		return nil, true, err
	}
	providers := append([]models.ProviderDefinition(nil), manifest.Providers...)
	sort.SliceStable(providers, func(i, j int) bool { return providers[i].Name < providers[j].Name })
	for _, provider := range providers {
		definition, err := s.GetProviderSchema(serverID.String(), provider.Name)
		if err != nil {
			return nil, true, err
		}
		for _, resource := range definition.Resources {
			if resource.Kind != "Interface" {
				continue
			}
			spec, err := interfaceSpec(resource, in, nil)
			if err != nil {
				return nil, true, err
			}
			created, err := s.PutProviderResource(serverID.String(), provider.Name, resource.Kind, in.Name, spec)
			return created, true, err
		}
	}
	return nil, true, invalidInput("no provider on server %q supports interfaces", serverID)
}

func (s *Service) updateResourceInterface(serverID, ifaceID uuid.UUID, in NetworkInterfaceInput) (*models.Resource, bool, error) {
	resource, err := s.store.Resources(serverID).Get(ifaceID.String())
	if storage.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, err
	}
	if in.Name != resource.Name {
		return nil, true, invalidInput("renaming an interface is not supported; create a new resource instead")
	}
	provider := providerFromAPIVersion(resource.APIVersion)
	definition, err := s.GetProviderSchema(serverID.String(), provider)
	if err != nil {
		return nil, true, err
	}
	for _, candidate := range definition.Resources {
		if candidate.Kind == resource.Kind {
			spec, err := interfaceSpec(candidate, in, resource.Spec)
			if err != nil {
				return nil, true, err
			}
			updated, err := s.PutProviderResource(serverID.String(), provider, resource.Kind, resource.Name, spec)
			return updated, true, err
		}
	}
	return nil, true, invalidInput("provider %q no longer supports interfaces", provider)
}

func interfaceSpec(definition models.ResourceDefinition, in NetworkInterfaceInput, current json.RawMessage) (json.RawMessage, error) {
	var spec map[string]any
	if len(current) > 0 {
		if err := json.Unmarshal(current, &spec); err != nil {
			return nil, err
		}
	} else {
		spec = make(map[string]any)
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(definition.SpecSchema, &schema); err != nil {
		return nil, err
	}
	for _, field := range []string{"name", "iface"} {
		if _, ok := schema.Properties[field]; ok {
			spec[field] = in.Name
		}
	}
	for _, field := range []string{"address", "addr"} {
		if _, ok := schema.Properties[field]; ok {
			spec[field] = in.Address
		}
	}
	if len(current) == 0 {
		if _, ok := schema.Properties["listen"]; ok {
			spec["listen"] = 51820
		}
	}
	return json.Marshal(spec)
}

func (s *Service) deleteResourceInterface(serverID, ifaceID uuid.UUID) (bool, error) {
	resource, err := s.store.Resources(serverID).Get(ifaceID.String())
	if storage.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	return true, s.DeleteProviderResource(serverID.String(), providerFromAPIVersion(resource.APIVersion), resource.Kind, resource.Name)
}
