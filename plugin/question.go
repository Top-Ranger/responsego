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
	"strconv"
	"sync"
	"time"

	"github.com/Top-Ranger/responsego/helper"
	"github.com/Top-Ranger/responsego/registry"
	"github.com/Top-Ranger/responsego/translation"
)

func init() {
	err := registry.RegisterFeedbackPlugin(func() registry.FeedbackPlugin { return new(question) }, "Question")
	if err != nil {
		panic(err)
	}
}

const questionConfig = `
<h1>%s</h1>
<p>%s: <input id="Question" type="text"></p>
<table style="border: none;">
    <tr style="border: none; background-color: inherit;">
        <td style="border: none;"><input id="Question_1" type="text"></td>
        <td style="border: none;"><input id="Question_2" type="text"></td>
        <td style="border: none;"><input id="Question_3" type="text"></td>
    </tr>
    <tr style="border: none; background-color: inherit;">
        <td style="border: none;"><input id="Question_4" type="text"></td>
        <td style="border: none;"><input id="Question_5" type="text"></td>
        <td style="border: none;"><input id="Question_6" type="text"></td>
    </tr>
    <tr style="border: none; background-color: inherit;">
        <td style="border: none;"><input id="Question_7" type="text"></td>
        <td style="border: none;"><input id="Question_8" type="text"></td>
        <td style="border: none;"><input id="Question_9" type="text"></td>
    </tr>
</table>
<p><button onclick="sendActivate('Question', JSON.stringify({'1': document.getElementById('Question_1').value, '2': document.getElementById('Question_2').value, '3': document.getElementById('Question_3').value, '4': document.getElementById('Question_4').value, '5': document.getElementById('Question_5').value, '6': document.getElementById('Question_6').value, '7': document.getElementById('Question_7').value, '8': document.getElementById('Question_8').value, '9': document.getElementById('Question_9').value, 'q': document.getElementById('Question').value}))">%s</button></p>
<p><button onclick="saveElement('Question', JSON.stringify({'1': document.getElementById('Question_1').value, '2': document.getElementById('Question_2').value, '3': document.getElementById('Question_3').value, '4': document.getElementById('Question_4').value, '5': document.getElementById('Question_5').value, '6': document.getElementById('Question_6').value, '7': document.getElementById('Question_7').value, '8': document.getElementById('Question_8').value, '9': document.getElementById('Question_9').value, 'q': document.getElementById('Question').value}), '%s: '+document.getElementById('Question').value)">%s</button></p>
`

const questionUser = `
<h1>{{.Question}}</h1>
<table style="border: none;">
{{range $i, $e := .Answers}}
    <tr style="border: none;">
        <td id="Question_cell_{{$i}}" style="border: none;">{{$e}}</td>
		<td style="border: none;"><button id="Question_button_{{$i}}" onclick="sendData('Question','{{$i}}');document.getElementById('Question_cell_{{$i}}').style.backgroundColor='var(--primary-colour-dark)';var e=document.getElementById('Question_button_0'); if(e!=null){e.disabled=true;};var e=document.getElementById('Question_button_1'); if(e!=null){e.disabled=true;};var e=document.getElementById('Question_button_2'); if(e!=null){e.disabled=true;};var e=document.getElementById('Question_button_3'); if(e!=null){e.disabled=true;};var e=document.getElementById('Question_button_4'); if(e!=null){e.disabled=true;};var e=document.getElementById('Question_button_5'); if(e!=null){e.disabled=true;};var e=document.getElementById('Question_button_6'); if(e!=null){e.disabled=true;};var e=document.getElementById('Question_button_7'); if(e!=null){e.disabled=true;};var e=document.getElementById('Question_button_8'); if(e!=null){e.disabled=true;};">{{$.Translation.Submit}}</button></td>
	</tr>
{{end}}
</table>
`

var questionUserTemplate = template.Must(template.New("questionUser").Parse(questionUser))

type questionUserStruct struct {
	Question    string
	Answers     []string
	Translation translation.Translation
}

const questionAdmin = `
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
<p><button onclick="sendData('Question', 'close')">{{.Translation.Finish}}</button></p>
`

