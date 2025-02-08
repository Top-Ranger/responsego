// SPDX-License-Identifier: Apache-2.0
// Copyright 2020,2023,2025 Marcus Soll
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
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/Top-Ranger/responsego/helper"
	"github.com/Top-Ranger/responsego/registry"
	"github.com/Top-Ranger/responsego/translation"
)

func init() {
	err := registry.RegisterFeedbackPlugin(func() registry.FeedbackPlugin { return new(number) }, "Number")
	if err != nil {
		panic(err)
	}
}

const numberConfig = `
<h1>%s</h1>
<p>%s: <input id="Number" type="text"></p>
<p><button onclick="sendActivate('Number', JSON.stringify({'q': document.getElementById('Number').value}))">%s</button></p>
<p><button onclick="saveElement('Number', JSON.stringify({'q': document.getElementById('Number').value}), '%s: '+document.getElementById('Number').value)">%s</button></p>
`

const numberUser = `
<h1>{{.Question}}</h1>
{{$.Translation.DisplayNumber}}: <input id="numberInput" type="number" onchange="document.getElementById('numberButton').disabled = document.getElementById('numberInput').value == ''">
<button id="numberButton" onclick="if(!document.getElementById('numberInput').reportValidity()){return;};sendData('Number',document.getElementById('numberInput').value);document.getElementById('numberInput').disabled=true;document.getElementById('numberButton').disabled=true;" disabled>{{$.Translation.Submit}}</button>
`

var numberUserTemplate = template.Must(template.New("numberUser").Parse(numberUser))

type numberUserStruct struct {
	Question    string
	Translation translation.Translation
}

const numberAdmin = `
<h1>{{.Question}}</h1>
<table style="border: none;">
{{range $i, $e := .Answers}}
    <tr style="border: none;">
        <td style="border: none;">{{$e.Question}}</td>
		<td style="border: none;">{{$e.Count}}</td>
	</tr>
{{end}}
	<tr style="border: none;">
		<td style="border: none;"><em>{{.Translation.Submitted}}</em></td>
		<td style="border: none;"><em>{{.Submitted}}</em></td>
	</tr>
</table>
<p><button onclick="sendData('Number', 'close')">{{.Translation.Finish}}</button></p>
`

var numberAdminTemplate = template.Must(template.New("numberAdmin").Parse(numberAdmin))

type numberAdminStruct struct {
	Question string
	Answers  []struct {
		Question int
		Count    int
	}
	Submitted   int
	Translation translation.Translation
}

type number struct {
	adminHTML  chan<- template.HTML
	userHTML   chan<- template.HTML
	adminInput <-chan []byte
	userInput  <-chan []byte
	ctx        context.Context
	cancel     context.CancelFunc

	Question        string
	NumberAnswers   map[int]int
	NumberSubmitted int
	NumberChanged   bool
	AnswerLock      sync.Mutex
	Finished        bool
}

func (n *number) ConfigHTML() (string, template.HTML) {
	tl := translation.GetDefaultTranslation()
	return tl.DisplayNumber, template.HTML(fmt.Sprintf(numberConfig, template.HTMLEscapeString(tl.DisplayNumber), template.HTMLEscapeString(tl.DisplayQuestion), template.HTMLEscapeString(tl.Activate), template.HTMLEscapeString(tl.DisplayNumber), template.HTMLEscapeString(tl.SaveElement)))
}

func (n *number) AdminHTMLChannel(c chan<- template.HTML) {
	n.adminHTML = c
}

func (n *number) UserHTMLChannel(c chan<- template.HTML) {
	n.userHTML = c
}

func (n *number) ReceiveUserChannel(c <-chan []byte) {
	n.userInput = c
}

func (n *number) ReceiveAdminChannel(c <-chan []byte) {
	n.adminInput = c
}

