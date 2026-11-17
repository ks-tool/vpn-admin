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
	"encoding/json"
	"errors"

	"github.com/ks-tool/vpn-admin/models"
)

func providerSecretArguments(secret *models.ResourceSecret) (json.RawMessage, error) {
	if len(secret.Arguments) > 0 {
		if !json.Valid(secret.Arguments) {
			return nil, errors.New("stored provider secret arguments are invalid JSON")
		}
		return secret.Arguments, nil
	}
	values := make(map[string]any)
	if len(secret.PrivateKey) > 0 {
		values["privateKey"] = secret.PrivateKey
	}
	if len(secret.PresharedKey) > 0 {
		values["presharedKey"] = secret.PresharedKey
	}
	for field, hooks := range map[string][]string{"preUp": secret.PreUp, "postUp": secret.PostUp, "preDown": secret.PreDown, "postDown": secret.PostDown} {
		if len(hooks) > 0 {
			values[field] = hooks
		}
	}
	if len(values) == 0 {
		return nil, nil
	}
	return json.Marshal(values)
}
