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

// Package migration reads the v1 BoltDB snapshot. Its snapshot types are the
// input contract of the offline migrator and are never used by vpn-admin.
package migration

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/ks-tool/vpn-admin/models"
	awgmodels "github.com/ks-tool/vpn-agent/v2/providers/awg/models"
	bolt "go.etcd.io/bbolt"
	"golang.org/x/crypto/bcrypt"
)

const targetSchemaVersion = 2

var (
	bucketServers       = []byte("servers")
	bucketUsers         = []byte("users")
	bucketInterfaces    = []byte("interfaces")
	bucketAuth          = []byte("auth")
	bucketAgentSources  = []byte("agent_sources")
	bucketRegistry      = []byte("registry_sources")
	bucketTopology      = []byte("topology")
	bucketResources     = []byte("resources")
	bucketLinks         = []byte("links")
	bucketProviders     = []byte("providers")
	bucketSecrets       = []byte("secrets")
	bucketMeta          = []byte("meta")
	schemaVersionKey    = []byte("schemaVersion")
	registryInitialized = []byte("registrySourcesInitialized")
	authCredentialsKey  = []byte("credentials")
	topologyMainKey     = []byte("main")
)

type v1Server struct {
	ID         uuid.UUID         `json:"id"`
	Name       string            `json:"name"`
	Info       models.ServerInfo `json:"info"`
	SSH        models.SSHConfig  `json:"ssh"`
	Agent      models.Agent      `json:"agent"`
	Interfaces []uuid.UUID       `json:"interfaces"`
}

type v1Interface struct {
	ID                        uuid.UUID `json:"id"`
	awgmodels.InterfaceConfig `json:",inline"`
	Tunnel                    *uuid.UUID `json:"tunnel"`
}

type v1Peer struct {
	Name        string        `json:"name"`
	PrivateKey  awgmodels.Key `json:"pk"`
	InterfaceID uuid.UUID     `json:"interface"`
	Disabled    bool          `json:"disabled"`
	DNS         []string      `json:"dns"`
}

type v1User struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Disabled    bool      `json:"disabled"`
	Peers       []v1Peer  `json:"peers"`
}

type v1RegistrySource struct {
	ID                 uuid.UUID `json:"id"`
	URL                string    `json:"url"`
	Type               string    `json:"type"`
	AgentRepository    string    `json:"agentRepository"`
	ProviderRepository string    `json:"providerRepository"`
	Username           string    `json:"username"`
	Password           string    `json:"password"`
	PlainHTTP          bool      `json:"plainHttp"`
}

type v1Connection struct {
	ID       uuid.UUID `json:"id"`
	SourceID uuid.UUID `json:"sourceId"`
	TargetID uuid.UUID `json:"targetId"`
}

type v1Topology struct {
	Servers     []models.TopologyServer   `json:"servers"`
	Interfaces  []models.NetworkInterface `json:"interfaces"`
	Connections []v1Connection            `json:"connections"`
}

type inventory struct {
	servers    map[uuid.UUID]models.Server
	interfaces map[uuid.UUID]v1Interface
	owners     map[uuid.UUID]models.Server
	topology   v1Topology
}

// V2 converts a v1 database into a separate resource-only v2 database.
func V2(sourcePath, destinationPath string, force bool) error {
	sourceAbs, err := filepath.Abs(sourcePath)
	if err != nil {
		return err
	}
	destinationAbs, err := filepath.Abs(destinationPath)
	if err != nil {
		return err
	}
	if sourceAbs == destinationAbs {
		return errors.New("source and destination must be different files")
	}
	if _, err := os.Stat(destinationAbs); err == nil && !force {
		return fmt.Errorf("destination %s already exists (use -force to replace it)", destinationAbs)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}

	source, err := bolt.Open(sourceAbs, 0o600, &bolt.Options{ReadOnly: true, Timeout: 5 * time.Second})
	if err != nil {
		return fmt.Errorf("open v1 source: %w", err)
	}
	defer func() { _ = source.Close() }()
	if err := validateSource(source); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destinationAbs), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(destinationAbs), ".v2-migration-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	defer func() { _ = os.Remove(temporaryPath) }()
	destination, err := bolt.Open(temporaryPath, 0o600, nil)
	if err != nil {
		return err
	}
	err = source.View(func(sourceTx *bolt.Tx) error {
		return destination.Update(func(destinationTx *bolt.Tx) error {
			return migrateSnapshot(sourceTx, destinationTx)
		})
	})
	if err != nil {
		_ = destination.Close()
		return err
	}
	if err := destination.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, destinationAbs)
}

