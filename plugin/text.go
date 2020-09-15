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

	"github.com/Top-Ranger/responsego/helper"
	"github.com/Top-Ranger/responsego/registry"
	"github.com/Top-Ranger/responsego/translation"
)

func init() {
	err := registry.RegisterFeedbackPlugin(func() registry.FeedbackPlugin { return new(text) }, "TextDisplay")
	if err != nil {
		panic(err)
	}
}

const textConfig = `
<h1>%s</h1>
<textarea class="fullwidth" id="text_textarea" rows="4"></textarea>
<p><button onclick="sendActivate('TextDisplay', document.getElementById('text_textarea').value)">%s</button></p>
<p><button onclick="saveElement('TextDisplay', document.getElementById('text_textarea').value, '%s: '+document.getElementById('text_textarea').value.substring(0,80)+(document.getElementById('text_textarea').value.length>80?'[...]':''))">%s</button></p>`

type text struct {
	adminHTML  chan<- template.HTML
	userHTML   chan<- template.HTML
	adminInput <-chan []byte
	userInput  <-chan []byte
	ctx        context.Context
	cancel     context.CancelFunc
	html       template.HTML
}

func (t *text) ConfigHTML() template.HTML {
	tl := translation.GetDefaultTranslation()
	return template.HTML(fmt.Sprintf(textConfig, template.HTMLEscapeString(tl.DisplayText), template.HTMLEscapeString(tl.Activate), template.HTMLEscapeString(tl.DisplayText), template.HTMLEscapeString(tl.SaveElement)))
}

func (t *text) AdminHTMLChannel(c chan<- template.HTML) {
	t.adminHTML = c
}

func (t *text) UserHTMLChannel(c chan<- template.HTML) {
	t.userHTML = c
}

func (t *text) ReceiveUserChannel(c <-chan []byte) {
	t.userInput = c
}

func (t *text) ReceiveAdminChannel(c <-chan []byte) {
	t.adminInput = c
}

func (t *text) Activate(b []byte) error {
	t.html = helper.Format(b)
	go func() { t.userHTML <- t.html }()
	go func() { t.adminHTML <- t.html }()
	t.ctx = context.Background()
	t.ctx, t.cancel = context.WithCancel(t.ctx)
	go func() {
		done := t.ctx.Done()
		for {
			select {
			case <-t.adminInput:
			case <-t.userInput:
			case <-done:
				return
			}
		}
	}()
	return nil
}

func (t *text) GetLastHTMLUser() template.HTML {
	return t.html
}

func (t *text) GetLastHTMLAdmin() template.HTML {
	return t.html
}

func (t *text) Deactivate() {
	if t.cancel != nil {
		t.cancel()
	}
}
