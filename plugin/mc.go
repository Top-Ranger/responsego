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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Top-Ranger/responsego/helper"
	"github.com/Top-Ranger/responsego/registry"
	"github.com/Top-Ranger/responsego/translation"
)

func init() {
	err := registry.RegisterFeedbackPlugin(func() registry.FeedbackPlugin { return new(mc) }, "MultipleChoice")
	if err != nil {
		panic(err)
	}
}

const mcConfig = `
<h1>%s</h1>
<p>%s: <input id="MC" type="text"></p>
<table style="border: none;">
    <tr style="border: none; background-color: inherit;">
        <td style="border: none;"><input id="MC_1" type="text"></td>
        <td style="border: none;"><input id="MC_2" type="text"></td>
        <td style="border: none;"><input id="MC_3" type="text"></td>
    </tr>
    <tr style="border: none; background-color: inherit;">
        <td style="border: none;"><input id="MC_4" type="text"></td>
        <td style="border: none;"><input id="MC_5" type="text"></td>
        <td style="border: none;"><input id="MC_6" type="text"></td>
    </tr>
    <tr style="border: none; background-color: inherit;">
        <td style="border: none;"><input id="MC_7" type="text"></td>
        <td style="border: none;"><input id="MC_8" type="text"></td>
        <td style="border: none;"><input id="MC_9" type="text"></td>
    </tr>
</table>
<p><button onclick="sendActivate('MultipleChoice', JSON.stringify({'1': document.getElementById('MC_1').value, '2': document.getElementById('MC_2').value, '3': document.getElementById('MC_3').value, '4': document.getElementById('MC_4').value, '5': document.getElementById('MC_5').value, '6': document.getElementById('MC_6').value, '7': document.getElementById('MC_7').value, '8': document.getElementById('MC_8').value, '9': document.getElementById('MC_9').value, 'q': document.getElementById('MC').value}))">%s</button></p>
<p><button onclick="saveElement('MultipleChoice', JSON.stringify({'1': document.getElementById('MC_1').value, '2': document.getElementById('MC_2').value, '3': document.getElementById('MC_3').value, '4': document.getElementById('MC_4').value, '5': document.getElementById('MC_5').value, '6': document.getElementById('MC_6').value, '7': document.getElementById('MC_7').value, '8': document.getElementById('MC_8').value, '9': document.getElementById('MC_9').value, 'q': document.getElementById('MC').value}), '%s: '+document.getElementById('MC').value)">%s</button></p>
`

const mcUser = `
<h1>{{.Question}}</h1>
<table style="border: none;">
{{range $i, $e := .Answers}}
    <tr style="border: none;">
        <td id="Question_cell_{{$i}}" style="border: none;"><label for="Question_check_{{$i}}">{{$e}}</label></td>
		<td style="border: none;"><input type="checkbox" id="Question_check_{{$i}}" name="{{$i}}" value="checked"></td>
	</tr>
{{end}}
</table>
<button id="Question_button" onclick="sendData('MultipleChoice',''{{range $i, $e := .Answers}}+document.getElementById('Question_check_{{$i}}').checked+';'{{end}});document.getElementById('Question_button').disabled=true;var e=document.getElementById('Question_check_0'); if(e!=null){e.disabled=true; if(e.checked){document.getElementById('Question_cell_0').style.backgroundColor='var(--primary-colour-dark)'};};var e=document.getElementById('Question_check_1'); if(e!=null){e.disabled=true;if(e.checked){document.getElementById('Question_cell_1').style.backgroundColor='var(--primary-colour-dark)'};};var e=document.getElementById('Question_check_2'); if(e!=null){e.disabled=true;if(e.checked){document.getElementById('Question_cell_2').style.backgroundColor='var(--primary-colour-dark)'};};var e=document.getElementById('Question_check_3'); if(e!=null){e.disabled=true;if(e.checked){document.getElementById('Question_cell_3').style.backgroundColor='var(--primary-colour-dark)'};};var e=document.getElementById('Question_check_4'); if(e!=null){e.disabled=true;if(e.checked){document.getElementById('Question_cell_4').style.backgroundColor='var(--primary-colour-dark)'};};var e=document.getElementById('Question_check_5'); if(e!=null){e.disabled=true;if(e.checked){document.getElementById('Question_cell_5').style.backgroundColor='var(--primary-colour-dark)'};};var e=document.getElementById('Question_check_6'); if(e!=null){e.disabled=true;if(e.checked){document.getElementById('Question_cell_6').style.backgroundColor='var(--primary-colour-dark)'};};var e=document.getElementById('Question_check_7'); if(e!=null){e.disabled=true;if(e.checked){document.getElementById('Question_cell_7').style.backgroundColor='var(--primary-colour-dark)'};};var e=document.getElementById('Question_check_8'); if(e!=null){e.disabled=true;if(e.checked){document.getElementById('Question_cell_8').style.backgroundColor='var(--primary-colour-dark)'};};">{{$.Translation.Submit}}</button>
`

var mcUserTemplate = template.Must(template.New("mcUser").Parse(mcUser))

type mcUserStruct struct {
	Question    string
	Answers     []string
	Translation translation.Translation
}

const mcAdmin = `
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
<p><button onclick="sendData('MultipleChoice', 'close')">{{.Translation.Finish}}</button></p>
`

var mcAdminTemplate = template.Must(template.New("mcAdmin").Parse(mcAdmin))

type mcAdminStruct struct {
	Question string
	Answers  []struct {
		Question string
		Count    int
	}
	Submitted   int
	Translation translation.Translation
}

