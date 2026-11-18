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

package models

import "uuid"

// Topology contains only neutral presentation metadata. Desired VPN state is
// stored in Resource and Link records.
type Topology struct {
	Servers    []TopologyServer   `json:"servers"`
	Interfaces []NetworkInterface `json:"interfaces"`
}

type TopologyServer struct {
	ID                uuid.UUID `json:"id"`
	Name              string    `json:"name"`
	Address           string    `json:"address,omitempty"`
	Description       string    `json:"description,omitempty"`
	Location          string    `json:"location,omitempty"`
	LocationError     string    `json:"locationError,omitempty"`
	Latitude          *float64  `json:"latitude,omitempty"`
	Longitude         *float64  `json:"longitude,omitempty"`
	CoordinatesManual bool      `json:"coordinatesManual,omitempty"`
	X                 float64   `json:"x"`
	Y                 float64   `json:"y"`
}

type NetworkInterface struct {
	ID       uuid.UUID `json:"id"`
	ServerID uuid.UUID `json:"serverId"`
	Name     string    `json:"name"`
	Address  string    `json:"address,omitempty"`
	X        float64   `json:"x"`
	Y        float64   `json:"y"`
}

type NodePosition struct {
	ID uuid.UUID `json:"id"`
	X  float64   `json:"x"`
	Y  float64   `json:"y"`
}
