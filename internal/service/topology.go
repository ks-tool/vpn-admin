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
	"strings"
	"uuid"

	"github.com/ks-tool/vpn-admin/models"
)

type TopologyServerInput struct {
	Name        string                  `json:"name"`
	Address     string                  `json:"address"`
	Description string                  `json:"description"`
	Location    string                  `json:"location"`
	Latitude    *float64                `json:"latitude"`
	Longitude   *float64                `json:"longitude"`
	Interfaces  []NetworkInterfaceInput `json:"interfaces"`
}

type NetworkInterfaceInput struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

func (s *Service) GetTopology() (*models.Topology, error) {
	debugOp("GetTopology").Msg("getting VPN topology")
	s.topologyMu.Lock()
	defer s.topologyMu.Unlock()
	topology, err := s.loadTopology()
	if err != nil {
		return nil, err
	}
	return topology, s.resolveTopologyLocations(topology)
}

func topologyServerIndex(topology *models.Topology, id uuid.UUID) int {
	for index := range topology.Servers {
		if topology.Servers[index].ID == id {
			return index
		}
	}
	return -1
}

func networkInterfaceIndex(topology *models.Topology, id uuid.UUID) int {
	for index := range topology.Interfaces {
		if topology.Interfaces[index].ID == id {
			return index
		}
	}
	return -1
}

func parseTopologyID(value, field string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil(), invalidInput("invalid %s", field)
	}
	return id, nil
}

func (s *Service) UpdateTopologyServer(id string, in TopologyServerInput) (*models.Topology, error) {
	serverID, err := parseTopologyID(id, "server id")
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(in.Name)) == 0 {
		return nil, invalidInput("server name is required")
	}
	location := strings.TrimSpace(in.Location)
	if len(location) == 0 {
		if err := validateServerCoordinates(in.Latitude, in.Longitude); err != nil {
			return nil, err
		}
	}
	s.topologyMu.Lock()
	defer s.topologyMu.Unlock()
	topology, err := s.loadTopology()
	if err != nil {
		return nil, err
	}
	index := topologyServerIndex(topology, serverID)
	if index < 0 {
		return nil, invalidInput("topology server not found")
	}
	if _, err := s.updateInventoryServer(serverID, in); err != nil {
		return nil, err
	}
	server := &topology.Servers[index]
	server.Name = strings.TrimSpace(in.Name)
	server.Address = strings.TrimSpace(in.Address)
	server.Description = strings.TrimSpace(in.Description)
	if server.Location != location {
		server.Latitude = nil
		server.Longitude = nil
	}
	server.Location = location
	server.LocationError = ""
	server.CoordinatesManual = false
	if len(location) == 0 {
		server.Latitude = in.Latitude
		server.Longitude = in.Longitude
		server.CoordinatesManual = in.Latitude != nil
	}
	return topology, s.store.Topology().Set(topology)
}

func (s *Service) DeleteTopologyServer(id string) error {
	serverID, err := parseTopologyID(id, "server id")
	if err != nil {
		return err
	}
	s.topologyMu.Lock()
	defer s.topologyMu.Unlock()
	topology, err := s.loadTopology()
	if err != nil {
		return err
	}
	index := topologyServerIndex(topology, serverID)
	if index < 0 {
		return invalidInput("topology server not found")
	}
	if _, err := s.deleteInventoryServer(serverID); err != nil {
		return err
	}
	removed := make(map[uuid.UUID]bool)
	interfaces := topology.Interfaces[:0]
	for _, iface := range topology.Interfaces {
		if iface.ServerID == serverID {
			removed[iface.ID] = true
			continue
		}
		interfaces = append(interfaces, iface)
	}
	topology.Interfaces = interfaces
	topology.Servers = append(topology.Servers[:index], topology.Servers[index+1:]...)
	return s.store.Topology().Set(topology)
}

