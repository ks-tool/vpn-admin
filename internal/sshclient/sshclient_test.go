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

package sshclient

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"testing"

	"github.com/ks-tool/vpn-admin/models"

	"golang.org/x/crypto/ssh"
)

func TestAuthMethodUnencryptedKeyIgnoresPassphrase(t *testing.T) {
	keyData := marshalTestPrivateKey(t, "")
	if _, err := authMethod(models.SSHConfig{KeyData: string(keyData)}, "cached-passphrase"); err != nil {
		t.Fatalf("unencrypted key rejected a cached passphrase: %v", err)
	}
}

func TestParseSignerRequiresCorrectPassphrase(t *testing.T) {
	keyData := marshalTestPrivateKey(t, "correct")
	for _, passphrase := range []string{"", "wrong"} {
		_, err := parseSigner(keyData, passphrase)
		if _, ok := err.(*PassphraseRequiredError); !ok {
			t.Fatalf("passphrase %q returned %T %v, want PassphraseRequiredError", passphrase, err, err)
		}
	}
	if _, err := parseSigner(keyData, "correct"); err != nil {
		t.Fatalf("correct passphrase was rejected: %v", err)
	}
}

func marshalTestPrivateKey(t *testing.T, passphrase string) []byte {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(privateKey, "test")
	if len(passphrase) > 0 {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(privateKey, "test", []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(block)
}
