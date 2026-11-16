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
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"uuid"

	"github.com/ks-tool/vpn-admin/internal/pki"
	"github.com/ks-tool/vpn-admin/models"
	"github.com/ks-tool/vpn-admin/storage/boltdb"
	agentmodels "github.com/ks-tool/vpn-agent/v2/models"
	awgmodels "github.com/ks-tool/vpn-agent/v2/providers/awg/models"
)

type fakeAgentTLS struct {
	mu              sync.Mutex
	metrics         map[string]awgmodels.InterfaceSnapshot
	resources       map[string]models.Resource
	schemaCalls     int
	actionCalls     int
	actionArguments json.RawMessage
	putOrder        []string
	putProviders    []string
	deleteProviders []string
	commitCalls     int
	deleteCalls     int
	failCommit      bool
	awg             bool
	providerErrors  map[string]string
	info            agentmodels.HostInfo
}

func newFakeAgentTLS(t *testing.T) (*httptest.Server, *models.AgentTLS, *fakeAgentTLS) {
	t.Helper()
	fa := &fakeAgentTLS{metrics: make(map[string]awgmodels.InterfaceSnapshot), resources: make(map[string]models.Resource), info: agentmodels.HostInfo{Backend: "kernel", Version: "test"}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /metrics/{name}", func(w http.ResponseWriter, r *http.Request) {
		fa.mu.Lock()
		snapshot, ok := fa.metrics[r.PathValue("name")]
		fa.mu.Unlock()
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(snapshot)
	})
	mux.HandleFunc("GET /info", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(fa.info)
	})
	definition := models.ResourceDefinition{APIVersion: "test/v1", Kind: "Service", SpecSchema: json.RawMessage(`{"type":"object"}`), SchemaHash: "hash", Actions: []models.ActionDefinition{{Name: "importSecret", Title: "Import", ResultType: models.ActionResultJSON, Mutating: true}}, Ports: []models.PortDefinition{{Name: "tunnel", LinkKinds: []string{"test-link"}, RemotePorts: []string{"tunnel"}}}}
	testProvider := models.ProviderDefinition{Name: "test", Version: "1", Resources: []models.ResourceDefinition{definition, {APIVersion: "test/v1", Kind: "Interface", SpecSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"addr":{"type":"string"}}}`), Ports: definition.Ports}, {APIVersion: "test/v1", Kind: "Peer", SpecSchema: json.RawMessage(`{"type":"object"}`)}}}
	awgProvider := models.ProviderDefinition{Name: "awg", Version: "1", Resources: []models.ResourceDefinition{{APIVersion: "awg.ks-tool.dev/v1", Kind: "Interface", SpecSchema: json.RawMessage(`{"type":"object"}`), Actions: definition.Actions}, {APIVersion: "awg.ks-tool.dev/v1", Kind: "Peer", SpecSchema: json.RawMessage(`{"type":"object"}`), Actions: definition.Actions}}}
	mux.HandleFunc("GET /v1/manifest", func(w http.ResponseWriter, r *http.Request) {
		fa.mu.Lock()
		awg := fa.awg
		providerErrors := fa.providerErrors
		fa.mu.Unlock()
		providers := []models.ProviderDefinition{testProvider}
		if awg {
			providers = append(providers, awgProvider)
		}
		_ = json.NewEncoder(w).Encode(models.Manifest{ProtocolVersion: models.ProtocolVersion, Providers: providers, ProviderErrors: providerErrors})
	})
	mux.HandleFunc("GET /v1/providers/{provider}/schema", func(w http.ResponseWriter, r *http.Request) {
		fa.mu.Lock()
		fa.schemaCalls++
		fa.mu.Unlock()
		if r.PathValue("provider") == "awg" {
			_ = json.NewEncoder(w).Encode(awgProvider)
			return
		}
		_ = json.NewEncoder(w).Encode(testProvider)
	})
	mux.HandleFunc("GET /v1/providers/{provider}/resources", func(w http.ResponseWriter, r *http.Request) {
		fa.mu.Lock()
		out := make([]models.Resource, 0, len(fa.resources))
		for _, item := range fa.resources {
			if providerFromAPIVersion(item.APIVersion) == r.PathValue("provider") {
				out = append(out, item)
			}
		}
		fa.mu.Unlock()
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("PUT /v1/providers/{provider}/resources/{kind}/{name}", func(w http.ResponseWriter, r *http.Request) {
		var desired models.Resource
		if err := json.NewDecoder(r.Body).Decode(&desired); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fa.mu.Lock()
		fa.putProviders = append(fa.putProviders, r.PathValue("provider"))
		key := r.PathValue("kind") + "/" + r.PathValue("name")
		previous, exists := fa.resources[key]
		fa.putOrder = append(fa.putOrder, desired.Kind)
		if exists {
			desired.ID = previous.ID
			desired.Generation = previous.Generation + 1
		} else {
			if len(desired.ID) == 0 {
				desired.ID = "resource-id"
			}
			desired.Generation = 1
		}
		desired.Status = models.ResourceStatus{ObservedGeneration: desired.Generation, Phase: models.ResourceReady}
		fa.resources[key] = desired
		fa.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(desired)
	})
	mux.HandleFunc("GET /v1/providers/{provider}/resources/{kind}/{name}/status", func(w http.ResponseWriter, r *http.Request) {
		fa.mu.Lock()
		item := fa.resources[r.PathValue("kind")+"/"+r.PathValue("name")]
		fa.mu.Unlock()
		_ = json.NewEncoder(w).Encode(item.Status)
	})
	mux.HandleFunc("POST /v1/providers/{provider}/resources/{kind}/{name}/actions/{action}", func(w http.ResponseWriter, r *http.Request) {
		body, _ := json.Marshal(json.RawMessage(`{}`))
		var input struct {
			Arguments json.RawMessage `json:"arguments"`
		}
		_ = json.NewDecoder(r.Body).Decode(&input)
		fa.mu.Lock()
		fa.actionCalls++
		fa.actionArguments = input.Arguments
		fa.mu.Unlock()
		if r.PathValue("action") == "renderClientConfig" {
			_ = json.NewEncoder(w).Encode(models.ActionResult{Type: models.ActionResultFile, Data: json.RawMessage(`"test://client"`), FileName: "peer.txt"})
			return
		}
		_, _ = w.Write(body)
	})
	mux.HandleFunc("DELETE /v1/providers/{provider}/resources/{kind}/{name}", func(w http.ResponseWriter, r *http.Request) {
		fa.mu.Lock()
		fa.deleteProviders = append(fa.deleteProviders, r.PathValue("provider"))
		delete(fa.resources, r.PathValue("kind")+"/"+r.PathValue("name"))
		fa.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("POST /v1/providers/{provider}/links/prepare", func(w http.ResponseWriter, r *http.Request) {
		var request models.LinkRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(models.LinkOffer{LinkID: request.Link.ID, Endpoint: request.Endpoint, Data: json.RawMessage(`{"publicKey":"test-public-key"}`)})
	})
	mux.HandleFunc("POST /v1/providers/{provider}/links/commit", func(w http.ResponseWriter, r *http.Request) {
		fa.mu.Lock()
		defer fa.mu.Unlock()
		if fa.failCommit {
			fa.failCommit = false
			http.Error(w, "temporary commit failure", http.StatusInternalServerError)
			return
		}
		fa.commitCalls++
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("POST /v1/providers/{provider}/links/delete", func(w http.ResponseWriter, r *http.Request) {
		fa.mu.Lock()
		fa.deleteCalls++
		fa.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	})

	ts := httptest.NewUnstartedServer(mux)
	tlsMaterial, err := pki.IssueAgentTLS("test-agent", []net.IP{net.ParseIP("127.0.0.1")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	serverCert, err := tls.X509KeyPair([]byte(tlsMaterial.Server.Certificate), []byte(tlsMaterial.Server.PrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(tlsMaterial.CA.Certificate)) {
		t.Fatal("failed to load CA cert")
	}
	ts.TLS = &tls.Config{Certificates: []tls.Certificate{serverCert}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool, MinVersion: tls.VersionTLS12}
	ts.StartTLS()
	return ts, tlsMaterial, fa
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	db, err := boltdb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	svc := New(db)
	t.Cleanup(svc.StopTunnels)
	t.Cleanup(func() { _ = db.Close() })
	return svc
}

func newTestServerWithAgent(t *testing.T) (*Service, *models.Server, *fakeAgentTLS) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	ts, tlsMaterial, agent := newFakeAgentTLS(t)
	t.Cleanup(ts.Close)
	svc := newTestService(t)
	server, err := svc.CreateServer(ServerInput{Name: "srv1", SSH: models.SSHConfig{Host: "example.invalid", User: "root", Password: "x"}, Agent: models.Agent{Address: ts.Listener.Addr().String(), TLS: tlsMaterial}})
	if err != nil {
		t.Fatal(err)
	}
	return svc, server, agent
}

func TestSyncPreservesProviderOnlyPeerAndExplicitDeleteStillWorks(t *testing.T) {
	svc, server, agent := newTestServerWithAgent(t)
	agent.awg = true
	interfaceResource := models.Resource{ID: uuid.New().String(), ServerID: server.ID.String(), APIVersion: "awg.ks-tool.dev/v1", Kind: "Interface", Name: "awg0", Spec: json.RawMessage(`{"iface":"awg0"}`)}
	peerResource := models.Resource{ID: uuid.New().String(), ServerID: server.ID.String(), APIVersion: "awg.ks-tool.dev/v1", Kind: "Peer", Name: "alice", Spec: json.RawMessage(`{"interfaceRef":"` + interfaceResource.ID + `"}`)}
	for _, resource := range []models.Resource{peerResource, interfaceResource} {
		if err := svc.saveProviderResource(server.ID.String(), resource); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.SyncServer(server.ID.String()); err != nil {
		t.Fatal(err)
	}
	remoteOnly := models.Resource{ID: uuid.New().String(), ServerID: server.ID.String(), APIVersion: "awg.ks-tool.dev/v1", Kind: "Peer", Name: "remote-only", Generation: 1, Spec: json.RawMessage(`{"interfaceRef":"` + interfaceResource.ID + `"}`), Status: models.ResourceStatus{ObservedGeneration: 1, Phase: models.ResourceReady}}
	agent.mu.Lock()
	agent.resources["Peer/remote-only"] = remoteOnly
	agent.mu.Unlock()
	if err := svc.SyncServer(server.ID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.Resources(server.ID).Get(remoteOnly.ID); err != nil {
		t.Fatalf("provider-only peer was not preserved locally: %v", err)
	}
	if err := svc.DeleteProviderResource(server.ID.String(), "awg", "Peer", peerResource.Name); err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	_, exists := agent.resources["Peer/"+peerResource.Name]
	_, remoteExists := agent.resources["Peer/remote-only"]
	agent.mu.Unlock()
	if exists || !remoteExists {
		t.Fatalf("explicit deletion removed wrong peers: deleted=%v remote=%v", exists, remoteExists)
	}
}
