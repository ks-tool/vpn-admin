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
	"testing"
	"uuid"

	"github.com/ks-tool/vpn-admin/models"
	"github.com/ks-tool/vpn-admin/storage"
)

func TestServerAdoptionMergesDuplicateMigratedTunnelPeer(t *testing.T) {
	svc, server, agent := newTestServerWithAgent(t)
	server.Unmanaged = true
	if err := svc.store.Servers().Set(server); err != nil {
		t.Fatal(err)
	}
	agent.awg = true
	interfaceID := uuid.New()
	tunnelName := "tunnel-" + interfaceID.String()
	tunnel := models.Resource{
		ID: uuid.New().String(), ServerID: server.ID.String(), APIVersion: "awg.ks-tool.dev/v1", Kind: "Peer", Name: tunnelName,
		Generation: 1, Spec: json.RawMessage(`{"name":"Tunnel peer","interfaceRef":"` + interfaceID.String() + `","publicKey":"remote-public"}`),
		Status: models.ResourceStatus{ObservedGeneration: 1, Phase: models.ResourceReady},
	}
	agent.resources["Peer/"+tunnelName] = tunnel
	imported := models.Resource{
		ID: uuid.New().String(), ServerID: server.ID.String(), APIVersion: "awg.ks-tool.dev/v1", Kind: "Peer", Name: "Imported peer-9360f848",
		Generation: 1, Spec: json.RawMessage(`{"name":"Imported peer","interfaceRef":"` + interfaceID.String() + `","publicKey":"remote-public"}`),
		Status: models.ResourceStatus{Phase: models.ResourcePending, Conditions: []models.Condition{{Type: "Secrets", Reason: "SecretMigrationPending"}}},
	}
	iface := models.Resource{ID: interfaceID.String(), ServerID: server.ID.String(), APIVersion: "awg.ks-tool.dev/v1", Kind: "Interface", Name: "awg0", Generation: 1, Spec: json.RawMessage(`{"iface":"awg0"}`)}
	for _, item := range []models.Resource{iface, imported} {
		if err := svc.saveProviderResource(server.ID.String(), item); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.store.Secrets().Set(&models.ResourceSecret{ResourceID: imported.ID, ServerID: server.ID.String(), PresharedKey: "tunnel-psk"}); err != nil {
		t.Fatal(err)
	}

	if err := svc.completeServerMigration(server); err != nil {
		t.Fatalf("completeServerMigration: %v", err)
	}
	if _, exists := agent.resources["Peer/"+imported.Name]; exists {
		t.Fatal("duplicate imported peer remained on the agent")
	}
	if _, exists := agent.resources["Peer/"+tunnelName]; !exists {
		t.Fatal("canonical tunnel peer disappeared from the agent")
	}
	if _, err := svc.store.Resources(server.ID).Get(imported.ID); !storage.IsNotFound(err) {
		t.Fatalf("duplicate imported peer remained in admin DB: %v", err)
	}
	secret, err := svc.store.Secrets().Get(tunnel.ID)
	if err != nil || secret.PresharedKey != "tunnel-psk" {
		t.Fatalf("tunnel PSK was not assigned to the canonical resource: %+v, %v", secret, err)
	}
	if agent.actionCalls == 0 || !strings.Contains(string(agent.actionArguments), `"presharedKey":"tunnel-psk"`) {
		t.Fatalf("tunnel PSK was not imported: calls=%d arguments=%s", agent.actionCalls, agent.actionArguments)
	}
}
