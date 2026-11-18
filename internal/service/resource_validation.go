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
	"uuid"
)

func (s *Service) applyPeerUserState(kind string, spec json.RawMessage) (json.RawMessage, error) {
	if kind != "Peer" {
		return spec, nil
	}
	var values map[string]any
	if err := json.Unmarshal(spec, &values); err != nil {
		return nil, err
	}
	userRef, _ := values["userRef"].(string)
	if len(userRef) == 0 {
		return spec, nil
	}
	userID, err := uuid.Parse(userRef)
	if err != nil {
		return nil, invalidInput("invalid peer userRef")
	}
	user, err := s.store.Users().Get(userID)
	if err != nil {
		return nil, err
	}
	if !user.Disabled {
		return spec, nil
	}
	values["disabled"] = true
	return json.Marshal(values)
}
