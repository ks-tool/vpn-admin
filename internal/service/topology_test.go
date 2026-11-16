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
	"context"
	"fmt"
	"testing"
	"uuid"

	"github.com/ks-tool/vpn-admin/models"
)

type topologyGeocoderFunc func(context.Context, string) (float64, float64, error)

func (f topologyGeocoderFunc) Geocode(ctx context.Context, location string) (float64, float64, error) {
	return f(ctx, location)
}

func TestTopologyServerLocation(t *testing.T) {
	svc := newTestService(t)
	calls := 0
	svc.geocoder = topologyGeocoderFunc(func(_ context.Context, location string) (float64, float64, error) {
		calls++
		if location != "Amsterdam" {
			return 0, 0, fmt.Errorf("unknown location")
		}
		return 52.37, 4.9, nil
	})
	server, err := svc.CreateServer(ServerInput{Name: "NL", Info: models.ServerInfo{Location: "Amsterdam"}, SSH: models.SSHConfig{Host: "192.0.2.1", User: "root", Password: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateTopologyServer(server.ID.String(), TopologyServerInput{Name: server.Name, Address: server.SSH.Host, Location: server.Info.Location}); err != nil {
		t.Fatal(err)
	}
	topology, err := svc.GetTopology()
	if err != nil {
		t.Fatal(err)
	}
	got := topology.Servers[0]
	if got.Latitude == nil || *got.Latitude != 52.37 || got.Longitude == nil || *got.Longitude != 4.9 || calls != 1 {
		t.Fatalf("location was not resolved: %#v, calls=%d", got, calls)
	}
	if _, err := svc.GetTopology(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("resolved location was requested again: calls=%d", calls)
	}
	if _, err := svc.UpdateTopologyServer(server.ID.String(), TopologyServerInput{Name: "NL", Address: server.SSH.Host, Location: "Unknown"}); err != nil {
		t.Fatal(err)
	}
	topology, err = svc.GetTopology()
	if err != nil || topology.Servers[0].LocationError == "" {
		t.Fatalf("location error was not retained: %#v, %v", topology, err)
	}
}

func TestTopologyProjectsResourceInventoryAndPreservesPositions(t *testing.T) {
	svc := newTestService(t)
	serverID := uuid.New()
	interfaceID := uuid.New()
	server := &models.Server{ID: serverID, Name: "Existing server", Info: models.ServerInfo{Description: "Already in BoltDB", Location: "Yekaterinburg"}, SSH: models.SSHConfig{Host: "old.example.com", User: "root", Password: "secret"}}
	if err := svc.store.Servers().Set(server); err != nil {
		t.Fatal(err)
	}
	resource := &models.Resource{ID: interfaceID.String(), ServerID: serverID.String(), APIVersion: "awg.ks-tool.dev/v1", Kind: "Interface", Name: "awg0", Spec: []byte(`{"addr":"10.20.0.1/24"}`)}
	if err := svc.store.Resources(serverID).Set(resource); err != nil {
		t.Fatal(err)
	}
	svc.geocoder = topologyGeocoderFunc(func(context.Context, string) (float64, float64, error) { return 56.8389, 60.6057, nil })
	topology, err := svc.GetTopology()
	if err != nil || len(topology.Servers) != 1 || len(topology.Interfaces) != 1 {
		t.Fatalf("resource inventory is missing: %#v, %v", topology, err)
	}
	if topology.Interfaces[0].ID != interfaceID || topology.Interfaces[0].Name != "awg0" || topology.Interfaces[0].Address != "10.20.0.1/24" {
		t.Fatalf("unexpected resource projection: %#v", topology.Interfaces[0])
	}
	if err := svc.MoveTopologyNodes([]models.NodePosition{{ID: serverID, X: 321, Y: 654}, {ID: interfaceID, X: 123, Y: 456}}); err != nil {
		t.Fatal(err)
	}
	topology, err = svc.UpdateTopologyServer(serverID.String(), TopologyServerInput{Name: "Renamed", Address: "new.example.com", Description: "Updated", Location: "Yekaterinburg"})
	if err != nil || topology.Servers[0].X != 321 || topology.Servers[0].Y != 654 || topology.Interfaces[0].X != 123 {
		t.Fatalf("positions were not preserved: %#v, %v", topology, err)
	}
	stored, err := svc.store.Servers().Get(serverID)
	if err != nil || stored.Name != "Renamed" || stored.SSH.Host != "new.example.com" || stored.SSH.Password != "secret" {
		t.Fatalf("server record was not updated safely: %#v, %v", stored, err)
	}
}
