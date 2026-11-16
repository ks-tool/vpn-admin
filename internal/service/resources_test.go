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
	"fmt"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/ks-tool/vpn-admin/models"
	"github.com/ks-tool/vpn-admin/storage"
)

func TestProviderResourceLifecycleAndSchemaCache(t *testing.T) {
	svc, server, agent := newTestServerWithAgent(t)
	serverID := server.ID.String()
	manifest, err := svc.GetProviderManifest(serverID)
	if err != nil || len(manifest.Providers) != 1 {
		t.Fatalf("GetProviderManifest: %+v, %v", manifest, err)
	}
	definition, err := svc.GetProviderSchema(serverID, "test")
	if err != nil || len(definition.Resources) != 3 {
		t.Fatalf("GetProviderSchema: %+v, %v", definition, err)
	}
	if _, err := svc.GetProviderSchema(serverID, "test"); err != nil {
		t.Fatal(err)
	}
	if agent.schemaCalls != 0 {
		t.Fatalf("file schema cache missed: %d requests", agent.schemaCalls)
	}

	created, err := svc.PutProviderResource(serverID, "test", "Service", "public", json.RawMessage(`{"nested":{"enabled":true}}`))
	if err != nil || created.ID != "resource-id" || created.Generation != 1 {
		t.Fatalf("create: %+v, %v", created, err)
	}
	updated, err := svc.PutProviderResource(serverID, "test", "Service", "public", json.RawMessage(`{"nested":{"enabled":false}}`))
	if err != nil || updated.ID != created.ID || updated.Generation != 2 {
		t.Fatalf("update: %+v, %v", updated, err)
	}
	resources, err := svc.ListProviderResources(serverID, "test")
	if err != nil || len(resources) != 1 {
		t.Fatalf("list: %+v, %v", resources, err)
	}
	status, err := svc.GetProviderResourceStatus(serverID, "test", "Service", "public")
	if err != nil || status.ObservedGeneration != 2 {
		t.Fatalf("status: %+v, %v", status, err)
	}
	if err := svc.DeleteProviderResource(serverID, "test", "Service", "public"); err != nil {
		t.Fatal(err)
	}
	resources, err = svc.ListProviderResources(serverID, "test")
	if err != nil || len(resources) != 0 {
		t.Fatalf("resource was not deleted: %+v, %v", resources, err)
	}
}

func TestRefreshProviderManifestReplacesCachedProviders(t *testing.T) {
	svc, server, agent := newTestServerWithAgent(t)
	agent.awg = true
	manifest, err := svc.GetProviderManifest(server.ID.String())
	if err != nil || len(manifest.Providers) != 2 {
		t.Fatalf("initial manifest: %+v, %v", manifest, err)
	}
	agent.awg = false
	cached, err := svc.GetProviderManifest(server.ID.String())
	if err != nil || len(cached.Providers) != 2 {
		t.Fatalf("cached manifest: %+v, %v", cached, err)
	}
	refreshed, err := svc.RefreshProviderManifest(server.ID.String())
	if err != nil || len(refreshed.Providers) != 1 {
		t.Fatalf("refreshed manifest: %+v, %v", refreshed, err)
	}
}

func TestWaitForProviderLifecycle(t *testing.T) {
	svc, server, agent := newTestServerWithAgent(t)
	agent.awg = true
	if err := svc.waitForProvider(server.ID.String(), "awg", time.Second); err != nil {
		t.Fatal(err)
	}
	agent.awg = false
	if err := svc.waitForProvider(server.ID.String(), "awg", 0); err == nil {
		t.Fatal("unregistered provider was accepted")
	}
	if err := svc.waitForProviderRemoval(server.ID.String(), "awg", time.Second); err != nil {
		t.Fatal(err)
	}
	agent.providerErrors = map[string]string{"awg": "create TUN failed"}
	if err := svc.waitForProvider(server.ID.String(), "awg", 0); err == nil || !strings.Contains(err.Error(), "create TUN failed") {
		t.Fatalf("provider error was not reported: %v", err)
	}
}

