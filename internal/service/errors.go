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
	"fmt"
	"time"
)

const pushTimeout = 15 * time.Second

type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalidInput(format string, args ...any) error {
	return &ValidationError{Msg: fmt.Sprintf(format, args...)}
}
