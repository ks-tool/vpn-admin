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
	"strings"
	"testing"

	awgmodels "github.com/ks-tool/vpn-agent/v2/providers/awg/models"
)

func TestWriteAmneziaParamsMTU(t *testing.T) {
	for _, test := range []struct {
		name string
		mtu  int
		want string
	}{
		{name: "configured", mtu: 1280, want: "MTU = 1280\n"},
		{name: "unset"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var config strings.Builder
			writeAmneziaParams(&config, awgmodels.InterfaceConfig{MTU: test.mtu})
			if config.String() != test.want {
				t.Fatalf("parameters = %q, want %q", config.String(), test.want)
			}
		})
	}
}