func (n *number) Activate(b []byte) error {
	input := make(map[string]string)
	err := json.Unmarshal(b, &input)
	if err != nil {
		return err
	}

	n.Question = input["q"]
	if n.Question == "" {
		return fmt.Errorf("no question found")
	}

	n.NumberAnswers = make(map[int]int)

	go func() {
		td := numberUserStruct{
			Question:    n.Question,
			Translation: translation.GetDefaultTranslation(),
		}
		var buf bytes.Buffer
		err := numberUserTemplate.Execute(&buf, td)
		if err != nil {
			log.Printf("error executing numberUser: %s", err.Error())
		}
		n.userHTML <- template.HTML(buf.Bytes())
	}()
	go func() {
		n.adminHTML <- n.getAdminPage()
	}()

	n.ctx = context.Background()
	n.ctx, n.cancel = context.WithCancel(n.ctx)
	go func() {
		done := n.ctx.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case b := <-n.adminInput:
				n.AnswerLock.Lock()
				if string(b) == "close" && !n.Finished {
					n.Finished = true
					n.AnswerLock.Unlock()
					t := n.numberGetChart()
					n.adminHTML <- t
					n.userHTML <- t
				} else {
					n.AnswerLock.Unlock()
				}

			case b := <-n.userInput:
				i, err := strconv.Atoi(string(b))
				if err == nil {
					n.AnswerLock.Lock()
					n.NumberAnswers[i]++
					n.NumberSubmitted++
					n.NumberChanged = true
					n.AnswerLock.Unlock()
				}

			case <-ticker.C:
				n.AnswerLock.Lock()
				finished := n.Finished
				changed := n.NumberChanged
				n.NumberChanged = false
				n.AnswerLock.Unlock()

				if !finished && changed {
					n.adminHTML <- n.getAdminPage()
				}
			case <-done:
				return
			}
		}
	}()
	return nil
}

func (n *number) GetLastHTMLUser() template.HTML {

	if n.Finished {
		return n.numberGetChart()
	}

	n.AnswerLock.Lock()
	defer n.AnswerLock.Unlock()

	td := numberUserStruct{
		Question:    n.Question,
		Translation: translation.GetDefaultTranslation(),
	}
	var buf bytes.Buffer
	err := numberUserTemplate.Execute(&buf, td)
	if err != nil {
		log.Printf("error executing numberUser: %s", err.Error())
	}
	return template.HTML(buf.Bytes())
}

func (n *number) GetLastHTMLAdmin() template.HTML {
	if n.Finished {
		return n.numberGetChart()
	}
	return n.getAdminPage()
}

func (n *number) Deactivate() {
	if n.cancel != nil {
		n.cancel()
	}
}

func (n *number) numberGetChart() template.HTML {
	n.AnswerLock.Lock()
	defer n.AnswerLock.Unlock()

	keys := make([]int, 0, len(n.NumberAnswers))
	for k := range n.NumberAnswers {
		keys = append(keys, k)
	}

	sort.Sort(sort.IntSlice(keys))

	v := make([]helper.ChartValue, len(keys))

	for i := range keys {
		v[i].Label = strconv.Itoa(keys[i])
		v[i].Value = float64(n.NumberAnswers[keys[i]])
	}
	return helper.BarChart(v, "numberChart", n.Question)
}

func (n *number) getAdminPage() template.HTML {
	n.AnswerLock.Lock()
	defer n.AnswerLock.Unlock()

	keys := make([]int, 0, len(n.NumberAnswers))
	for k := range n.NumberAnswers {
		keys = append(keys, k)
	}

	sort.Sort(sort.IntSlice(keys))

	td := numberAdminStruct{
		Question: n.Question,
		Answers: make([]struct {
			Question int
			Count    int
		}, 0, len(n.NumberAnswers)),
		Submitted:   n.NumberSubmitted,
		Translation: translation.GetDefaultTranslation(),
	}
	for i := range keys {
		td.Answers = append(td.Answers, struct {
			Question int
			Count    int
		}{keys[i], n.NumberAnswers[keys[i]]})
	}

	var buf bytes.Buffer
	err := numberAdminTemplate.Execute(&buf, td)
	if err != nil {
		log.Printf("error executing numberAdmin: %s", err.Error())
	}
	return template.HTML(buf.Bytes())
}

func (n *number) GetAdminDownload() []byte {
	n.AnswerLock.Lock()
	defer n.AnswerLock.Unlock()

	b, err := json.Marshal(n.NumberAnswers)
	if err != nil {
		return []byte(err.Error())
	}
	return b
}
