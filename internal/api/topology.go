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

package api

import (
	"net/http"

	"github.com/ks-tool/vpn-admin/internal/service"
	"github.com/ks-tool/vpn-admin/models"
	"github.com/uptrace/bunrouter"
)

func topologyFields(r bunrouter.Request) map[string]any {
	return map[string]any{"method": r.Method, "path": r.URL.Path}
}

func (h *Handler) topologyGet(w http.ResponseWriter, r bunrouter.Request) error {
	topology, err := h.svc.GetTopology()
	if err != nil {
		return handleErr(err, topologyFields(r))
	}
	return bunrouter.JSON(w, topology)
}

func (h *Handler) topologyTunnelMetrics(w http.ResponseWriter, r bunrouter.Request) error {
	metrics, err := h.svc.GetTunnelMetrics()
	if err != nil {
		return handleErr(err, topologyFields(r))
	}
	return bunrouter.JSON(w, metrics)
}

func (h *Handler) topologyServerUpdate(w http.ResponseWriter, r bunrouter.Request) error {
	var input service.TopologyServerInput
	if err := decode(r, &input); err != nil {
		return badRequest(err)
	}
	topology, err := h.svc.UpdateTopologyServer(r.Param("serverID"), input)
	if err != nil {
		return handleErr(err, topologyFields(r))
	}
	return bunrouter.JSON(w, topology)
}

func (h *Handler) topologyServerDelete(w http.ResponseWriter, r bunrouter.Request) error {
	if err := h.svc.DeleteTopologyServer(r.Param("serverID")); err != nil {
		return handleErr(err, topologyFields(r))
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) topologyInterfaceCreate(w http.ResponseWriter, r bunrouter.Request) error {
	var input service.NetworkInterfaceInput
	if err := decode(r, &input); err != nil {
		return badRequest(err)
	}
	topology, err := h.svc.CreateNetworkInterface(r.Param("serverID"), input)
	if err != nil {
		return handleErr(err, topologyFields(r))
	}
	w.WriteHeader(http.StatusCreated)
	return bunrouter.JSON(w, topology)
}

func (h *Handler) topologyInterfaceUpdate(w http.ResponseWriter, r bunrouter.Request) error {
	var input service.NetworkInterfaceInput
	if err := decode(r, &input); err != nil {
		return badRequest(err)
	}
	topology, err := h.svc.UpdateNetworkInterface(r.Param("interfaceID"), input)
	if err != nil {
		return handleErr(err, topologyFields(r))
	}
	return bunrouter.JSON(w, topology)
}

func (h *Handler) topologyInterfaceDelete(w http.ResponseWriter, r bunrouter.Request) error {
	if err := h.svc.DeleteNetworkInterface(r.Param("interfaceID")); err != nil {
		return handleErr(err, topologyFields(r))
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) topologyPositionsUpdate(w http.ResponseWriter, r bunrouter.Request) error {
	var input struct {
		Positions []models.NodePosition `json:"positions"`
	}
	if err := decode(r, &input); err != nil {
		return badRequest(err)
	}
	if err := h.svc.MoveTopologyNodes(input.Positions); err != nil {
		return handleErr(err, topologyFields(r))
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
