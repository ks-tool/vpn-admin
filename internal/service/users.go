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
	"fmt"
	"uuid"

	"github.com/ks-tool/vpn-admin/models"
)

func (s *Service) ListUsers() ([]models.User, error) {
	debugOp("ListUsers").Msg("listing users")
	return s.store.Users().List()
}

func (s *Service) GetUser(id string) (*models.User, error) {
	debugOp("GetUser").Str("user_id", id).Msg("getting user")
	uID, err := uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	u, err := s.store.Users().Get(uID)
	if err != nil {
		return nil, err
	}
	return u, nil
}

type UserInput struct {
	Name        string
	Description string
	Disabled    bool
}

func (s *Service) CreateUser(in UserInput) (*models.User, error) {
	debugOp("CreateUser").Str("name", in.Name).Msg("creating user")
	if len(in.Name) == 0 {
		return nil, fmt.Errorf("user name is required")
	}
	u := &models.User{ID: uuid.New(), Name: in.Name, Description: in.Description, Disabled: in.Disabled}
	if err := s.store.Users().Set(u); err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Service) UpdateUser(id string, in UserInput) (*models.User, error) {
	debugOp("UpdateUser").Str("user_id", id).Str("name", in.Name).Msg("updating user")
	uID, err := uuid.Parse(id)
	if err != nil {
		return nil, err
	}

	u, err := s.store.Users().Get(uID)
	if err != nil {
		return nil, err
	}
	u.Name = in.Name
	u.Description = in.Description
	u.Disabled = in.Disabled
	if err := s.store.Users().Set(u); err != nil {
		return nil, err
	}
	return u, nil
}

// DeleteUser revokes every Peer resource linked to the user, then deletes it.
func (s *Service) DeleteUser(id string) error {
	debugOp("DeleteUser").Str("user_id", id).Msg("deleting user")
	uID, err := uuid.Parse(id)
	if err != nil {
		return err
	}
	if _, err := s.store.Users().Get(uID); err != nil {
		return err
	}
	resources, err := s.allResources()
	if err != nil {
		return err
	}
	for _, resource := range resources {
		if resource.Kind != "Peer" {
			continue
		}
		var spec struct {
			UserRef string `json:"userRef"`
		}
		if err := json.Unmarshal(resource.Spec, &spec); err != nil {
			return err
		}
		if spec.UserRef != id {
			continue
		}
		if err := s.DeleteProviderResource(resource.ServerID, providerFromAPIVersion(resource.APIVersion), resource.Kind, resource.Name); err != nil {
			return fmt.Errorf("delete peer %q: %w", resource.Name, err)
		}
	}
	return s.store.Users().Delete(uID)
}
