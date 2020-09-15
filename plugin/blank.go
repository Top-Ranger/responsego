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

package plugin

import (
	"context"
	"fmt"
	"html/template"

	"github.com/Top-Ranger/responsego/registry"
	"github.com/Top-Ranger/responsego/translation"
)

func init() {
	err := registry.RegisterFeedbackPlugin(func() registry.FeedbackPlugin { return new(blank) }, "Blank")
	if err != nil {
		panic(err)
	}
}

const blankConfig = `
<h1>%s</h1>
<p><button onclick="sendActivate('Blank', '')">%s</button></p>
<p><button onclick="saveElement('Blank', '', '%s')">%s</button></p>`

type blank struct {
	adminHTML  chan<- template.HTML
	userHTML   chan<- template.HTML
	adminInput <-chan []byte
	userInput  <-chan []byte
	ctx        context.Context
	cancel     context.CancelFunc
}

func (b *blank) ConfigHTML() template.HTML {
	tl := translation.GetDefaultTranslation()
	return template.HTML(fmt.Sprintf(blankConfig, template.HTMLEscapeString(tl.DisplayBlank), template.HTMLEscapeString(tl.Activate), template.HTMLEscapeString(tl.DisplayBlank), template.HTMLEscapeString(tl.SaveElement)))
}

func (b *blank) AdminHTMLChannel(c chan<- template.HTML) {
	b.adminHTML = c
}

func (b *blank) UserHTMLChannel(c chan<- template.HTML) {
	b.userHTML = c
}

func (b *blank) ReceiveUserChannel(c <-chan []byte) {
	b.userInput = c
}

func (b *blank) ReceiveAdminChannel(c <-chan []byte) {
	b.adminInput = c
}

func (b *blank) Activate(by []byte) error {
	go func() { b.userHTML <- "" }()
	go func() { b.adminHTML <- "" }()
	b.ctx = context.Background()
	b.ctx, b.cancel = context.WithCancel(b.ctx)
	go func() {
		done := b.ctx.Done()
		for {
			select {
			case <-b.adminInput:
			case <-b.userInput:
			case <-done:
				return
			}
		}
	}()
	return nil
}

func (b *blank) GetLastHTMLUser() template.HTML {
	return ""
}

func (b *blank) GetLastHTMLAdmin() template.HTML {
	return ""
}

func (b *blank) Deactivate() {
	if b.cancel != nil {
		b.cancel()
	}
}
