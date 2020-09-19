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

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	_ "github.com/Top-Ranger/responsego/authenticater"
	_ "github.com/Top-Ranger/responsego/plugin"
	"github.com/Top-Ranger/responsego/registry"
	"github.com/Top-Ranger/responsego/translation"
)

// ConfigStruct contains all configuration options for PollGo!
type ConfigStruct struct {
	Language                 string
	Address                  string
	GCMinutes                int
	PathImpressum            string
	PathDSGVO                string
	ServerPath               string
	ServerName               string
	LogFailedLogin           bool
	NeedAuthenticationForNew bool
	Authenticater            string
	AuthenticaterConfig      string
}

var config ConfigStruct
var authenticater registry.Authenticater

func loadConfig(path string) (ConfigStruct, error) {
	log.Printf("main: Loading config (%s)", path)
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return ConfigStruct{}, errors.New(fmt.Sprintln("Can not read config.json:", err))
	}

	c := ConfigStruct{}
	err = json.Unmarshal(b, &c)
	if err != nil {
		return ConfigStruct{}, errors.New(fmt.Sprintln("Error while parsing config.json:", err))
	}

	if !strings.HasPrefix(c.ServerPath, "/") && c.ServerPath != "" {
		log.Println("load config: ServerPath does not start with '/', adding it as a prefix")
		c.ServerPath = strings.Join([]string{"/", c.ServerPath}, "")
	}
	c.ServerPath = strings.TrimSuffix(c.ServerPath, "/")
	c.ServerName = strings.TrimSuffix(c.ServerName, "/")

	return c, nil
}

func main() {
	configPath := flag.String("config", "./config.json", "Path to json config for ResponseGo!")
	flag.Parse()

	c, err := loadConfig(*configPath)
	if err != nil {
		panic(err)
	}
	config = c

	err = translation.SetDefaultTranslation(config.Language)
	if err != nil {
		log.Panicf("main: Error setting default language '%s': %s", config.Language, err.Error())
	}
	log.Printf("main: Setting language to '%s'", config.Language)

	if config.NeedAuthenticationForNew {
		a, ok := registry.GetAuthenticater(config.Authenticater)
		if !ok {
			log.Panicf("main: Unknown Authenticater '%s'", c.Authenticater)
		}
		b, err := ioutil.ReadFile(config.AuthenticaterConfig)
		if err != nil {
			log.Panicf("main: Can not read %s: %s", c.AuthenticaterConfig, err.Error())
		}
		err = a.LoadConfig(b)
		if err != nil {
			log.Panicf("main: Can not load Authenticater '%s': %s", config.Authenticater, err.Error())
		}
		authenticater = a
	}

	RunServer()

	s := make(chan os.Signal)
	signal.Notify(s, os.Interrupt, syscall.SIGTERM)

	log.Println("main: waiting")

	for range s {
		StopServer()
		return
	}
}