var questionAdminTemplate = template.Must(template.New("questionAdmin").Parse(questionAdmin))

type questionAdminStruct struct {
	Question string
	Answers  []struct {
		Question string
		Count    int
	}
	Submitted   int
	Translation translation.Translation
}

type question struct {
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

func (q *question) ConfigHTML() (string, template.HTML) {
	tl := translation.GetDefaultTranslation()
	return tl.DisplayQuestion, template.HTML(fmt.Sprintf(questionConfig, template.HTMLEscapeString(tl.DisplayQuestion), template.HTMLEscapeString(tl.DisplayQuestion), template.HTMLEscapeString(tl.Activate), template.HTMLEscapeString(tl.DisplayQuestion), template.HTMLEscapeString(tl.SaveElement)))
}

func (q *question) AdminHTMLChannel(c chan<- template.HTML) {
	q.adminHTML = c
}

func (q *question) UserHTMLChannel(c chan<- template.HTML) {
	q.userHTML = c
}

func (q *question) ReceiveUserChannel(c <-chan []byte) {
	q.userInput = c
}

func (q *question) ReceiveAdminChannel(c <-chan []byte) {
	q.adminInput = c
}

func (q *question) Activate(b []byte) error {
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
		td := questionUserStruct{
			Question:    q.Question,
			Answers:     q.QuestionAnswers,
			Translation: translation.GetDefaultTranslation(),
		}
		var buf bytes.Buffer
		err := questionUserTemplate.Execute(&buf, td)
		if err != nil {
			log.Printf("error executing questionUser: %s", err.Error())
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
				i, err := strconv.Atoi(string(b))
				if err == nil {
					q.AnswerLock.Lock()
					if i < len(q.AnswerCount) {
						q.AnswerCount[i]++
						q.NumberSubmitted++
						q.NumberChanged = true
					}
					q.AnswerLock.Unlock()
				}

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

func (q *question) GetLastHTMLUser() template.HTML {

	if q.Finished {
		return q.questionGetChart()
	}

	q.AnswerLock.Lock()
	defer q.AnswerLock.Unlock()

	td := questionUserStruct{
		Question:    q.Question,
		Answers:     q.QuestionAnswers,
		Translation: translation.GetDefaultTranslation(),
	}
	var buf bytes.Buffer
	err := questionUserTemplate.Execute(&buf, td)
	if err != nil {
		log.Printf("error executing questionUser: %s", err.Error())
	}
	return template.HTML(buf.Bytes())
}

func (q *question) GetLastHTMLAdmin() template.HTML {
	if q.Finished {
		return q.questionGetChart()
	}
	return q.getAdminPage()
}

func (q *question) Deactivate() {
	if q.cancel != nil {
		q.cancel()
	}
}

func (q *question) questionGetChart() template.HTML {
	q.AnswerLock.Lock()
	defer q.AnswerLock.Unlock()

	v := make([]helper.ChartValue, len(q.AnswerCount))

	for i := range q.AnswerCount {
		v[i].Label = q.QuestionAnswers[i]
		v[i].Value = float64(q.AnswerCount[i])
	}
	return helper.PieChart(v, "Question_chart", q.Question)
}

func (q *question) getAdminPage() template.HTML {
	q.AnswerLock.Lock()
	defer q.AnswerLock.Unlock()

	td := questionAdminStruct{
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
	err := questionAdminTemplate.Execute(&buf, td)
	if err != nil {
		log.Printf("error executing questionAdmin: %s", err.Error())
	}
	return template.HTML(buf.Bytes())
}

type questionResultStruct struct {
	Question        string
	QuestionAnswers []string
	AnswerCount     []int
}

func (q *question) GetAdminDownload() []byte {
	q.AnswerLock.Lock()
	defer q.AnswerLock.Unlock()

	b, err := json.Marshal(questionResultStruct{Question: q.Question, QuestionAnswers: q.QuestionAnswers, AnswerCount: q.AnswerCount})
	if err != nil {
		return []byte(err.Error())
	}
	return b
}
