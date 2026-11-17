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

// Package agentclient is a typed HTTP client over the agent's resource API
// and its transparent provider bridge. It takes an arbitrary *http.Client
// so the caller decides the transport — an SSH
// tunnel (internal/sshclient.TunnelClient) or a direct mTLS client — the
// client itself doesn't care which.
package agentclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	awgmodels "github.com/ks-tool/vpn-agent/v2/providers/awg/models"

	agentmodels "github.com/ks-tool/vpn-agent/v2/models"
)

// NotFoundError reports that the AWG provider has no metrics for the interface.
type NotFoundError struct {
	Interface string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("interface %q not found on agent", e.Interface)
}

// Client talks to a single agent's HTTP API.
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// New returns a Client that issues requests through httpClient against
// baseURL (e.g. "http://127.0.0.1:8080" for a tunnelled agent, or
// "https://<agent-addr>" for direct mTLS).
func New(httpClient *http.Client, baseURL string) *Client {
	return &Client{httpClient: httpClient, baseURL: strings.TrimSuffix(baseURL, "/")}
}

// Info fetches the host facts the agent discovered at startup — its backend and
// creatable interface kinds, whether Docker is available on the host, whether
// the agent itself runs in a container, kernel-module presence (GET /info).
func (c *Client) Info(ctx context.Context) (*agentmodels.HostInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/info", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET /info: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, responseError(resp, "")
	}

	var info agentmodels.HostInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode host info: %w", err)
	}
	return &info, nil
}

// Metrics fetches the agent's latest host snapshot (GET /metrics).
func (c *Client) Metrics(ctx context.Context) (*agentmodels.SystemSnapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/metrics", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET /metrics: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, responseError(resp, "")
	}

	var snap agentmodels.SystemSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		return nil, fmt.Errorf("decode system metrics snapshot: %w", err)
	}
	return &snap, nil
}

// InterfaceMetrics fetches one interface's peer metrics and agent-measured p95 latency.
func (c *Client) InterfaceMetrics(ctx context.Context, name string) (*awgmodels.InterfaceSnapshot, error) {
	var snapshot awgmodels.MetricsSnapshot
	if err := c.interfaceMetricsGet(ctx, "/v1/providers/awg/bridge/metrics", &snapshot); err != nil {
		return nil, err
	}
	for index := range snapshot.Interfaces {
		if snapshot.Interfaces[index].Interface == name {
			return &snapshot.Interfaces[index], nil
		}
	}
	return nil, &NotFoundError{Interface: name}
}

// InterfaceMetricsHistory fetches one interface's retained peer history.
func (c *Client) InterfaceMetricsHistory(ctx context.Context, name string) (*awgmodels.InterfaceHistory, error) {
	var history awgmodels.SystemHistory
	if err := c.interfaceMetricsGet(ctx, "/v1/providers/awg/bridge/metrics/history", &history); err != nil {
		return nil, err
	}
	for index := range history.Interfaces {
		if history.Interfaces[index].Interface == name {
			return &history.Interfaces[index], nil
		}
	}
	return nil, &NotFoundError{Interface: name}
}

func (c *Client) interfaceMetricsGet(ctx context.Context, path string, output any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return responseError(resp, "")
	}
	if err := json.NewDecoder(resp.Body).Decode(output); err != nil {
		return fmt.Errorf("decode interface metrics: %w", err)
	}
	return nil
}

// MetricsHistory fetches host-level samples retained by the agent (GET /metrics/history).
func (c *Client) MetricsHistory(ctx context.Context) (*agentmodels.SystemHistory, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/metrics/history", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET /metrics/history: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, responseError(resp, "")
	}

	var hist agentmodels.SystemHistory
	if err := json.NewDecoder(resp.Body).Decode(&hist); err != nil {
		return nil, fmt.Errorf("decode metrics history: %w", err)
	}
	return &hist, nil
}

// SetMetricsEnabled turns the agent's metrics collection on/off at runtime
// (PATCH /metrics). Not persisted by the agent across restarts — callers
// that want the setting to survive a redeploy re-apply it via SyncServer,
// same as interface config.
func (c *Client) SetMetricsEnabled(ctx context.Context, enabled bool) error {
	body, err := json.Marshal(map[string]bool{"enabled": enabled})
	if err != nil {
		return fmt.Errorf("marshal metrics state: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.baseURL+"/metrics", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	markIdempotent(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("PATCH /metrics: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return responseError(resp, "")
	}
	return nil
}

// SetProfilingEnabled turns the agent's Go runtime profiling (the /debug/pprof
// endpoints) on/off at runtime (PATCH /profiling). Like metrics, not persisted
// by the agent across restarts — re-applied via SyncServer.
func (c *Client) SetProfilingEnabled(ctx context.Context, enabled bool) error {
	body, err := json.Marshal(map[string]bool{"enabled": enabled})
	if err != nil {
		return fmt.Errorf("marshal profiling state: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.baseURL+"/profiling", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	markIdempotent(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("PATCH /profiling: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return responseError(resp, "")
	}
	return nil
}

// maxProfileBytes caps how much of a profiling dump the admin will read, so a
// runaway/hostile response can't exhaust its memory. Real pprof dumps are far
// smaller (KBs to a few MB).
const maxProfileBytes = 128 << 20 // 128 MiB

// Profile fetches a Go runtime profiling dump from the agent
// (GET /debug/pprof/<name>). For the CPU "profile" and "trace" kinds, seconds
// sets the sampling window (appended as ?seconds=N); it's ignored for the
// instantaneous kinds (heap/goroutine/allocs/…). The agent answers 403 unless
// profiling is enabled (SetProfilingEnabled). Returns the raw dump bytes.
func (c *Client) Profile(ctx context.Context, name string, seconds int) ([]byte, error) {
	u := c.baseURL + "/debug/pprof/" + name
	if seconds > 0 {
		u += fmt.Sprintf("?seconds=%d", seconds)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET /debug/pprof/%s: %w", name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, responseError(resp, "")
	}

	// Read one byte past the cap so hitting it is distinguishable from a genuine
	// EOF: io.ReadAll(io.LimitReader(body, N)) returns exactly N bytes with a nil
	// error when the body is larger, which would silently hand back a truncated —
	// and therefore corrupt, unparseable — dump. Error out instead.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxProfileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read profile: %w", err)
	}
	if int64(len(data)) > maxProfileBytes {
		return nil, fmt.Errorf("profile dump exceeds %d bytes; refusing to return a truncated dump", maxProfileBytes)
	}
	return data, nil
}

// markIdempotent tells net/http.Transport it's safe to silently retry req
// on a fresh connection if the pooled (often SSH-tunnelled, see
// internal/sshclient.TunnelClient) connection it picked turned out to
// already be dead — e.g. the agent process restarted, or the SSH session
// was idle long enough to be dropped. Without this, that case surfaces as
// a bare PUT/DELETE/PATCH EOF. Go only auto-retries safe HTTP methods on an
// unused connection; other methods require an explicit replayability signal
// (see (*Request).isReplayable and the Transport documentation). Resource
// PUT/DELETE and settings PATCH operations are idempotent, so repeating the
// exact same request is safe here. The
// value is intentionally empty: the agent doesn't need to see it, and an
// empty value tells Transport not to even put it on the wire (see the
// same doc comment).
func markIdempotent(req *http.Request) {
	req.Header.Set("Idempotency-Key", "")
}

func responseError(resp *http.Response, name string) error {
	if resp.StatusCode == http.StatusNotFound && len(name) > 0 {
		return &NotFoundError{Interface: name}
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("agent returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
}
