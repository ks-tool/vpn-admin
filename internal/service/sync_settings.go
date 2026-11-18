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
	"uuid"

	"github.com/ks-tool/vpn-admin/internal/agentclient"
	"github.com/rs/zerolog/log"
)

func (s *Service) syncMonitoringState(serverID uuid.UUID) {
	server, err := s.store.Servers().Get(serverID)
	if err != nil || !server.Agent.MonitoringDisabled {
		return
	}
	if err := s.callAgent(server, func(ctx context.Context, client *agentclient.Client) error {
		return client.SetMetricsEnabled(ctx, false)
	}); err != nil {
		log.Warn().Err(err).Str("server_id", serverID.String()).Msg("failed to re-apply monitoring state")
	}
}

func (s *Service) syncProfilingState(serverID uuid.UUID) {
	server, err := s.store.Servers().Get(serverID)
	if err != nil || !server.Agent.ProfilingEnabled {
		return
	}
	if err := s.callAgent(server, func(ctx context.Context, client *agentclient.Client) error {
		return client.SetProfilingEnabled(ctx, true)
	}); err != nil {
		log.Warn().Err(err).Str("server_id", serverID.String()).Msg("failed to re-apply profiling state")
	}
}