func validateSource(db *bolt.DB) error {
	return db.View(func(tx *bolt.Tx) error {
		if tx.Bucket(bucketResources) != nil || tx.Bucket(bucketLinks) != nil {
			return errors.New("source is not a v1 database; only the original awg-admin database can be migrated")
		}
		if tx.Bucket(bucketServers) == nil || tx.Bucket(bucketInterfaces) == nil || tx.Bucket(bucketUsers) == nil {
			return errors.New("source schema is not a v1 database")
		}
		return nil
	})
}

func migrateSnapshot(source, target *bolt.Tx) error {
	for _, name := range [][]byte{bucketServers, bucketUsers, bucketAuth, bucketAgentSources, bucketRegistry, bucketTopology, bucketResources, bucketLinks, bucketProviders, bucketSecrets, bucketMeta} {
		if _, err := target.CreateBucketIfNotExists(name); err != nil {
			return err
		}
	}
	for _, name := range [][]byte{bucketAuth, bucketAgentSources} {
		if sourceBucket := source.Bucket(name); sourceBucket != nil {
			if err := sourceBucket.ForEach(func(key, value []byte) error {
				if value == nil {
					return nil
				}
				return target.Bucket(name).Put(key, value)
			}); err != nil {
				return err
			}
		}
	}
	if target.Bucket(bucketAuth).Get(authCredentialsKey) == nil {
		hash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		if err := put(target.Bucket(bucketAuth), authCredentialsKey, &models.AuthCredentials{Username: "admin", PasswordHash: string(hash)}); err != nil {
			return err
		}
	}

	data := inventory{
		servers: make(map[uuid.UUID]models.Server), interfaces: make(map[uuid.UUID]v1Interface),
		owners:   make(map[uuid.UUID]models.Server),
		topology: v1Topology{Servers: []models.TopologyServer{}, Interfaces: []models.NetworkInterface{}, Connections: []v1Connection{}},
	}
	if err := source.Bucket(bucketServers).ForEach(func(key, value []byte) error {
		var old v1Server
		if err := json.Unmarshal(value, &old); err != nil {
			return fmt.Errorf("decode server %x: %w", key, err)
		}
		server := models.Server{ID: old.ID, Name: old.Name, Info: old.Info, SSH: old.SSH, Agent: old.Agent, Unmanaged: true}
		data.servers[server.ID] = server
		for _, interfaceID := range old.Interfaces {
			raw := source.Bucket(bucketInterfaces).Get(binaryID(interfaceID))
			if raw == nil {
				return fmt.Errorf("server %s references missing interface %s", server.ID, interfaceID)
			}
			var iface v1Interface
			if err := json.Unmarshal(raw, &iface); err != nil {
				return fmt.Errorf("decode interface %s: %w", interfaceID, err)
			}
			data.interfaces[iface.ID] = iface
			data.owners[iface.ID] = server
		}
		return put(target.Bucket(bucketServers), key, &server)
	}); err != nil {
		return err
	}

	if bucket := source.Bucket(bucketTopology); bucket != nil {
		if value := bucket.Get(topologyMainKey); len(value) > 0 {
			if err := json.Unmarshal(value, &data.topology); err != nil {
				return fmt.Errorf("decode topology: %w", err)
			}
		}
	}
	if err := source.Bucket(bucketUsers).ForEach(func(key, value []byte) error {
		var old v1User
		if err := json.Unmarshal(value, &old); err != nil {
			return fmt.Errorf("decode user %x: %w", key, err)
		}
		user := models.User{ID: old.ID, Name: old.Name, Description: old.Description, Disabled: old.Disabled}
		if err := put(target.Bucket(bucketUsers), key, &user); err != nil {
			return err
		}
		for _, peer := range old.Peers {
			iface, ok := data.interfaces[peer.InterfaceID]
			if !ok {
				return fmt.Errorf("user %s peer %q references missing interface %s", old.ID, peer.Name, peer.InterfaceID)
			}
			resource, secret, err := migrateUserPeer(old, peer, iface, data.owners[iface.ID])
			if err != nil {
				return err
			}
			if err := put(target.Bucket(bucketResources), []byte(resource.ID), &resource); err != nil {
				return err
			}
			if err := put(target.Bucket(bucketSecrets), []byte(resource.ID), &secret); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}

	for _, iface := range data.interfaces {
		resource, secret, err := migrateInterface(data.owners[iface.ID], iface)
		if err != nil {
			return err
		}
		if err := put(target.Bucket(bucketResources), []byte(resource.ID), &resource); err != nil {
			return err
		}
		if err := put(target.Bucket(bucketSecrets), []byte(resource.ID), &secret); err != nil {
			return err
		}
	}
	if err := migrateEmbeddedPeers(source, target, data); err != nil {
		return err
	}
	if err := migrateLinks(target, data); err != nil {
		return err
	}
	topology := models.Topology{Servers: data.topology.Servers, Interfaces: data.topology.Interfaces}
	if err := put(target.Bucket(bucketTopology), topologyMainKey, &topology); err != nil {
		return err
	}
	if err := migrateRegistrySources(source.Bucket(bucketRegistry), target.Bucket(bucketRegistry)); err != nil {
		return err
	}
	meta := target.Bucket(bucketMeta)
	if err := meta.Put(registryInitialized, []byte("1")); err != nil {
		return err
	}
	if key, _ := target.Bucket(bucketRegistry).Cursor().First(); key == nil {
		for _, source := range defaultRegistrySources() {
			if err := put(target.Bucket(bucketRegistry), binaryID(source.ID), source); err != nil {
				return err
			}
		}
	}
	return meta.Put(schemaVersionKey, []byte(strconv.Itoa(targetSchemaVersion)))
}

func migrateInterface(server models.Server, iface v1Interface) (models.Resource, models.ResourceSecret, error) {
	data, err := json.Marshal(iface.InterfaceConfig)
	if err != nil {
		return models.Resource{}, models.ResourceSecret{}, err
	}
	var spec map[string]any
	if err := json.Unmarshal(data, &spec); err != nil {
		return models.Resource{}, models.ResourceSecret{}, err
	}
	for _, field := range []string{"pk", "peers", "preUp", "postUp", "preDown", "postDown"} {
		delete(spec, field)
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		return models.Resource{}, models.ResourceSecret{}, err
	}
	status := pendingStatus()
	provider, _ := json.Marshal(map[string]any{"interface": iface.Interface, "publicKey": keyString(iface.PrivateKey.PublicKey()), "secretMigrationPending": true})
	status.Provider = provider
	status.Conditions = append(status.Conditions, pendingSecretCondition())
	resource := models.Resource{ID: iface.ID.String(), ServerID: server.ID.String(), APIVersion: "awg.ks-tool.dev/v1", Kind: "Interface", Name: iface.Interface, Generation: 1, Spec: raw, Status: status}
	secret := models.ResourceSecret{ResourceID: resource.ID, ServerID: server.ID.String(), PrivateKey: iface.PrivateKey.String(), PreUp: iface.PreUp, PostUp: iface.PostUp, PreDown: iface.PreDown, PostDown: iface.PostDown}
	return resource, secret, nil
}

func migrateUserPeer(user v1User, peer v1Peer, iface v1Interface, server models.Server) (models.Resource, models.ResourceSecret, error) {
	publicKey := keyString(peer.PrivateKey.PublicKey())
	var configured *awgmodels.InterfacePeer
	for index := range iface.Peers {
		if iface.Peers[index].Key.String() == publicKey {
			configured = &iface.Peers[index]
			break
		}
	}
	if configured == nil {
		return models.Resource{}, models.ResourceSecret{}, fmt.Errorf("user %s peer %q is missing from interface %s; restore the v1 interface peer instead of allocating a new address", user.ID, peer.Name, iface.ID)
	}
	id := models.NewDeterministicUUID(user.ID.String() + ":" + peer.InterfaceID.String() + ":" + publicKey)
	values := map[string]any{
		"name": peer.Name, "interfaceRef": peer.InterfaceID.String(), "userRef": user.ID.String(), "publicKey": publicKey,
		"allowedIPs": configured.AllowedIPs, "disabled": peer.Disabled || user.Disabled || configured.Disabled,
		"keepaliveSeconds": int(configured.KeepaliveInterval.Seconds()),
	}
	if len(peer.DNS) > 0 {
		values["dns"] = peer.DNS
	}
	resource, secret, err := newPeerResource(id, server, values, peer.PrivateKey.String(), configured.PresharedKey)
	resource.Name = peer.Name
	return resource, secret, err
}

func migrateEmbeddedPeers(source, target *bolt.Tx, data inventory) error {
	owned := make(map[string]bool)
	if err := source.Bucket(bucketUsers).ForEach(func(_, value []byte) error {
		var user v1User
		if err := json.Unmarshal(value, &user); err != nil {
			return err
		}
		for _, peer := range user.Peers {
			owned[peer.InterfaceID.String()+":"+keyString(peer.PrivateKey.PublicKey())] = true
		}
		return nil
	}); err != nil {
		return err
	}
	interfaceKeys := make(map[string]bool)
	for _, iface := range data.interfaces {
		interfaceKeys[keyString(iface.PrivateKey.PublicKey())] = true
	}
	for _, iface := range data.interfaces {
		for _, peer := range iface.Peers {
			key := iface.ID.String() + ":" + peer.Key.String()
			if owned[key] || interfaceKeys[peer.Key.String()] {
				continue
			}
			id := models.NewDeterministicUUID("embedded:" + key)
			values := map[string]any{
				"name": "Imported peer", "interfaceRef": iface.ID.String(), "publicKey": peer.Key.String(),
				"allowedIPs": peer.AllowedIPs, "disabled": peer.Disabled, "keepaliveSeconds": int(peer.KeepaliveInterval.Seconds()),
			}
			resource, secret, err := newPeerResource(id, data.owners[iface.ID], values, "", peer.PresharedKey)
			if err != nil {
				return err
			}
			if err := put(target.Bucket(bucketResources), []byte(resource.ID), &resource); err != nil {
				return err
			}
			if len(secret.PrivateKey) > 0 || len(secret.PresharedKey) > 0 {
				if err := put(target.Bucket(bucketSecrets), []byte(resource.ID), &secret); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func newPeerResource(id uuid.UUID, server models.Server, values map[string]any, privateKey string, presharedKey *awgmodels.Key) (models.Resource, models.ResourceSecret, error) {
	spec, err := json.Marshal(values)
	if err != nil {
		return models.Resource{}, models.ResourceSecret{}, err
	}
	name, _ := values["name"].(string)
	status := pendingStatus()
	secret := models.ResourceSecret{ResourceID: id.String(), ServerID: server.ID.String(), PrivateKey: privateKey}
	hasSecret := len(privateKey) > 0
	if presharedKey != nil {
		secret.PresharedKey = presharedKey.String()
		hasSecret = len(secret.PresharedKey) > 0
	}
	provider, _ := json.Marshal(map[string]any{"publicKey": values["publicKey"], "secretMigrationPending": hasSecret})
	status.Provider = provider
	if hasSecret {
		status.Conditions = append(status.Conditions, pendingSecretCondition())
	}
	resourceName := name + "-" + id.String()[:8]
	if userRef, ok := values["userRef"].(string); ok {
		resourceName = userRef + "-" + resourceName
	}
	resource := models.Resource{ID: id.String(), ServerID: server.ID.String(), APIVersion: "awg.ks-tool.dev/v1", Kind: "Peer", Name: resourceName, Generation: 1, Spec: spec, Status: status}
	return resource, secret, nil
}

func migrateLinks(target *bolt.Tx, data inventory) error {
	tunnels := make(map[uuid.UUID][]v1Interface)
	for _, iface := range data.interfaces {
		if iface.Tunnel != nil {
			tunnels[*iface.Tunnel] = append(tunnels[*iface.Tunnel], iface)
		}
	}
	pairs := make(map[string]bool)
	for tunnelID, members := range tunnels {
		sort.SliceStable(members, func(i, j int) bool {
			if (members[i].Table == 0) != (members[j].Table == 0) {
				return members[i].Table != 0
			}
			return members[i].ID.String() < members[j].ID.String()
		})
		for index := 0; index+1 < len(members); index++ {
			id := models.NewDeterministicUUID(tunnelID.String() + ":" + strconv.Itoa(index))
			if index == 0 {
				id = tunnelID
			}
			link := migrateLink(id, members[index], members[index+1], data.owners, json.RawMessage(fmt.Sprintf(`{"tunnelId":%q}`, tunnelID.String())))
			if err := put(target.Bucket(bucketLinks), []byte(link.ID), &link); err != nil {
				return err
			}
			pairs[members[index].ID.String()+members[index+1].ID.String()] = true
		}
	}
	for _, connection := range data.topology.Connections {
		source, sourceOK := data.interfaces[connection.SourceID]
		targetInterface, targetOK := data.interfaces[connection.TargetID]
		if !sourceOK || !targetOK || pairs[connection.SourceID.String()+connection.TargetID.String()] {
			continue
		}
		link := migrateLink(connection.ID, source, targetInterface, data.owners, json.RawMessage(`{}`))
		if err := put(target.Bucket(bucketLinks), []byte(link.ID), &link); err != nil {
			return err
		}
	}
	return nil
}

func migrateLink(id uuid.UUID, source, target v1Interface, servers map[uuid.UUID]models.Server, spec json.RawMessage) models.Link {
	return models.Link{
		ID: id.String(), Kind: "awg", Generation: 1, Source: migrateEndpoint(source, servers[source.ID]), Target: migrateEndpoint(target, servers[target.ID]), Spec: spec,
		Status: models.LinkStatus{Phase: models.LinkPending, Conditions: pendingStatus().Conditions},
	}
}

func migrateEndpoint(iface v1Interface, server models.Server) models.LinkEndpoint {
	return models.LinkEndpoint{ServerID: server.ID.String(), ResourceID: iface.ID.String(), APIVersion: "awg.ks-tool.dev/v1", Kind: "Interface", Name: iface.Interface, Port: "tunnel", Address: server.SSH.Host}
}

func pendingStatus() models.ResourceStatus {
	now := time.Now().UTC()
	return models.ResourceStatus{Phase: models.ResourcePending, Conditions: []models.Condition{
		{Type: "Ready", Status: models.ConditionUnknown, Reason: "Unmanaged", Message: "waiting for vpn-agent and AWG provider migration", LastTransitionTime: now},
		{Type: "Managed", Status: models.ConditionFalse, Reason: "Unmanaged", Message: "resource has not been adopted by an agent provider", LastTransitionTime: now},
	}}
}

func pendingSecretCondition() models.Condition {
	return models.Condition{Type: "Secrets", Status: models.ConditionFalse, Reason: "SecretMigrationPending", Message: "private key remains in migration storage until the agent confirms migration", LastTransitionTime: time.Now().UTC()}
}

func migrateRegistrySources(source, target *bolt.Bucket) error {
	if source == nil {
		return nil
	}
	return source.ForEach(func(key, value []byte) error {
		if value == nil {
			return nil
		}
		var old v1RegistrySource
		if err := json.Unmarshal(value, &old); err != nil {
			return err
		}
		if old.Type == models.RegistrySourceAgent || old.Type == models.RegistrySourceProvider {
			return target.Put(key, value)
		}
		agentRepository := old.AgentRepository
		providerRepository := old.ProviderRepository
		baseURL := strings.TrimSuffix(old.URL, "/")
		if len(agentRepository) == 0 {
			agentRepository = "oci://ghcr.io/ks-tool/vpn-agent"
			if len(baseURL) > 0 && baseURL != "oci://ghcr.io" {
				agentRepository = baseURL + "/ks-tool/vpn-agent"
			}
		}
		if len(providerRepository) == 0 {
			providerRepository = "oci://ghcr.io/ks-tool/vpn-providers"
			if len(baseURL) > 0 && baseURL != "oci://ghcr.io" {
				providerRepository = baseURL + "/vpn-providers"
			}
		}
		for _, item := range []*models.RegistrySource{
			{ID: old.ID, URL: agentRepository, Type: models.RegistrySourceAgent, Username: old.Username, Password: old.Password, PlainHTTP: old.PlainHTTP},
			{ID: uuid.New(), URL: providerRepository, Type: models.RegistrySourceProvider, Username: old.Username, Password: old.Password, PlainHTTP: old.PlainHTTP},
		} {
			if err := put(target, binaryID(item.ID), item); err != nil {
				return err
			}
		}
		return nil
	})
}

func defaultRegistrySources() []*models.RegistrySource {
	return []*models.RegistrySource{
		{ID: uuid.New(), URL: "oci://ghcr.io/ks-tool/vpn-agent", Type: models.RegistrySourceAgent},
		{ID: uuid.New(), URL: "oci://ghcr.io/ks-tool/vpn-providers", Type: models.RegistrySourceProvider},
	}
}

func binaryID(id uuid.UUID) []byte {
	value := [16]byte(id)
	return value[:]
}

func keyString(key awgmodels.Key) string {
	return key.String()
}

func put(bucket *bolt.Bucket, key []byte, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return bucket.Put(key, data)
}
