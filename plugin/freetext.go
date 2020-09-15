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
	"bytes"
	"context"
	"fmt"
	"html/template"
	"log"
	"sync"
	"time"

	"github.com/Top-Ranger/responsego/registry"
	"github.com/Top-Ranger/responsego/translation"
)

func init() {
	err := registry.RegisterFeedbackPlugin(func() registry.FeedbackPlugin { return new(freetext) }, "FreeText")
	if err != nil {
		panic(err)
	}
}

const freetextConfig = `
<h1>%s</h1>
<p>%s: <input id="FreeText" type="text"></p>
<p><button onclick="sendActivate('FreeText', document.getElementById('FreeText').value)">%s</button></p>
<p><button onclick="saveElement('FreeText', document.getElementById('FreeText').value, '%s: '+document.getElementById('FreeText').value)">%s</button></p>
`

const freetextUser = `
<h1>{{.Question}}</h1>
<textarea id="FreeText"></textarea>
<p><button onclick="sendData('FreeText', document.getElementById('FreeText').value); document.getElementById('FreeText').value = ''">{{.Translation.Submit}}</button></p>
`

var freetextUserTemplate = template.Must(template.New("freetextUser").Parse(freetextUser))

type freetextUserStruct struct {
	Question    string
	Translation translation.Translation
}

const freetextAdmin = `
<h1>{{.Question}}</h1>
<p>{{.Translation.UpdateAll5Seconds}}</p>
<ul>
{{range $i, $e := .Answers}}
<li>{{$e}}</li>
{{end}}
</ul>
`

var freetextAdminTemplate = template.Must(template.New("questionAdmin").Parse(freetextAdmin))

type freetextAdminStruct struct {
	Question    string
	Answers     []string
	Translation translation.Translation
}

type freetext struct {
	adminHTML  chan<- template.HTML
	userHTML   chan<- template.HTML
	adminInput <-chan []byte
	userInput  <-chan []byte
	ctx        context.Context
	cancel     context.CancelFunc

	Question      string
	Answers       []string
	NumberChanged bool
	AnswerLock    sync.Mutex
}

func (f *freetext) ConfigHTML() template.HTML {
	tl := translation.GetDefaultTranslation()
	return template.HTML(fmt.Sprintf(freetextConfig, template.HTMLEscapeString(tl.DisplayFreeText), template.HTMLEscapeString(tl.FreeTextQuestion), template.HTMLEscapeString(tl.Activate), template.HTMLEscapeString(tl.DisplayFreeText), template.HTMLEscapeString(tl.SaveElement)))
}

func (f *freetext) AdminHTMLChannel(c chan<- template.HTML) {
	f.adminHTML = c
}

func (f *freetext) UserHTMLChannel(c chan<- template.HTML) {
	f.userHTML = c
}

func (f *freetext) ReceiveUserChannel(c <-chan []byte) {
	f.userInput = c
}

func (f *freetext) ReceiveAdminChannel(c <-chan []byte) {
	f.adminInput = c
}

func (f *freetext) Activate(b []byte) error {
	f.Question = string(b)
	go func() {
		td := freetextUserStruct{
			Question:    f.Question,
			Translation: translation.GetDefaultTranslation(),
		}
		var buf bytes.Buffer
		err := freetextUserTemplate.Execute(&buf, td)
		if err != nil {
			log.Printf("error executing freetextUser: %s", err.Error())
		}
		f.userHTML <- template.HTML(buf.Bytes())
	}()
	go func() {
		f.adminHTML <- f.getAdminPage()
	}()

	f.ctx = context.Background()
	f.ctx, f.cancel = context.WithCancel(f.ctx)
	go func() {
		done := f.ctx.Done()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-f.adminInput:
			case b := <-f.userInput:
				f.AnswerLock.Lock()
				f.Answers = append(f.Answers, string(b))
				f.NumberChanged = true
				f.AnswerLock.Unlock()

			case <-ticker.C:
				f.AnswerLock.Lock()
				changed := f.NumberChanged
				f.NumberChanged = false
				f.AnswerLock.Unlock()
				if changed {
					f.adminHTML <- f.getAdminPage()
				}
			case <-done:
				return
			}
		}
	}()
	return nil
}

func (f *freetext) GetLastHTMLUser() template.HTML {
	f.AnswerLock.Lock()
	defer f.AnswerLock.Unlock()

	td := freetextUserStruct{
		Question:    f.Question,
		Translation: translation.GetDefaultTranslation(),
	}
	var buf bytes.Buffer
	err := freetextUserTemplate.Execute(&buf, td)
	if err != nil {
		log.Printf("error executing freetextUser: %s", err.Error())
	}
	return template.HTML(buf.Bytes())
}

func (f *freetext) GetLastHTMLAdmin() template.HTML {
	return f.getAdminPage()
}

func (f *freetext) Deactivate() {
	if f.cancel != nil {
		f.cancel()
	}
}

func (f *freetext) getAdminPage() template.HTML {
	f.AnswerLock.Lock()
	defer f.AnswerLock.Unlock()

	td := freetextAdminStruct{
		Question:    f.Question,
		Answers:     f.Answers,
		Translation: translation.GetDefaultTranslation(),
	}

	var buf bytes.Buffer
	err := freetextAdminTemplate.Execute(&buf, td)
	if err != nil {
		log.Printf("error executing freetextAdmin: %s", err.Error())
	}
	return template.HTML(buf.Bytes())
}
