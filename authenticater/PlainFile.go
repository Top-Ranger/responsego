// SPDX-License-Identifier: Apache-2.0
// Copyright 2020 Marcus Soll
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
	"encoding/json"
	"fmt"

	"github.com/Top-Ranger/responsego/helper"
	"github.com/Top-Ranger/responsego/registry"
)

// PlainFile is a simple Authenticater which takes a file cpontaining a two dimensional JSON Array as a configuration.
//[
//    ["user1", "password1"],
//    ["user2", "password2"]
//]
//A username can only be specified once.
//All entries must be in plain text
type PlainFile struct {
	users map[string]string
}

func init() {
	err := registry.RegisterAuthenticater(&PlainFile{}, "PlainFile")
	if err != nil {
		panic(err)
	}
}

// LoadConfig loads the configuration. It is assumed that this is only called once before Authenticate is called.
func (p *PlainFile) LoadConfig(b []byte) error {
	p.users = make(map[string]string)
	data := make([][]string, 0)
	err := json.Unmarshal(b, &data)
	if err != nil {
		return err
	}
	for i := range data {
		if len(data[i]) != 2 {
			return fmt.Errorf("entry %d has length %d, but must be 2 [username, password]", i, len(data[i]))
		}
		_, ok := p.users[data[i][0]]
		if ok {
			return fmt.Errorf("user %s found more than once", data[i][0])
		}
		p.users[data[i][0]] = helper.EncodePassword(data[i][1])
	}
	return nil
}

// Authenticate validates a user/password configuration. It is safe for parallel usage.
func (p *PlainFile) Authenticate(user, password string) (bool, error) {
	pw, ok := p.users[user]
	if !ok {
		return false, nil
	}
	if pw == helper.EncodePassword(password) {
		return true, nil
	}
	return false, nil
}
