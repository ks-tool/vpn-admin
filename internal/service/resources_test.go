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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/ks-tool/vpn-admin/models"
	"github.com/ks-tool/vpn-admin/storage"
	awgmodels "github.com/ks-tool/vpn-agent/v2/providers/awg/models"
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

func TestProviderSchemaRefetchesIncompleteManifest(t *testing.T) {
	svc, server, agent := newTestServerWithAgent(t)
	record := &models.ProviderRecord{ServerID: server.ID.String(), Installed: true, Definition: models.ProviderDefinition{Name: "test", Version: "1", Resources: []models.ResourceDefinition{{APIVersion: "test/v1", Kind: "Service", SpecSchema: json.RawMessage(`{"type":"object"}`)}}}}
	if err := svc.store.Providers().Set(record); err != nil {
		t.Fatal(err)
	}
	definition, err := svc.GetProviderSchema(server.ID.String(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(definition.Resources) == 0 || len(definition.Resources[0].Actions) == 0 || len(definition.Resources[0].SchemaHash) == 0 {
		t.Fatalf("incomplete schema was returned: %+v", definition)
	}
	agent.mu.Lock()
	calls := agent.schemaCalls
	agent.mu.Unlock()
	if calls == 0 {
		t.Fatal("provider schema was not fetched from the agent")
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

func TestSyncedSecretUsesActionAndRemainsInAdminDB(t *testing.T) {
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
	secret, err := svc.store.Secrets().Get(item.ID)
	if err != nil || secret.PrivateKey != "private" || secret.PresharedKey != "psk" {
		t.Fatalf("synced secret was not preserved: %+v, %v", secret, err)
	}
	if strings.Contains(string(resources[0].Spec), "private") {
		t.Fatal("private key leaked into resource spec")
	}
}

func TestSecretSyncUsesLiveSchemaWithStaleCachedActions(t *testing.T) {
	for _, kind := range []string{"Interface", "Peer"} {
		t.Run(kind, func(t *testing.T) {
			svc, server, agent := newTestServerWithAgent(t)
			record := &models.ProviderRecord{ServerID: server.ID.String(), Installed: true, Definition: models.ProviderDefinition{Name: "test", Version: "1", Resources: []models.ResourceDefinition{{APIVersion: "test/v1", Kind: kind, SpecSchema: json.RawMessage(`{"type":"object"}`), SchemaHash: "hash"}}}}
			if err := svc.store.Providers().Set(record); err != nil {
				t.Fatal(err)
			}
			item := models.Resource{ID: "pending-" + kind, ServerID: server.ID.String(), APIVersion: "test/v1", Kind: kind, Name: "resource", Generation: 1, Spec: json.RawMessage(`{}`), Status: models.ResourceStatus{Conditions: []models.Condition{{Type: "Secrets", Reason: "SecretMigrationPending"}}}}
			if err := svc.saveProviderResource(server.ID.String(), item); err != nil {
				t.Fatal(err)
			}
			if err := svc.store.Secrets().Set(&models.ResourceSecret{ResourceID: item.ID, ServerID: server.ID.String(), PrivateKey: "private"}); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.ListProviderResources(server.ID.String(), "test"); err != nil {
				t.Fatal(err)
			}
			if agent.actionCalls != 1 || !strings.Contains(string(agent.actionArguments), `"privateKey":"private"`) {
				t.Fatalf("live schema did not restore secret: calls=%d, arguments=%s", agent.actionCalls, agent.actionArguments)
			}
		})
	}
}

func TestProviderPendingFlagRestoresSecretFromAdminDB(t *testing.T) {
	svc, server, agent := newTestServerWithAgent(t)
	item := models.Resource{ID: "pending-peer", ServerID: server.ID.String(), APIVersion: "test/v1", Kind: "Service", Name: "alice-laptop", Generation: 1, Spec: json.RawMessage(`{"publicKey":"public"}`), Status: models.ResourceStatus{Provider: json.RawMessage(`{"secretMigrationPending":true}`)}}
	if err := svc.saveProviderResource(server.ID.String(), item); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.Secrets().Set(&models.ResourceSecret{ResourceID: item.ID, ServerID: server.ID.String(), PrivateKey: "private", PresharedKey: "psk"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListProviderResources(server.ID.String(), "test"); err != nil {
		t.Fatal(err)
	}
	if agent.actionCalls != 1 || !strings.Contains(string(agent.actionArguments), `"privateKey":"private"`) || !strings.Contains(string(agent.actionArguments), `"presharedKey":"psk"`) {
		t.Fatalf("stored secrets were not pushed: calls=%d, arguments=%s", agent.actionCalls, agent.actionArguments)
	}
	secret, err := svc.store.Secrets().Get(item.ID)
	if err != nil || secret.PrivateKey != "private" || secret.PresharedKey != "psk" {
		t.Fatalf("synced secret was not preserved: %+v, %v", secret, err)
	}
}

func TestMissingSecretDoesNotBlockResourceSync(t *testing.T) {
	svc, server, agent := newTestServerWithAgent(t)
	item := models.Resource{ID: "peer-without-secret", ServerID: server.ID.String(), APIVersion: "test/v1", Kind: "Service", Name: "alice-laptop", Generation: 1, Spec: json.RawMessage(`{}`), Status: models.ResourceStatus{Provider: json.RawMessage(`{"secretMigrationPending":true}`)}}
	if err := svc.saveProviderResource(server.ID.String(), item); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListProviderResources(server.ID.String(), "test"); err != nil {
		t.Fatalf("missing secret blocked sync: %v", err)
	}
	if agent.actionCalls != 0 {
		t.Fatalf("missing secret triggered %d actions", agent.actionCalls)
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
	agent.withoutActions = true
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
	if err := svc.syncProviderResourceSecret(server.ID.String(), definition, item); err == nil {
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

func TestRenderProviderClientConfigIncludesTextAndQRCode(t *testing.T) {
	svc, server, agent := newTestServerWithAgent(t)
	result, err := svc.RenderProviderClientConfig(server.ID.String(), "test", "Peer", "alice-laptop", json.RawMessage(`{"endpoint":"192.0.2.1:51820"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "test://client" || result.FileName != "peer.txt" {
		t.Fatalf("unexpected client configuration: %+v", result)
	}
	png, err := base64.StdEncoding.DecodeString(result.QRCode)
	if err != nil || len(png) < 8 || string(png[:8]) != "\x89PNG\r\n\x1a\n" {
		t.Fatalf("invalid QR PNG: %v", err)
	}
	if agent.actionCalls != 1 || !strings.Contains(string(agent.actionArguments), `"endpoint":"192.0.2.1:51820"`) {
		t.Fatalf("action calls=%d, arguments=%s", agent.actionCalls, agent.actionArguments)
	}
}

func TestResourcesUseProviderReconcileOrder(t *testing.T) {
	definition := &models.ProviderDefinition{Resources: []models.ResourceDefinition{
		{Kind: "Interface", ReconcileOrder: 0},
		{Kind: "Other", ReconcileOrder: 50},
		{Kind: "Peer", ReconcileOrder: 100},
	}}
	resources := []models.Resource{
		{Kind: "Peer", Name: "peer"},
		{Kind: "Other", Name: "b"},
		{Kind: "Interface", Name: "interface"},
		{Kind: "Other", Name: "a"},
	}
	sortProviderResources(resources, definition)
	got := make([]string, 0, len(resources))
	for _, item := range resources {
		got = append(got, item.Name)
	}
	expected := []string{"interface", "a", "b", "peer"}
	for index := range expected {
		if got[index] != expected[index] {
			t.Fatalf("resource order = %v", got)
		}
	}
}

func TestSyncPushesLocalSpecInsteadOfOverwriting(t *testing.T) {
	svc, server, agent := newTestServerWithAgent(t)
	agent.awg = true
	local := models.Resource{ID: uuid.New().String(), ServerID: server.ID.String(), APIVersion: "awg.ks-tool.dev/v1", Kind: "Interface", Name: "awg0", Generation: 1, Spec: json.RawMessage(`{"iface":"awg0","addr":"10.0.0.1/24"}`)}
	if err := svc.saveProviderResource(server.ID.String(), local); err != nil {
		t.Fatal(err)
	}
	remote := local
	remote.Generation = 3
	remote.Spec = json.RawMessage(`{"iface":"awg0","addr":"10.9.0.1/24"}`)
	remote.Status = models.ResourceStatus{ObservedGeneration: 3, Phase: models.ResourceReady}
	agent.resources["Interface/awg0"] = remote
	if _, err := svc.ListProviderResources(server.ID.String(), "awg"); err != nil {
		t.Fatal(err)
	}
	stored, err := svc.store.Resources(server.ID).Get(local.ID)
	if err != nil || string(stored.Spec) != string(local.Spec) {
		t.Fatalf("remote spec overwrote local desired state: %+v, %v", stored, err)
	}
	if stored.Generation != 4 || string(agent.resources["Interface/awg0"].Spec) != string(local.Spec) {
		t.Fatalf("local desired state was not pushed: local=%+v remote=%+v", stored, agent.resources["Interface/awg0"])
	}
}

func TestSyncRejectsResourceIDConflict(t *testing.T) {
	svc, server, agent := newTestServerWithAgent(t)
	agent.awg = true
	local := models.Resource{ID: uuid.New().String(), ServerID: server.ID.String(), APIVersion: "awg.ks-tool.dev/v1", Kind: "Interface", Name: "awg0", Generation: 1, Spec: json.RawMessage(`{"iface":"awg0"}`)}
	if err := svc.saveProviderResource(server.ID.String(), local); err != nil {
		t.Fatal(err)
	}
	remote := local
	remote.ID = uuid.New().String()
	remote.Status = models.ResourceStatus{ObservedGeneration: 1, Phase: models.ResourceReady}
	agent.resources["Interface/awg0"] = remote
	if _, err := svc.ListProviderResources(server.ID.String(), "awg"); err == nil || !strings.Contains(err.Error(), "resource id") {
		t.Fatalf("resource identity conflict = %v", err)
	}
}

func resourceTestPublicKey(key awgmodels.Key) string {
	publicKey := key.PublicKey()
	return publicKey.String()
}

func TestAdoptionPushesManagedSpecBeforeSecret(t *testing.T) {
	svc, server, agent := newTestServerWithAgent(t)
	agent.awg = true
	agent.preserveUpdateStatus = true
	agent.validatePeerSecrets = true
	privateKey, err := awgmodels.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	otherKey, err := awgmodels.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New().String()
	localSpec := json.RawMessage(`{"name":"alice","interfaceRef":"interface-id","publicKey":"` + resourceTestPublicKey(privateKey) + `","allowedIPs":["10.0.0.2/32"]}`)
	remoteSpec := json.RawMessage(`{"name":"alice","interfaceRef":"interface-id","publicKey":"` + resourceTestPublicKey(otherKey) + `","allowedIPs":["10.0.0.2/32"]}`)
	local := models.Resource{
		ID: id, ServerID: server.ID.String(), APIVersion: "awg.ks-tool.dev/v1", Kind: "Peer", Name: "alice", Generation: 1, Spec: localSpec,
		Status: models.ResourceStatus{Phase: models.ResourcePending, Conditions: []models.Condition{{Type: "Secrets", Reason: "SecretMigrationPending"}}},
	}
	if err := svc.saveProviderResource(server.ID.String(), local); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.Secrets().Set(&models.ResourceSecret{ResourceID: id, ServerID: server.ID.String(), PrivateKey: privateKey.String()}); err != nil {
		t.Fatal(err)
	}
	remote := local
	remote.Spec = remoteSpec
	remote.Status = models.ResourceStatus{ObservedGeneration: 1, Phase: models.ResourceReady}
	agent.resources["Peer/alice"] = remote

	if _, err := svc.listProviderResources(server.ID.String(), "awg", true); err != nil {
		t.Fatalf("adoption rejected the managed peer secret: %v", err)
	}
	if agent.actionCalls != 1 {
		t.Fatalf("peer secret imports = %d", agent.actionCalls)
	}
	if got := agent.resources["Peer/alice"].Spec; string(got) != string(localSpec) {
		t.Fatalf("remote managed spec was not replaced: %s", got)
	}
}

func TestProviderDeploymentSyncAdoptsUnmanagedPeerWithCanonicalID(t *testing.T) {
	svc, server, agent := newTestServerWithAgent(t)
	server.Unmanaged = true
	if err := svc.store.Servers().Set(server); err != nil {
		t.Fatal(err)
	}
	agent.awg = true
	agent.validatePeerSecrets = true
	privateKey, err := awgmodels.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	remoteKey, err := awgmodels.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	localID := uuid.New().String()
	localSpec := json.RawMessage(`{"name":"iphone","interfaceRef":"interface-id","publicKey":"` + resourceTestPublicKey(privateKey) + `","allowedIPs":["10.0.0.2/32"]}`)
	remoteSpec := json.RawMessage(`{"name":"iphone","interfaceRef":"interface-id","publicKey":"` + resourceTestPublicKey(remoteKey) + `","allowedIPs":["10.0.0.2/32"]}`)
	local := models.Resource{
		ID: localID, ServerID: server.ID.String(), APIVersion: "awg.ks-tool.dev/v1", Kind: "Peer", Name: "iphone", Generation: 1, Spec: localSpec,
		Status: models.ResourceStatus{Phase: models.ResourcePending, Conditions: []models.Condition{{Type: "Secrets", Reason: "SecretMigrationPending"}}},
	}
	if err := svc.saveProviderResource(server.ID.String(), local); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.Secrets().Set(&models.ResourceSecret{ResourceID: localID, ServerID: server.ID.String(), PrivateKey: privateKey.String()}); err != nil {
		t.Fatal(err)
	}
	remote := local
	remote.ID = uuid.New().String()
	remote.Spec = remoteSpec
	remote.Status = models.ResourceStatus{ObservedGeneration: 1, Phase: models.ResourceReady}
	agent.resources["Peer/iphone"] = remote

	if err := svc.syncServerAndLinks(server.ID.String()); err != nil {
		t.Fatalf("provider deployment sync failed: %v", err)
	}
	created := agent.resources["Peer/iphone"]
	if created.ID != localID || string(created.Spec) != string(localSpec) {
		t.Fatalf("canonical peer was not restored: %+v", created)
	}
	if agent.actionCalls != 1 {
		t.Fatalf("peer secret imports = %d", agent.actionCalls)
	}
	storedServer, err := svc.store.Servers().Get(server.ID)
	if err != nil || storedServer.Unmanaged {
		t.Fatalf("server migration did not complete: %+v, %v", storedServer, err)
	}
}

func TestSyncWaitsForServerMigration(t *testing.T) {
	svc, server, agent := newTestServerWithAgent(t)
	server.Unmanaged = true
	if err := svc.store.Servers().Set(server); err != nil {
		t.Fatal(err)
	}
	agent.awg = true
	item := models.Resource{ID: uuid.New().String(), ServerID: server.ID.String(), APIVersion: "awg.ks-tool.dev/v1", Kind: "Interface", Name: "awg0", Generation: 1, Spec: json.RawMessage(`{"iface":"awg0"}`)}
	if err := svc.saveProviderResource(server.ID.String(), item); err != nil {
		t.Fatal(err)
	}
	migrationApplying := make(chan struct{})
	concurrentPut := make(chan struct{}, 1)
	releaseMigration := make(chan struct{})
	var migrationBlocked atomic.Bool
	agent.beforePut = func(_ int, _ string, _ models.Resource) {
		if migrationBlocked.CompareAndSwap(false, true) {
			close(migrationApplying)
			<-releaseMigration
			return
		}
		select {
		case concurrentPut <- struct{}{}:
		default:
		}
	}
	migrationDone := make(chan error, 1)
	go func() {
		migrationServer := *server
		migrationDone <- svc.completeServerMigration(&migrationServer)
	}()
	<-migrationApplying
	syncDone := make(chan error, 1)
	go func() { syncDone <- svc.SyncServer(server.ID.String()) }()

	syncFinishedEarly := false
	syncReturned := false
	select {
	case <-concurrentPut:
		syncFinishedEarly = true
	case err := <-syncDone:
		syncFinishedEarly = true
		syncReturned = true
		if err != nil {
			t.Errorf("concurrent sync failed: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
	}
	close(releaseMigration)
	if err := <-migrationDone; err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	if !syncReturned {
		select {
		case err := <-syncDone:
			if err != nil {
				t.Fatalf("sync failed: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("sync did not finish after migration")
		}
	}
	if syncFinishedEarly {
		t.Fatal("sync ran concurrently with server migration")
	}
}

func TestAdoptionImportsRemoteInventory(t *testing.T) {
	svc, server, agent := newTestServerWithAgent(t)
	server.Unmanaged = true
	if err := svc.store.Servers().Set(server); err != nil {
		t.Fatal(err)
	}
	agent.awg = true
	remote := models.Resource{ID: uuid.New().String(), ServerID: server.ID.String(), APIVersion: "awg.ks-tool.dev/v1", Kind: "Interface", Name: "awg0", Generation: 1, Spec: json.RawMessage(`{"iface":"awg0"}`), Status: models.ResourceStatus{ObservedGeneration: 1, Phase: models.ResourceReady}}
	agent.resources["Interface/awg0"] = remote
	if err := svc.completeServerMigration(server); err != nil {
		t.Fatal(err)
	}
	stored, err := svc.store.Resources(server.ID).Get(remote.ID)
	if err != nil || stored.Name != remote.Name {
		t.Fatalf("remote inventory was not adopted: %+v, %v", stored, err)
	}
}

func TestSecretRestoreRetryUsesCanonicalOpaquePayload(t *testing.T) {
	svc, server, agent := newTestServerWithAgent(t)
	item := models.Resource{ID: "canonical-peer", ServerID: server.ID.String(), APIVersion: "test/v1", Kind: "Peer", Name: "alice", Generation: 1, Spec: json.RawMessage(`{}`), Status: models.ResourceStatus{Conditions: []models.Condition{{Type: "Secrets", Reason: "SecretMigrationPending"}}}}
	if err := svc.saveProviderResource(server.ID.String(), item); err != nil {
		t.Fatal(err)
	}
	secret := &models.ResourceSecret{ResourceID: item.ID, ServerID: server.ID.String(), Arguments: json.RawMessage(`{"presharedKey":"opaque"}`), PresharedKey: "legacy"}
	if err := svc.store.Secrets().Set(secret); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		pending, err := svc.store.Resources(server.ID).Get(item.ID)
		if err != nil {
			t.Fatal(err)
		}
		pending.Status.Conditions = []models.Condition{{Type: "Secrets", Reason: "SecretMigrationPending"}}
		if err := svc.saveProviderResource(server.ID.String(), *pending); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.ListProviderResources(server.ID.String(), "test"); err != nil {
			t.Fatal(err)
		}
	}
	if agent.actionCalls != 2 || string(agent.actionArguments) != string(secret.Arguments) {
		t.Fatalf("secret retry payload = %s, calls=%d", agent.actionArguments, agent.actionCalls)
	}
	stored, err := svc.store.Secrets().Get(item.ID)
	if err != nil || string(stored.Arguments) != string(secret.Arguments) {
		t.Fatalf("opaque secret was not preserved: %+v, %v", stored, err)
	}
}

func TestSyncRecreatesEditedResourceAndRestoresSecret(t *testing.T) {
	svc, server, agent := newTestServerWithAgent(t)
	agent.strictGenerations = true
	agent.pendingCreates = true
	item := models.Resource{ID: "edited-peer", ServerID: server.ID.String(), APIVersion: "test/v1", Kind: "Peer", Name: "alice", Generation: 2, Spec: json.RawMessage(`{"revision":2}`), Status: models.ResourceStatus{ObservedGeneration: 2, Phase: models.ResourceReady}}
	if err := svc.saveProviderResource(server.ID.String(), item); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.Secrets().Set(&models.ResourceSecret{ResourceID: item.ID, ServerID: server.ID.String(), Arguments: json.RawMessage(`{"presharedKey":"secret"}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListProviderResources(server.ID.String(), "test"); err != nil {
		t.Fatalf("recreate edited resource: %v", err)
	}
	created := agent.resources["Peer/alice"]
	if created.Generation != 1 || created.ID != item.ID {
		t.Fatalf("recreated resource = %+v", created)
	}
	if agent.actionCalls != 1 || string(agent.actionArguments) != `{"presharedKey":"secret"}` {
		t.Fatalf("secret was not restored after create: calls=%d payload=%s", agent.actionCalls, agent.actionArguments)
	}
}

func TestSyncHonorsRemoteSecretPending(t *testing.T) {
	svc, server, agent := newTestServerWithAgent(t)
	item := models.Resource{ID: "ready-peer", ServerID: server.ID.String(), APIVersion: "test/v1", Kind: "Peer", Name: "alice", Generation: 1, Spec: json.RawMessage(`{}`), Status: models.ResourceStatus{ObservedGeneration: 1, Phase: models.ResourceReady}}
	if err := svc.saveProviderResource(server.ID.String(), item); err != nil {
		t.Fatal(err)
	}
	remote := item
	remote.Status.Provider = json.RawMessage(`{"secretMigrationPending":true}`)
	agent.resources["Peer/alice"] = remote
	if err := svc.store.Secrets().Set(&models.ResourceSecret{ResourceID: item.ID, ServerID: server.ID.String(), Arguments: json.RawMessage(`{"presharedKey":"secret"}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListProviderResources(server.ID.String(), "test"); err != nil {
		t.Fatal(err)
	}
	if agent.actionCalls != 1 {
		t.Fatalf("remote pending secret was ignored: %d", agent.actionCalls)
	}
}

func TestPutDoesNotAdoptRemoteOnly(t *testing.T) {
	svc, server, agent := newTestServerWithAgent(t)
	agent.awg = true
	remote := models.Resource{ID: uuid.New().String(), ServerID: server.ID.String(), APIVersion: "awg.ks-tool.dev/v1", Kind: "Peer", Name: "remote-only", Generation: 1, Spec: json.RawMessage(`{}`), Status: models.ResourceStatus{ObservedGeneration: 1, Phase: models.ResourceReady}}
	agent.resources["Peer/remote-only"] = remote
	if _, err := svc.PutProviderResource(server.ID.String(), "awg", "Peer", remote.Name, json.RawMessage(`{"changed":true}`)); err == nil || !strings.Contains(err.Error(), "adoption") {
		t.Fatalf("remote-only PUT error = %v", err)
	}
	if _, err := svc.store.Resources(server.ID).Get(remote.ID); !storage.IsNotFound(err) {
		t.Fatalf("remote-only resource was adopted by PUT: %v", err)
	}
}

func TestAdminPutLostResponseRetryDoesNotRollback(t *testing.T) {
	svc, server, agent := newTestServerWithAgent(t)
	agent.awg = true
	agent.strictGenerations = true
	local := models.Resource{ID: uuid.New().String(), ServerID: server.ID.String(), APIVersion: "awg.ks-tool.dev/v1", Kind: "Interface", Name: "awg0", Generation: 1, Spec: json.RawMessage(`{"revision":"old"}`)}
	if err := svc.saveProviderResource(server.ID.String(), local); err != nil {
		t.Fatal(err)
	}
	remote := local
	remote.Generation = 2
	remote.Spec = json.RawMessage(`{"revision":"requested"}`)
	remote.Status = models.ResourceStatus{ObservedGeneration: 2, Phase: models.ResourceReady}
	agent.resources["Interface/awg0"] = remote
	updated, err := svc.PutProviderResource(server.ID.String(), "awg", "Interface", local.Name, remote.Spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(agent.putSpecs) != 1 || string(agent.putSpecs[0]) != string(remote.Spec) {
		t.Fatalf("retry rolled back and reapplied spec: %s", agent.putSpecs)
	}
	stored, err := svc.store.Resources(server.ID).Get(local.ID)
	if err != nil || string(stored.Spec) != string(updated.Spec) {
		t.Fatalf("retry result was not saved: %+v, %v", stored, err)
	}
}

func TestSecretRestoreRetriesSameCanonicalResourceAfterActionFailure(t *testing.T) {
	svc, server, agent := newTestServerWithAgent(t)
	agent.actionFailures = 1
	item := models.Resource{ID: "canonical-peer", ServerID: server.ID.String(), APIVersion: "test/v1", Kind: "Peer", Name: "alice", Generation: 1, Spec: json.RawMessage(`{}`), Status: models.ResourceStatus{Conditions: []models.Condition{{Type: "Secrets", Reason: "SecretMigrationPending"}}}}
	if err := svc.saveProviderResource(server.ID.String(), item); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.Secrets().Set(&models.ResourceSecret{ResourceID: item.ID, ServerID: server.ID.String(), Arguments: json.RawMessage(`{"presharedKey":"opaque"}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListProviderResources(server.ID.String(), "test"); err == nil {
		t.Fatal("action failure was ignored")
	}
	if _, err := svc.ListProviderResources(server.ID.String(), "test"); err != nil {
		t.Fatalf("secret retry: %v", err)
	}
	if len(agent.actionResources) != 2 || agent.actionResources[0] != "Peer/alice" || agent.actionResources[1] != "Peer/alice" {
		t.Fatalf("secret retry resources = %v", agent.actionResources)
	}
	if string(agent.actionArguments) != `{"presharedKey":"opaque"}` {
		t.Fatalf("secret retry payload = %s", agent.actionArguments)
	}
}