func TestMigratedSecretUsesActionAndIsDeletedAfterAcceptance(t *testing.T) {
	svc, server, agent := newTestServerWithAgent(t)
	item := models.Resource{ID: "pending-peer", ServerID: server.ID.String(), APIVersion: "test/v1", Kind: "Service", Name: "alice-laptop", Generation: 1, Spec: json.RawMessage(`{"publicKey":"public"}`), Status: models.ResourceStatus{Conditions: []models.Condition{{Type: "Secrets", Status: models.ConditionFalse, Reason: "SecretMigrationPending"}}}}
	if err := svc.saveProviderResource(server.ID.String(), item); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.Secrets().Set(&models.ResourceSecret{ResourceID: item.ID, ServerID: server.ID.String(), PrivateKey: "private", PresharedKey: "psk"}); err != nil {
		t.Fatal(err)
	}
	resources, err := svc.ListProviderResources(server.ID.String(), "test")
	if err != nil || len(resources) != 1 {
		t.Fatalf("ListProviderResources: %+v, %v", resources, err)
	}
	if agent.actionCalls != 1 || !strings.Contains(string(agent.actionArguments), `"privateKey":"private"`) || strings.Contains(string(agent.actionArguments), "resourceId") {
		t.Fatalf("secret action arguments = %s, calls=%d", agent.actionArguments, agent.actionCalls)
	}
	if _, err := svc.store.Secrets().Get(item.ID); !storage.IsNotFound(err) {
		t.Fatalf("accepted migration secret was not deleted: %v", err)
	}
	if strings.Contains(string(resources[0].Spec), "private") {
		t.Fatal("private key leaked into resource spec")
	}
}

func TestServerAdoptionRequiresAWGProvider(t *testing.T) {
	svc, server, agent := newTestServerWithAgent(t)
	server.Unmanaged = true
	if err := svc.store.Servers().Set(server); err != nil {
		t.Fatal(err)
	}
	item := models.Resource{ID: uuid.New().String(), ServerID: server.ID.String(), APIVersion: "awg.ks-tool.dev/v1", Kind: "Interface", Name: "awg0", Generation: 1, Spec: json.RawMessage(`{"iface":"awg0"}`)}
	if err := svc.saveProviderResource(server.ID.String(), item); err != nil {
		t.Fatal(err)
	}
	status, err := svc.ServerAgentStatus(server.ID.String())
	if err != nil || status != models.AgentStatusUnmanaged {
		t.Fatalf("status without provider = %q, %v", status, err)
	}
	agent.awg = true
	status, err = svc.ServerAgentStatus(server.ID.String())
	if err != nil || status != models.AgentStatusOK {
		t.Fatalf("status after provider registration = %q, %v", status, err)
	}
	stored, err := svc.store.Servers().Get(server.ID)
	if err != nil || stored.Unmanaged {
		t.Fatalf("adoption did not complete: %+v, %v", stored, err)
	}
}

