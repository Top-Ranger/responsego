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

// Package registry provides a central way to register and use all available feedback plugins.
// All plugins should be registered prior to the program starting, normally through init().
package registry

import (
	"html/template"
	"sort"
	"sync"
)

// AlreadyRegisteredError represents an error where an option is already registeres
type AlreadyRegisteredError string

// Error returns the error description
func (a AlreadyRegisteredError) Error() string {
	return string(a)
}

// FeedbackPlugin represents an element for giving feedback
type FeedbackPlugin interface {
	ConfigHTML() template.HTML
	AdminHTMLChannel(chan<- template.HTML)
	UserHTMLChannel(chan<- template.HTML)
	ReceiveUserChannel(<-chan []byte)
	ReceiveAdminChannel(<-chan []byte)
	Activate([]byte) error
	Deactivate()
	GetLastHTMLUser() template.HTML
	GetLastHTMLAdmin() template.HTML
}

var (
	knownFeedbackPlugins      = make(map[string]func() FeedbackPlugin)
	knownFeedbackPluginsMutex = sync.RWMutex{}
)

// RegisterFeedbackPlugin registeres a data safe.
// The name of the data safe is used as an identifier and must be unique.
// You can savely use it in parallel.
func RegisterFeedbackPlugin(fp func() FeedbackPlugin, name string) error {
	knownFeedbackPluginsMutex.Lock()
	defer knownFeedbackPluginsMutex.Unlock()

	_, ok := knownFeedbackPlugins[name]
	if ok {
		return AlreadyRegisteredError("Feedback Plugins already registered")
	}
	knownFeedbackPlugins[name] = fp
	return nil
}

// GetFeedbackPlugins returns a feedback plugin.
// The bool indicates whether it existed. You can only use it if the bool is true.
func GetFeedbackPlugins(name string) (func() FeedbackPlugin, bool) {
	knownFeedbackPluginsMutex.RLock()
	defer knownFeedbackPluginsMutex.RUnlock()
	f, ok := knownFeedbackPlugins[name]
	return f, ok
}

// GetNamesOfFeedbackPlugins returns a list of names of all known Feedback plugins.
// The list is sorted alphabetically.
func GetNamesOfFeedbackPlugins() []string {
	knownFeedbackPluginsMutex.RLock()
	defer knownFeedbackPluginsMutex.RUnlock()

	s := make([]string, 0, len(knownFeedbackPlugins))
	for k := range knownFeedbackPlugins {
		s = append(s, k)
	}

	sort.StringSlice(s).Sort()
	return s
}
