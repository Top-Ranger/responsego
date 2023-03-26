// SPDX-License-Identifier: Apache-2.0
// Copyright 2020,2023 Marcus Soll
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
	err := registry.RegisterFeedbackPlugin(func() registry.FeedbackPlugin { return new(timeQuestion) }, "TimeQuestion")
	if err != nil {
		panic(err)
	}
}

const timeQuestionConfig = `
<h1>%s</h1>
<p>%s: <input id="TimeQuestion" type="text"></p>
<p>%s: <input id="TimeQuestionPrecision" type="number" min="1" max="60" value="1"> %s</p>
<p><button onclick="sendActivate('TimeQuestion', JSON.stringify({'q': document.getElementById('TimeQuestion').value, 'p': document.getElementById('TimeQuestionPrecision').value}))">%s</button></p>
<p><button onclick="saveElement('TimeQuestion', JSON.stringify({'q': document.getElementById('TimeQuestion').value, 'p': document.getElementById('TimeQuestionPrecision').value}), '%s: '+document.getElementById('TimeQuestion').value)">%s</button></p>
`

const timeQuestionUser = `
<h1>{{.Question}}</h1>
{{$.Translation.DisplayTimeQuestion}}: <input id="timeQuestionInput" type="time" onchange="document.getElementById('timeQuestionButton').disabled = document.getElementById('timeQuestionInput').value == ''">
<button id="timeQuestionButton" onclick="if(!document.getElementById('timeQuestionInput').reportValidity()){return;};sendData('TimeQuestion',document.getElementById('timeQuestionInput').value);document.getElementById('timeQuestionInput').disabled=true;document.getElementById('timeQuestionButton').disabled=true;" disabled>{{$.Translation.Submit}}</button>
`

var timeQuestionUserTemplate = template.Must(template.New("timeQuestionUser").Parse(timeQuestionUser))

type timeQuestionUserStruct struct {
	Question    string
	Translation translation.Translation
}

const timeQuestionAdmin = `
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
<p><button onclick="sendData('TimeQuestion', 'close')">{{.Translation.Finish}}</button></p>
`

var timeQuestionAdminTemplate = template.Must(template.New("timeQuestionAdmin").Parse(timeQuestionAdmin))

type timeQuestionAdminStruct struct {
	Question string
	Answers  []struct {
		Question string
		Count    int
	}
	Submitted   int
	Translation translation.Translation
}

type timeQuestion struct {
	adminHTML  chan<- template.HTML
	userHTML   chan<- template.HTML
	adminInput <-chan []byte
	userInput  <-chan []byte
	ctx        context.Context
	cancel     context.CancelFunc

	Question            string
	Precision           time.Duration
	TimeQuestionAnswers map[time.Time]int
	NumberSubmitted     int
	TimeQuestionChanged bool
	AnswerLock          sync.Mutex
	Finished            bool
}

func (n *timeQuestion) ConfigHTML() (string, template.HTML) {
	tl := translation.GetDefaultTranslation()
	return tl.DisplayTimeQuestion, template.HTML(fmt.Sprintf(timeQuestionConfig, template.HTMLEscapeString(tl.DisplayTimeQuestion), template.HTMLEscapeString(tl.DisplayQuestion), template.HTMLEscapeString(tl.Precision), template.HTMLEscapeString(tl.Minutes), template.HTMLEscapeString(tl.Activate), template.HTMLEscapeString(tl.DisplayTimeQuestion), template.HTMLEscapeString(tl.SaveElement)))
}

func (n *timeQuestion) AdminHTMLChannel(c chan<- template.HTML) {
	n.adminHTML = c
}

func (n *timeQuestion) UserHTMLChannel(c chan<- template.HTML) {
	n.userHTML = c
}

func (n *timeQuestion) ReceiveUserChannel(c <-chan []byte) {
	n.userInput = c
}

func (n *timeQuestion) ReceiveAdminChannel(c <-chan []byte) {
	n.adminInput = c
}

