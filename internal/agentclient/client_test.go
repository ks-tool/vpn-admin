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

package agentclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	agentmodels "github.com/ks-tool/vpn-agent/v2/models"
)

func TestClientInfo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/info" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(agentmodels.HostInfo{Backend: "kernel", Version: "test"})
	}))
	defer server.Close()
	info, err := New(server.Client(), server.URL).Info(context.Background())
	if err != nil || info.Backend != "kernel" || info.Version != "test" {
		t.Fatalf("Info = %+v, %v", info, err)
	}
}