func (s *Service) CreateNetworkInterface(serverID string, in NetworkInterfaceInput) (*models.Topology, error) {
	id, err := parseTopologyID(serverID, "server id")
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(in.Name)) == 0 {
		return nil, invalidInput("interface name is required")
	}
	s.topologyMu.Lock()
	defer s.topologyMu.Unlock()
	topology, err := s.loadTopology()
	if err != nil {
		return nil, err
	}
	if topologyServerIndex(topology, id) < 0 {
		return nil, invalidInput("topology server not found")
	}
	for _, iface := range topology.Interfaces {
		if iface.ServerID == id && iface.Name == strings.TrimSpace(in.Name) {
			return nil, invalidInput("interface %q already exists on this server", in.Name)
		}
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Address = strings.TrimSpace(in.Address)
	created, _, err := s.createResourceInterface(id, in)
	if err != nil {
		return nil, err
	}
	resourceID, err := uuid.Parse(created.ID)
	if err != nil {
		return nil, err
	}
	topology.Interfaces = append(topology.Interfaces, models.NetworkInterface{ID: resourceID, ServerID: id, Name: created.Name, Address: resourceAddress(*created), X: 420, Y: 80 + float64(len(topology.Interfaces))*120})
	return topology, s.store.Topology().Set(topology)
}

func (s *Service) UpdateNetworkInterface(id string, in NetworkInterfaceInput) (*models.Topology, error) {
	ifaceID, err := parseTopologyID(id, "interface id")
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(in.Name)) == 0 {
		return nil, invalidInput("interface name is required")
	}
	s.topologyMu.Lock()
	defer s.topologyMu.Unlock()
	topology, err := s.loadTopology()
	if err != nil {
		return nil, err
	}
	index := networkInterfaceIndex(topology, ifaceID)
	if index < 0 {
		return nil, invalidInput("network interface not found")
	}
	iface := &topology.Interfaces[index]
	for _, other := range topology.Interfaces {
		if other.ID != ifaceID && other.ServerID == iface.ServerID && other.Name == strings.TrimSpace(in.Name) {
			return nil, invalidInput("interface %q already exists on this server", in.Name)
		}
	}
	updated, _, err := s.updateResourceInterface(iface.ServerID, ifaceID, NetworkInterfaceInput{Name: strings.TrimSpace(in.Name), Address: strings.TrimSpace(in.Address)})
	if err != nil {
		return nil, err
	}
	iface.Name = updated.Name
	iface.Address = resourceAddress(*updated)
	return topology, s.store.Topology().Set(topology)
}

func (s *Service) DeleteNetworkInterface(id string) error {
	ifaceID, err := parseTopologyID(id, "interface id")
	if err != nil {
		return err
	}
	s.topologyMu.Lock()
	defer s.topologyMu.Unlock()
	topology, err := s.loadTopology()
	if err != nil {
		return err
	}
	index := networkInterfaceIndex(topology, ifaceID)
	if index < 0 {
		return invalidInput("network interface not found")
	}
	iface := topology.Interfaces[index]
	if _, err := s.deleteResourceInterface(iface.ServerID, ifaceID); err != nil {
		return err
	}
	topology.Interfaces = append(topology.Interfaces[:index], topology.Interfaces[index+1:]...)
	return s.store.Topology().Set(topology)
}

func (s *Service) MoveTopologyNodes(positions []models.NodePosition) error {
	s.topologyMu.Lock()
	defer s.topologyMu.Unlock()
	topology, err := s.loadTopology()
	if err != nil {
		return err
	}
	for _, position := range positions {
		if index := topologyServerIndex(topology, position.ID); index >= 0 {
			topology.Servers[index].X = position.X
			topology.Servers[index].Y = position.Y
		}
		if index := networkInterfaceIndex(topology, position.ID); index >= 0 {
			topology.Interfaces[index].X = position.X
			topology.Interfaces[index].Y = position.Y
		}
	}
	return s.store.Topology().Set(topology)
}
