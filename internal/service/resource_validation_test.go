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
	"encoding/json"
	"testing"
	"uuid"

	"github.com/ks-tool/vpn-admin/models"
)

func TestPeerOfDisabledUserIsDisabledByBackend(t *testing.T) {
	svc, server, agent := newTestServerWithAgent(t)
	user, err := svc.CreateUser(UserInput{Name: "alice", Disabled: true})
	if err != nil {
		t.Fatal(err)
	}
	peer, err := svc.PutProviderResource(server.ID.String(), "test", "Peer", "alice-laptop", json.RawMessage(`{"userRef":"`+user.ID.String()+`","disabled":false}`))
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Disabled bool `json:"disabled"`
	}
	if err := json.Unmarshal(peer.Spec, &spec); err != nil || !spec.Disabled {
		t.Fatalf("peer of disabled user is active: %+v, %v", peer, err)
	}
	remote := agent.resources["Peer/"+peer.Name]
	if err := json.Unmarshal(remote.Spec, &spec); err != nil || !spec.Disabled {
		t.Fatalf("provider received an active peer: %+v, %v", remote, err)
	}
}

func TestInterfaceRenameIsRejectedWithoutChangingResource(t *testing.T) {
	svc, server, agent := newTestServerWithAgent(t)
	created, err := svc.PutProviderResource(server.ID.String(), "test", "Interface", "test0", json.RawMessage(`{"name":"test0","addr":"10.0.0.1/24"}`))
	if err != nil {
		t.Fatal(err)
	}
	before := append(json.RawMessage(nil), created.Spec...)
	_, err = svc.PutProviderResource(server.ID.String(), "test", "Interface", "test0", json.RawMessage(`{"name":"renamed","addr":"10.0.0.2/24"}`))
	if err == nil {
		t.Fatal("interface identity rename was accepted")
	}
	stored, err := svc.store.Resources(server.ID).Get(created.ID)
	if err != nil || string(stored.Spec) != string(before) {
		t.Fatalf("resource changed after rejected rename: %+v, %v", stored, err)
	}
	if len(agent.putOrder) != 1 {
		t.Fatalf("rejected rename reached provider: %v", agent.putOrder)
	}
}

func TestTopologyInterfaceSpecIncludesListenPort(t *testing.T) {
	definition := models.ResourceDefinition{SpecSchema: json.RawMessage(`{"type":"object","properties":{"iface":{"type":"string"},"addr":{"type":"string"},"listen":{"type":"integer"}}}`)}
	raw, err := interfaceSpec(definition, NetworkInterfaceInput{Name: "awg0", Address: "10.0.0.1/24"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Listen *uint16 `json:"listen"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil || spec.Listen == nil || *spec.Listen != 51820 {
		t.Fatalf("listen port is missing: %s, %v", raw, err)
	}
}

func TestLocationChangeThroughServerRecordInvalidatesCoordinates(t *testing.T) {
	svc := newTestService(t)
	serverID := uuid.New()
	latitude, longitude := 52.37, 4.9
	server := &models.Server{ID: serverID, Name: "server", Info: models.ServerInfo{Location: "Amsterdam"}}
	if err := svc.store.Servers().Set(server); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.Topology().Set(&models.Topology{Servers: []models.TopologyServer{{ID: serverID, Location: "Amsterdam", Latitude: &latitude, Longitude: &longitude}}, Interfaces: []models.NetworkInterface{}}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	svc.geocoder = topologyGeocoderFunc(func(_ context.Context, location string) (float64, float64, error) {
		calls++
		if location != "Berlin" {
			t.Fatalf("stale location was reused: %q", location)
		}
		return 52.52, 13.405, nil
	})
	server.Info.Location = "Berlin"
	if err := svc.store.Servers().Set(server); err != nil {
		t.Fatal(err)
	}
	topology, err := svc.GetTopology()
	if err != nil {
		t.Fatal(err)
	}
	got := topology.Servers[0]
	if calls != 1 || got.Latitude == nil || *got.Latitude != 52.52 || got.Longitude == nil || *got.Longitude != 13.405 {
		t.Fatalf("stale coordinates survived location change: %+v, calls=%d", got, calls)
	}
}

func TestTopologyRenameIsRejectedBeforeProviderCall(t *testing.T) {
	svc := newTestService(t)
	serverID := uuid.New()
	interfaceID := uuid.New()
	if err := svc.store.Servers().Set(&models.Server{ID: serverID, Name: "server"}); err != nil {
		t.Fatal(err)
	}
	resource := &models.Resource{ID: interfaceID.String(), ServerID: serverID.String(), APIVersion: "test/v1", Kind: "Interface", Name: "test0", Spec: []byte(`{"name":"test0"}`)}
	if err := svc.store.Resources(serverID).Set(resource); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateNetworkInterface(interfaceID.String(), NetworkInterfaceInput{Name: "renamed", Address: "10.0.0.1/24"}); err == nil {
		t.Fatal("topology rename was accepted")
	}
	stored, err := svc.store.Resources(serverID).Get(resource.ID)
	if err != nil || string(stored.Spec) != string(resource.Spec) {
		t.Fatalf("resource changed after rejected topology rename: %+v, %v", stored, err)
	}
}