func (n *timeQuestion) Activate(b []byte) error {
	input := make(map[string]string)
	err := json.Unmarshal(b, &input)
	if err != nil {
		return err
	}

	n.Question = input["q"]
	if n.Question == "" {
		return fmt.Errorf("no question found")
	}

	precision := input["p"]
	if precision == "" {
		return fmt.Errorf("no precision found")
	}

	p, err := strconv.Atoi(precision)
	if err != nil {
		return fmt.Errorf("can not parse precision: %w", err)
	}

	n.Precision = time.Duration(p) * time.Minute

	n.TimeQuestionAnswers = make(map[time.Time]int)

	go func() {
		td := timeQuestionUserStruct{
			Question:    n.Question,
			Translation: translation.GetDefaultTranslation(),
		}
		var buf bytes.Buffer
		err := timeQuestionUserTemplate.Execute(&buf, td)
		if err != nil {
			log.Printf("error executing timeQuestionUser: %s", err.Error())
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
					t := n.timeQuestionGetChart()
					n.adminHTML <- t
					n.userHTML <- t
				} else {
					n.AnswerLock.Unlock()
				}

			case b := <-n.userInput:
				t, err := time.Parse("15:04", string(b))
				if err == nil {
					t = t.Truncate(n.Precision)
					n.AnswerLock.Lock()
					n.TimeQuestionAnswers[t]++
					n.NumberSubmitted++
					n.TimeQuestionChanged = true
					n.AnswerLock.Unlock()
				}

			case <-ticker.C:
				n.AnswerLock.Lock()
				finished := n.Finished
				changed := n.TimeQuestionChanged
				n.TimeQuestionChanged = false
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

func (n *timeQuestion) GetLastHTMLUser() template.HTML {

	if n.Finished {
		return n.timeQuestionGetChart()
	}

	n.AnswerLock.Lock()
	defer n.AnswerLock.Unlock()

	td := timeQuestionUserStruct{
		Question:    n.Question,
		Translation: translation.GetDefaultTranslation(),
	}
	var buf bytes.Buffer
	err := timeQuestionUserTemplate.Execute(&buf, td)
	if err != nil {
		log.Printf("error executing timeQuestionUser: %s", err.Error())
	}
	return template.HTML(buf.Bytes())
}

func (n *timeQuestion) GetLastHTMLAdmin() template.HTML {
	if n.Finished {
		return n.timeQuestionGetChart()
	}
	return n.getAdminPage()
}

func (n *timeQuestion) Deactivate() {
	if n.cancel != nil {
		n.cancel()
	}
}

func (n *timeQuestion) timeQuestionGetChart() template.HTML {
	n.AnswerLock.Lock()
	defer n.AnswerLock.Unlock()

	keys := make([]time.Time, 0, len(n.TimeQuestionAnswers))
	for k := range n.TimeQuestionAnswers {
		keys = append(keys, k)
	}

	sort.Slice(keys, func(i, j int) bool { return keys[i].Before(keys[j]) })

	v := make([]helper.ChartValue, len(keys))

	for i := range keys {
		v[i].Label = fmt.Sprintf("%02d:%02d", keys[i].Hour(), keys[i].Minute())
		v[i].Value = float64(n.TimeQuestionAnswers[keys[i]])
	}
	return helper.BarChart(v, "timeQuestionChart", n.Question)
}

func (n *timeQuestion) getAdminPage() template.HTML {
	n.AnswerLock.Lock()
	defer n.AnswerLock.Unlock()

	keys := make([]time.Time, 0, len(n.TimeQuestionAnswers))
	for k := range n.TimeQuestionAnswers {
		keys = append(keys, k)
	}

	sort.Slice(keys, func(i, j int) bool { return keys[i].Before(keys[j]) })

	td := timeQuestionAdminStruct{
		Question: n.Question,
		Answers: make([]struct {
			Question string
			Count    int
		}, 0, len(n.TimeQuestionAnswers)),
		Submitted:   n.NumberSubmitted,
		Translation: translation.GetDefaultTranslation(),
	}
	for i := range keys {
		td.Answers = append(td.Answers, struct {
			Question string
			Count    int
		}{fmt.Sprintf("%02d:%02d", keys[i].Hour(), keys[i].Minute()), n.TimeQuestionAnswers[keys[i]]})
	}

	var buf bytes.Buffer
	err := timeQuestionAdminTemplate.Execute(&buf, td)
	if err != nil {
		log.Printf("error executing timeQuestionAdmin: %s", err.Error())
	}
	return template.HTML(buf.Bytes())
}