type mc struct {
	adminHTML  chan<- template.HTML
	userHTML   chan<- template.HTML
	adminInput <-chan []byte
	userInput  <-chan []byte
	ctx        context.Context
	cancel     context.CancelFunc

	Question        string
	QuestionAnswers []string
	AnswerCount     []int
	NumberSubmitted int
	NumberChanged   bool
	AnswerLock      sync.Mutex
	Finished        bool
}

func (q *mc) ConfigHTML() (string, template.HTML) {
	tl := translation.GetDefaultTranslation()
	return tl.DisplayMultipleChoice, template.HTML(fmt.Sprintf(mcConfig, template.HTMLEscapeString(tl.DisplayMultipleChoice), template.HTMLEscapeString(tl.DisplayMultipleChoice), template.HTMLEscapeString(tl.Activate), template.HTMLEscapeString(tl.DisplayMultipleChoice), template.HTMLEscapeString(tl.SaveElement)))
}

func (q *mc) AdminHTMLChannel(c chan<- template.HTML) {
	q.adminHTML = c
}

func (q *mc) UserHTMLChannel(c chan<- template.HTML) {
	q.userHTML = c
}

func (q *mc) ReceiveUserChannel(c <-chan []byte) {
	q.userInput = c
}

func (q *mc) ReceiveAdminChannel(c <-chan []byte) {
	q.adminInput = c
}

func (q *mc) Activate(b []byte) error {
	input := make(map[string]string)
	err := json.Unmarshal(b, &input)
	if err != nil {
		return err
	}

	q.Question = input["q"]
	if q.Question == "" {
		return fmt.Errorf("no question found")
	}

	for i := 1; i <= 9; i++ {
		s := input[fmt.Sprintf("%d", i)]
		if s != "" {
			q.QuestionAnswers = append(q.QuestionAnswers, s)
		}
	}

	if len(q.QuestionAnswers) == 0 {
		return fmt.Errorf("no answers found")
	}

	q.AnswerCount = make([]int, len(q.QuestionAnswers))

	go func() {
		td := mcUserStruct{
			Question:    q.Question,
			Answers:     q.QuestionAnswers,
			Translation: translation.GetDefaultTranslation(),
		}
		var buf bytes.Buffer
		err := mcUserTemplate.Execute(&buf, td)
		if err != nil {
			log.Printf("error executing mcUser: %s", err.Error())
		}
		q.userHTML <- template.HTML(buf.Bytes())
	}()
	go func() {
		q.adminHTML <- q.getAdminPage()
	}()

	q.ctx = context.Background()
	q.ctx, q.cancel = context.WithCancel(q.ctx)
	go func() {
		done := q.ctx.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case b := <-q.adminInput:
				q.AnswerLock.Lock()
				if string(b) == "close" && !q.Finished {
					q.Finished = true
					q.AnswerLock.Unlock()
					t := q.questionGetChart()
					q.adminHTML <- t
					q.userHTML <- t
				} else {
					q.AnswerLock.Unlock()
				}

			case b := <-q.userInput:
				split := strings.Split(string(b), ";")
				q.AnswerLock.Lock()
				if len(split) >= len(q.AnswerCount) {
					q.NumberSubmitted++
					q.NumberChanged = true
					for i := range q.AnswerCount {
						b, err := strconv.ParseBool(split[i])
						if err == nil && b {
							q.AnswerCount[i]++
						}
					}
				}
				q.AnswerLock.Unlock()
			case <-ticker.C:
				q.AnswerLock.Lock()
				finished := q.Finished
				changed := q.NumberChanged
				q.NumberChanged = false
				q.AnswerLock.Unlock()

				if !finished && changed {
					q.adminHTML <- q.getAdminPage()
				}
			case <-done:
				return
			}
		}
	}()
	return nil
}

func (q *mc) GetLastHTMLUser() template.HTML {

	if q.Finished {
		return q.questionGetChart()
	}

	q.AnswerLock.Lock()
	defer q.AnswerLock.Unlock()

	td := mcUserStruct{
		Question:    q.Question,
		Answers:     q.QuestionAnswers,
		Translation: translation.GetDefaultTranslation(),
	}
	var buf bytes.Buffer
	err := mcUserTemplate.Execute(&buf, td)
	if err != nil {
		log.Printf("error executing mcUser: %s", err.Error())
	}
	return template.HTML(buf.Bytes())
}

func (q *mc) GetLastHTMLAdmin() template.HTML {
	if q.Finished {
		return q.questionGetChart()
	}
	return q.getAdminPage()
}

func (q *mc) Deactivate() {
	if q.cancel != nil {
		q.cancel()
	}
}

func (q *mc) questionGetChart() template.HTML {
	q.AnswerLock.Lock()
	defer q.AnswerLock.Unlock()

	v := make([]helper.ChartValue, len(q.AnswerCount))

	for i := range q.AnswerCount {
		v[i].Label = q.QuestionAnswers[i]
		v[i].Value = float64(q.AnswerCount[i])
	}
	return helper.BarChart(v, "Question_chart", q.Question)
}

func (q *mc) getAdminPage() template.HTML {
	q.AnswerLock.Lock()
	defer q.AnswerLock.Unlock()

	td := mcAdminStruct{
		Question: q.Question,
		Answers: make([]struct {
			Question string
			Count    int
		}, 0, len(q.QuestionAnswers)),
		Submitted:   q.NumberSubmitted,
		Translation: translation.GetDefaultTranslation(),
	}
	for i := range q.AnswerCount {
		td.Answers = append(td.Answers, struct {
			Question string
			Count    int
		}{q.QuestionAnswers[i], q.AnswerCount[i]})
	}

	var buf bytes.Buffer
	err := mcAdminTemplate.Execute(&buf, td)
	if err != nil {
		log.Printf("error executing mcAdmin: %s", err.Error())
	}
	return template.HTML(buf.Bytes())
}
