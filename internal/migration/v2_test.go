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

package migration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"uuid"

	"github.com/ks-tool/vpn-admin/internal/migration"
	"github.com/ks-tool/vpn-admin/models"
	"github.com/ks-tool/vpn-admin/storage/boltdb"
	awgmodels "github.com/ks-tool/vpn-agent/v2/providers/awg/models"
	bolt "go.etcd.io/bbolt"
)

type fixture struct {
	serverA uuid.UUID
	serverB uuid.UUID
	ifaceA  uuid.UUID
	ifaceB  uuid.UUID
	tunnel  uuid.UUID
	user    uuid.UUID
	peerKey awgmodels.Key
}

func TestV2MigratesCompleteInventoryWithoutLegacyBuckets(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "awg-admin.db")
	destination := filepath.Join(root, "vpn-admin", "db")
	data := createFixture(t, source, false)
	before, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := migration.V2(source, destination, false); err != nil {
		t.Fatalf("migration.V2: %v", err)
	}
	after, err := os.ReadFile(source)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("source changed: %v", err)
	}

	db, err := boltdb.Open(destination)
	if err != nil {
		t.Fatalf("open migrated database: %v", err)
	}
	defer func() { _ = db.Close() }()
	if version, err := db.SchemaVersion(); err != nil || version != 2 {
		t.Fatalf("schema version = %d, %v", version, err)
	}
	server, err := db.Servers().Get(data.serverA)
	if err != nil || !server.Unmanaged || server.Info.Location != "Moscow" || server.SSH.KeyData != "private-ssh-key" {
		t.Fatalf("server was not preserved: %+v, %v", server, err)
	}
	user, err := db.Users().Get(data.user)
	if err != nil || user.Name != "alice" {
		t.Fatalf("user was not preserved: %+v, %v", user, err)
	}
	resources, err := db.Resources(data.serverA).List()
	if err != nil || len(resources) != 2 {
		t.Fatalf("resources = %+v, %v", resources, err)
	}
	var interfaceResource, peerResource *models.Resource
	for index := range resources {
		switch resources[index].Kind {
		case "Interface":
			interfaceResource = &resources[index]
		case "Peer":
			peerResource = &resources[index]
		}
	}
	if interfaceResource == nil || interfaceResource.ID != data.ifaceA.String() {
		t.Fatalf("interface identity was not preserved: %+v", resources)
	}
	if strings.Contains(string(interfaceResource.Spec), "private-awg-key") || strings.Contains(string(interfaceResource.Spec), `"peers"`) {
		t.Fatalf("secret or embedded peers leaked into interface spec: %s", interfaceResource.Spec)
	}
	if peerResource == nil {
		t.Fatal("user peer resource was not migrated")
	}
	if peerResource.Name != "laptop" {
		t.Fatalf("peer name changed during migration: %q", peerResource.Name)
	}
	var peerSpec map[string]any
	if err := json.Unmarshal(peerResource.Spec, &peerSpec); err != nil {
		t.Fatal(err)
	}
	if peerSpec["userRef"] != data.user.String() || peerSpec["interfaceRef"] != data.ifaceA.String() || peerSpec["publicKey"] != keyString(data.peerKey.PublicKey()) {
		t.Fatalf("peer references changed: %+v", peerSpec)
	}
	secret, err := db.Secrets().Get(peerResource.ID)
	if err != nil || secret.PrivateKey != data.peerKey.String() {
		t.Fatalf("peer secret was not preserved: %+v, %v", secret, err)
	}
	links, err := db.Links().List()
	if err != nil || len(links) != 1 || links[0].ID != data.tunnel.String() {
		t.Fatalf("tunnel was not migrated: %+v, %v", links, err)
	}
	topology, err := db.Topology().Get()
	if err != nil || len(topology.Servers) != 1 || topology.Servers[0].Latitude == nil {
		t.Fatalf("topology metadata was not preserved: %+v, %v", topology, err)
	}
	auth, err := db.Auth().Get()
	if err != nil || auth.PasswordHash != "existing-password-hash" {
		t.Fatalf("auth changed: %+v, %v", auth, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	assertCleanBuckets(t, destination)
}

func TestV2RejectsNonV1AndFailedMigrationLeavesDestinationAbsent(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "v1.db")
	destination := filepath.Join(root, "v2.db")
	createFixture(t, source, true)
	if err := migration.V2(source, destination, false); err == nil {
		t.Fatal("corrupt user record was migrated")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("partial destination survived: %v", err)
	}

	current := filepath.Join(root, "current.db")
	raw, err := bolt.Open(current, 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucket([]byte("resources"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := migration.V2(current, destination, false); err == nil || !strings.Contains(err.Error(), "not a v1 database") {
		t.Fatalf("non-v1 source error = %v", err)
	}
}

func createFixture(t *testing.T, path string, corruptUsers bool) fixture {
	t.Helper()
	raw, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	data := fixture{serverA: uuid.New(), serverB: uuid.New(), ifaceA: uuid.New(), ifaceB: uuid.New(), tunnel: uuid.New(), user: uuid.New(), peerKey: mustKey(t)}
	interfaceKey := mustKey(t)
	latitude := 55.7558
	if err := raw.Update(func(tx *bolt.Tx) error {
		for _, name := range []string{"servers", "interfaces", "users", "auth", "topology"} {
			if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
				return err
			}
		}
		put := func(bucket string, key []byte, value any) error {
			encoded, err := json.Marshal(value)
			if err != nil {
				return err
			}
			return tx.Bucket([]byte(bucket)).Put(key, encoded)
		}
		if err := put("servers", binaryID(data.serverA), map[string]any{"id": data.serverA, "name": "a", "info": map[string]any{"location": "Moscow"}, "ssh": map[string]any{"host": "192.0.2.1", "keyData": "private-ssh-key"}, "agent": map[string]any{"addr": "127.0.0.1:8080"}, "interfaces": []uuid.UUID{data.ifaceA}}); err != nil {
			return err
		}
		if err := put("servers", binaryID(data.serverB), map[string]any{"id": data.serverB, "name": "b", "interfaces": []uuid.UUID{data.ifaceB}}); err != nil {
			return err
		}
		if err := put("interfaces", binaryID(data.ifaceA), map[string]any{"id": data.ifaceA, "iface": "awg0", "addr": "10.0.0.1/24", "listen": 51820, "table": 100, "pk": interfaceKey.String(), "peers": []map[string]any{{"key": keyString(data.peerKey.PublicKey()), "ips": []string{"10.0.0.2/32"}}}, "preUp": []string{"private-hook"}, "tunnel": data.tunnel}); err != nil {
			return err
		}
		if err := put("interfaces", binaryID(data.ifaceB), map[string]any{"id": data.ifaceB, "iface": "awg0", "addr": "10.0.0.2/24", "pk": keyString(mustKey(t)), "tunnel": data.tunnel}); err != nil {
			return err
		}
		if corruptUsers {
			return tx.Bucket([]byte("users")).Put(binaryID(data.user), []byte(`{"id":"not-a-uuid"}`))
		}
		if err := put("users", binaryID(data.user), map[string]any{"id": data.user, "name": "alice", "peers": []map[string]any{{"name": "laptop", "pk": data.peerKey.String(), "interface": data.ifaceA, "dns": []string{"1.1.1.1"}}}}); err != nil {
			return err
		}
		if err := put("auth", []byte("credentials"), map[string]any{"username": "admin", "passwordHash": "existing-password-hash"}); err != nil {
			return err
		}
		return put("topology", []byte("main"), map[string]any{"servers": []map[string]any{{"id": data.serverA, "location": "Moscow", "latitude": latitude}}, "connections": []map[string]any{{"id": uuid.New(), "sourceId": data.ifaceA, "targetId": data.ifaceB}}})
	}); err != nil {
		_ = raw.Close()
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	return data
}

func assertCleanBuckets(t *testing.T, path string) {
	t.Helper()
	raw, err := bolt.Open(path, 0o600, &bolt.Options{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	if err := raw.View(func(tx *bolt.Tx) error {
		if tx.Bucket([]byte("interfaces")) != nil {
			t.Fatal("legacy interfaces bucket survived migration")
		}
		return tx.Bucket([]byte("users")).ForEach(func(_, value []byte) error {
			if bytes.Contains(value, []byte(`"peers"`)) {
				t.Fatalf("legacy user peers survived migration: %s", value)
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
}

func binaryID(id uuid.UUID) []byte {
	value := [16]byte(id)
	return value[:]
}

func mustKey(t *testing.T) awgmodels.Key {
	t.Helper()
	key, err := awgmodels.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func keyString(key awgmodels.Key) string {
	return key.String()
}

func TestV2PreservesEmbeddedPeerAndInterfaceSecrets(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "v1.db")
	destination := filepath.Join(root, "v2.db")
	data := createFixture(t, source, false)
	embeddedKey := mustKey(t)
	presharedKey := mustKey(t)
	raw, err := bolt.Open(source, 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Update(func(tx *bolt.Tx) error {
		var iface map[string]any
		if err := json.Unmarshal(tx.Bucket([]byte("interfaces")).Get(binaryID(data.ifaceA)), &iface); err != nil {
			return err
		}
		iface["peers"] = append(iface["peers"].([]any), map[string]any{"key": keyString(embeddedKey.PublicKey()), "ips": []string{"10.0.0.3/32"}, "psk": presharedKey.String()})
		encoded, err := json.Marshal(iface)
		if err != nil {
			return err
		}
		return tx.Bucket([]byte("interfaces")).Put(binaryID(data.ifaceA), encoded)
	}); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := migration.V2(source, destination, false); err != nil {
		t.Fatal(err)
	}
	db, err := boltdb.Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	resources, err := db.Resources(data.serverA).List()
	if err != nil || len(resources) != 3 {
		t.Fatalf("embedded peer was lost: %+v, %v", resources, err)
	}
	found := false
	for _, resource := range resources {
		if resource.Kind == "Interface" {
			secret, err := db.Secrets().Get(resource.ID)
			if err != nil || len(secret.PreUp) != 1 || secret.PreUp[0] != "private-hook" {
				t.Fatalf("interface hooks were lost: %+v, %v", secret, err)
			}
		}
		if resource.Kind != "Peer" {
			continue
		}
		var spec map[string]any
		if err := json.Unmarshal(resource.Spec, &spec); err != nil {
			t.Fatal(err)
		}
		if spec["publicKey"] == keyString(embeddedKey.PublicKey()) {
			found = true
			if _, exists := spec["userRef"]; exists {
				t.Fatalf("embedded peer acquired a user: %+v", spec)
			}
			secret, err := db.Secrets().Get(resource.ID)
			if err != nil || secret.PresharedKey != presharedKey.String() {
				t.Fatalf("embedded peer PSK was lost: %+v, %v", secret, err)
			}
		}
	}
	if !found {
		t.Fatal("embedded peer resource was not created")
	}
}

func TestV2MigratesSourcesAndProtectsDestination(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "v1.db")
	destination := filepath.Join(root, "v2.db")
	createFixture(t, source, false)
	agentSourceID := uuid.New()
	registryID := uuid.New()
	raw, err := bolt.Open(source, 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Update(func(tx *bolt.Tx) error {
		for _, name := range []string{"agent_sources", "registry_sources"} {
			if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
				return err
			}
		}
		agent, _ := json.Marshal(map[string]any{"id": agentSourceID, "name": "local", "path": "/tmp/vpn-agent"})
		if err := tx.Bucket([]byte("agent_sources")).Put(binaryID(agentSourceID), agent); err != nil {
			return err
		}
		registry, _ := json.Marshal(map[string]any{"id": registryID, "url": "oci://registry.example/", "username": "reader", "password": "secret"})
		return tx.Bucket([]byte("registry_sources")).Put(binaryID(registryID), registry)
	}); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := migration.V2(source, destination, false); err == nil {
		t.Fatal("existing destination was replaced without force")
	}
	if content, err := os.ReadFile(destination); err != nil || string(content) != "existing" {
		t.Fatalf("destination changed: %q, %v", content, err)
	}
	if err := migration.V2(source, destination, true); err != nil {
		t.Fatal(err)
	}
	db, err := boltdb.Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	agentSource, err := db.AgentSources().Get(agentSourceID)
	if err != nil || agentSource.Path != "/tmp/vpn-agent" {
		t.Fatalf("agent source was not preserved: %+v, %v", agentSource, err)
	}
	registries, err := db.RegistrySources().List()
	if err != nil || len(registries) != 2 {
		t.Fatalf("registry source was not split: %+v, %v", registries, err)
	}
	expectedURLs := map[string]string{models.RegistrySourceAgent: "oci://registry.example/ks-tool/vpn-agent", models.RegistrySourceProvider: "oci://registry.example/vpn-providers"}
	for _, registry := range registries {
		if registry.Password != "secret" || registry.URL != expectedURLs[registry.Type] {
			t.Fatalf("registry source was corrupted: %+v", registry)
		}
	}
}

func TestV2EmbeddedPeerWithoutPSKHasNoPendingSecret(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "v1.db")
	destination := filepath.Join(root, "v2.db")
	data := createFixture(t, source, false)
	embeddedKey := mustKey(t)
	raw, err := bolt.Open(source, 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Update(func(tx *bolt.Tx) error {
		var iface map[string]any
		if err := json.Unmarshal(tx.Bucket([]byte("interfaces")).Get(binaryID(data.ifaceA)), &iface); err != nil {
			return err
		}
		iface["peers"] = append(iface["peers"].([]any), map[string]any{"key": keyString(embeddedKey.PublicKey()), "ips": []string{"10.0.0.3/32"}})
		encoded, err := json.Marshal(iface)
		if err != nil {
			return err
		}
		return tx.Bucket([]byte("interfaces")).Put(binaryID(data.ifaceA), encoded)
	}); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := migration.V2(source, destination, false); err != nil {
		t.Fatal(err)
	}
	db, err := boltdb.Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	resources, err := db.Resources(data.serverA).List()
	if err != nil {
		t.Fatal(err)
	}
	for _, resource := range resources {
		if resource.Kind != "Peer" {
			continue
		}
		var spec map[string]any
		if err := json.Unmarshal(resource.Spec, &spec); err != nil {
			t.Fatal(err)
		}
		if spec["publicKey"] != keyString(embeddedKey.PublicKey()) {
			continue
		}
		for _, condition := range resource.Status.Conditions {
			if condition.Reason == "SecretMigrationPending" {
				t.Fatalf("peer without secrets is pending migration: %+v", resource.Status)
			}
		}
		if _, err := db.Secrets().Get(resource.ID); err == nil {
			t.Fatal("empty secret record was created")
		}
		return
	}
	t.Fatal("embedded peer without PSK was not migrated")
}

func TestV2RejectsUserPeerMissingFromInterface(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "v1.db")
	destination := filepath.Join(root, "v2.db")
	data := createFixture(t, source, false)
	raw, err := bolt.Open(source, 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Update(func(tx *bolt.Tx) error {
		var iface map[string]any
		if err := json.Unmarshal(tx.Bucket([]byte("interfaces")).Get(binaryID(data.ifaceA)), &iface); err != nil {
			return err
		}
		iface["addr"] = "10.0.0.10/24"
		iface["peers"] = []any{}
		encoded, err := json.Marshal(iface)
		if err != nil {
			return err
		}
		return tx.Bucket([]byte("interfaces")).Put(binaryID(data.ifaceA), encoded)
	}); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := migration.V2(source, destination, false); err == nil || !strings.Contains(err.Error(), "is missing from interface") {
		t.Fatalf("inconsistent v1 peer error = %v", err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("migration with an allocated peer address produced a destination: %v", err)
	}
}
