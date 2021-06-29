// SPDX-License-Identifier: Apache-2.0
// Copyright 2020,2021 Marcus Soll
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	  http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package authenticater

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/Top-Ranger/responsego/registry"
	"golang.org/x/crypto/bcrypt"
)

// PlainFile is a simple Authenticater which takes a file cpontaining a two dimensional JSON Array as a configuration.
// [
//     ["user1", "bcrypthash_password1"],
//     ["user2", "bcrypthash_password2"]
// ]
// A username can only be specified once.
// All entries must be in plain text. The hash must be a base64 encoded password. Can be generated using tools like caddy hash-password (https://caddyserver.com/docs/command-line#caddy-hash-password).
type BcryptFile struct {
	users map[string][]byte
}

func init() {
	err := registry.RegisterAuthenticater(&BcryptFile{}, "BcryptFile")
	if err != nil {
		panic(err)
	}
}

// LoadConfig loads the configuration. It is assumed that this is only called once before Authenticate is called.
func (bf *BcryptFile) LoadConfig(b []byte) error {
	bf.users = make(map[string][]byte)
	data := make([][]string, 0)
	err := json.Unmarshal(b, &data)
	if err != nil {
		return err
	}
	for i := range data {
		if len(data[i]) != 2 {
			return fmt.Errorf("entry %d has length %d, but must be 2 [username, bcrypthash_password]", i, len(data[i]))
		}
		_, ok := bf.users[data[i][0]]
		if ok {
			return fmt.Errorf("user %s found more than once", data[i][0])
		}
		// try decoding
		decoded, err := base64.StdEncoding.DecodeString(data[i][1])
		if err != nil {
			return fmt.Errorf("user %s hash can not be decoded: %w", data[i][0], err)
		}
		bf.users[data[i][0]] = decoded
	}
	return nil
}

// Authenticate validates a user/password configuration. It is safe for parallel usage.
func (bf *BcryptFile) Authenticate(user, password string) (bool, error) {
	pw, ok := bf.users[user]
	if !ok {
		return false, nil
	}
	if err := bcrypt.CompareHashAndPassword(pw, []byte(password)); err == nil {
		return true, nil
	}
	return false, nil
}
