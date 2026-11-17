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
	"encoding/hex"
	"encoding/json"
	"testing"
	"uuid"

	"github.com/ks-tool/vpn-admin/models"
	awgmodels "github.com/ks-tool/vpn-agent/v2/providers/awg/models"
)

func TestGetTunnelMetricsMatchesDestinationPublicKey(t *testing.T) {
	svc := newTestService(t)
	sourceServer, sourceAgent := newLinkTestServer(t, svc, "a")
	targetServer, _ := newLinkTestServer(t, svc, "b")
	source := models.Resource{ID: uuid.New().String(), ServerID: sourceServer.ID.String(), APIVersion: "test/v1", Kind: "Interface", Name: "office", Spec: []byte(`{"iface":"awg0"}`)}
	targetKey, err := awgmodels.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	targetPublicKey := targetKey.PublicKey()
	providerStatus, _ := json.Marshal(map[string]string{"publicKey": targetPublicKey.String()})
	target := models.Resource{ID: uuid.New().String(), ServerID: targetServer.ID.String(), APIVersion: "test/v1", Kind: "Interface", Name: "awg0", Spec: []byte(`{}`), Status: models.ResourceStatus{Provider: providerStatus}}
	for _, resource := range []models.Resource{source, target} {
		if err := svc.saveProviderResource(resource.ServerID, resource); err != nil {
			t.Fatal(err)
		}
	}
	connection, err := svc.CreateProviderLink(source.ID, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	p95 := 87.5
	sourceAgent.metrics["awg0"] = awgmodels.InterfaceSnapshot{Interface: "awg0", Peers: []awgmodels.PeerSnapshot{{PublicKey: hex.EncodeToString(targetPublicKey[:]), LatencyP95Ms: &p95}}}
	metrics, err := svc.GetTunnelMetrics()
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics) != 1 || metrics[0].ConnectionID != "link:"+connection.ID || metrics[0].LatencyP95Ms == nil || *metrics[0].LatencyP95Ms != p95 {
		t.Fatalf("unexpected tunnel metrics: %+v", metrics)
	}
}