func TestResourcesAreSubmittedInterfaceFirst(t *testing.T) {
	svc, server, agent := newTestServerWithAgent(t)
	agent.awg = true
	for _, item := range []models.Resource{
		{ID: uuid.New().String(), ServerID: server.ID.String(), APIVersion: "awg.ks-tool.dev/v1", Kind: "Peer", Name: "peer", Spec: json.RawMessage(`{}`)},
		{ID: uuid.New().String(), ServerID: server.ID.String(), APIVersion: "awg.ks-tool.dev/v1", Kind: "Interface", Name: "awg0", Spec: json.RawMessage(`{"iface":"awg0"}`)},
	} {
		if err := svc.saveProviderResource(server.ID.String(), item); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.ListProviderResources(server.ID.String(), "awg"); err != nil {
		t.Fatal(err)
	}
	if len(agent.putOrder) != 2 || agent.putOrder[0] != "Interface" || agent.putOrder[1] != "Peer" {
		t.Fatalf("resource order = %v", agent.putOrder)
	}
}

func TestDeleteUserRevokesLinkedPeerResources(t *testing.T) {
	svc, server, agent := newTestServerWithAgent(t)
	user, err := svc.CreateUser(UserInput{Name: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	peer, err := svc.PutProviderResource(server.ID.String(), "test", "Peer", "alice-laptop", json.RawMessage(fmt.Sprintf(`{"userRef":%q}`, user.ID)))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteUser(user.ID.String()); err != nil {
		t.Fatal(err)
	}
	if _, exists := agent.resources["Peer/"+peer.Name]; exists {
		t.Fatal("peer resource survived user deletion")
	}
	if _, err := svc.store.Resources(server.ID).Get(peer.ID); !storage.IsNotFound(err) {
		t.Fatalf("local peer survived user deletion: %v", err)
	}
}

func TestDeleteProviderInterfaceRejectsDependencies(t *testing.T) {
	svc, server, _ := newTestServerWithAgent(t)
	interfaceID := uuid.New()
	item := models.Resource{ID: interfaceID.String(), ServerID: server.ID.String(), APIVersion: "test/v1", Kind: "Interface", Name: "test0", Generation: 1, Spec: json.RawMessage(`{"name":"test0"}`)}
	peer := models.Resource{ID: uuid.New().String(), ServerID: server.ID.String(), APIVersion: "test/v1", Kind: "Peer", Name: "peer", Generation: 1, Spec: json.RawMessage(fmt.Sprintf(`{"interfaceRef":%q}`, interfaceID.String()))}
	for _, resource := range []models.Resource{item, peer} {
		if err := svc.saveProviderResource(server.ID.String(), resource); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.DeleteProviderResource(server.ID.String(), "test", "Interface", item.Name); err == nil || !strings.Contains(err.Error(), "remove peers") {
		t.Fatalf("interface with peer was deleted: %v", err)
	}
	if err := svc.store.Resources(server.ID).Delete(peer.ID); err != nil {
		t.Fatal(err)
	}
	link := &models.Link{ID: uuid.New().String(), Source: models.LinkEndpoint{ResourceID: interfaceID.String()}, Target: models.LinkEndpoint{ResourceID: uuid.New().String()}}
	if err := svc.store.Links().Set(link); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteProviderResource(server.ID.String(), "test", "Interface", item.Name); err == nil || !strings.Contains(err.Error(), "remove provider links") {
		t.Fatalf("interface with link was deleted: %v", err)
	}
}

func TestPendingSecretRequiresProviderAction(t *testing.T) {
	svc, server, agent := newTestServerWithAgent(t)
	agent.awg = false
	item := models.Resource{ID: uuid.New().String(), ServerID: server.ID.String(), APIVersion: "awg.ks-tool.dev/v1", Kind: "Interface", Name: "awg0", Spec: []byte(`{}`), Status: models.ResourceStatus{Conditions: []models.Condition{{Type: "Secrets", Reason: "SecretMigrationPending"}}}}
	if err := svc.saveProviderResource(server.ID.String(), item); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.Secrets().Set(&models.ResourceSecret{ResourceID: item.ID, ServerID: server.ID.String(), PrivateKey: "private"}); err != nil {
		t.Fatal(err)
	}
	definition, err := svc.GetProviderSchema(server.ID.String(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.migrateProviderResourceSecret(server.ID.String(), definition, item); err == nil {
		t.Fatal("pending secret was accepted without importSecret action")
	}
	if _, err := svc.store.Secrets().Get(item.ID); err != nil {
		t.Fatalf("secret disappeared without provider confirmation: %v", err)
	}
}

func TestServerAdoptionSkipsEmptyEmbeddedPeerSecret(t *testing.T) {
	svc, server, agent := newTestServerWithAgent(t)
	server.Unmanaged = true
	if err := svc.store.Servers().Set(server); err != nil {
		t.Fatal(err)
	}
	agent.awg = true
	interfaceID := uuid.New()
	for _, resource := range []models.Resource{
		{ID: interfaceID.String(), ServerID: server.ID.String(), APIVersion: "awg.ks-tool.dev/v1", Kind: "Interface", Name: "awg0", Spec: []byte(`{"iface":"awg0"}`)},
		{ID: uuid.New().String(), ServerID: server.ID.String(), APIVersion: "awg.ks-tool.dev/v1", Kind: "Peer", Name: "embedded", Spec: []byte(`{"interfaceRef":"` + interfaceID.String() + `","publicKey":"public"}`)},
	} {
		if err := svc.saveProviderResource(server.ID.String(), resource); err != nil {
			t.Fatal(err)
		}
	}
	status, err := svc.ServerAgentStatus(server.ID.String())
	if err != nil || status != models.AgentStatusOK {
		t.Fatalf("embedded peer blocked adoption: status=%q, %v", status, err)
	}
	if agent.actionCalls != 0 {
		t.Fatalf("empty peer triggered %d secret imports", agent.actionCalls)
	}
}
