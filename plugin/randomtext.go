// SPDX-License-Identifier: Apache-2.0
// Copyright 2023 Marcus Soll
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
	"errors"
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
	err := registry.RegisterFeedbackPlugin(func() registry.FeedbackPlugin { return new(randomgroup) }, "RandomGroup")
	if err != nil {
		panic(err)
	}
}

const randomgroupConfig = `
<h1>{{.Translation.DisplayRandomGroup}}</h1>
<label for="RandomGroupTitle">{{.Translation.Title}}:</label> <input id="RandomGroupTitle" type="text"><br>
<textarea class="fullwidth" id="RandomGroupTextarea" rows="10">
1
<<<--->>>
2
<<<--->>>
3
</textarea><br>
<label for="RandomGroupSeperator">{{.Translation.Seperator}}:</label> <input id="RandomGroupSeperator" type="text" value="<<<--->>>"><br>
<p><button onclick="sendActivate('RandomGroup', randomgroupGetData())">{{.Translation.Activate}}</button></p>
<p><button onclick="saveElement('RandomGroup', randomgroupGetData()), '{{.Translation.DisplayRandomGroup}}: '+document.getElementById('RandomGroupTitle').value.substring(0,80)+(document.getElementById('RandomGroupTitle').value.length>80?'[...]':''))">{{.Translation.SaveElement}}</button></p>

<script>
function randomgroupGetData() {
	let data = {"Title": document.getElementById('RandomGroupTitle').value, "Text": document.getElementById('RandomGroupTextarea').value, "Seperator": document.getElementById('RandomGroupSeperator').value};
	return JSON.stringify(data);
}
</script>
`

var randomgroupConfigTemplate = template.Must(template.New("randomgroupConfig").Parse(randomgroupConfig))

type randomgroupConfigStruct struct {
	Translation translation.Translation
}

const randomgroupUser = `
<h1>{{.Headline}}</h1>
<div id="RandomGroup"></div>

<script>
var randomGroup = [
{{range $i, $e := .Texts}}
'{{$e}}',
{{end}}
];

var randomGroupSelection = Math.floor(Math.random() * randomGroup.length);
document.getElementById("RandomGroup").innerHTML = randomGroup[randomGroupSelection];
sendDataSilent("RandomGroup", ""+randomGroupSelection);
</script>`

var randomgroupUserTemplate = template.Must(template.New("randomgroupConfig").Parse(randomgroupUser))

type randomgroupUserStruct struct {
	Headline    string
	Texts       []template.HTML
	Translation translation.Translation
}

const randomgroupAdmin = `
<h1>{{.Headline}}</h1>

{{.Chart}}

{{range $i, $e := .Answers}}
<h2>{{addOne $i}}</h2>
<div style="background-color: white;margin: 10px;">
{{$e}}
</div>
{{end}}


<script>
function updateChart(b) {
	try {
	 var data = JSON.parse(b);
	 console.log(data)
	 chartData.data.datasets[0].data = data.Data;
	 chart.update();
	} catch (e) {
	 console.log(e);
	}
 };

data_function = function(b) {
	setTimeout(updateChart(b), 1000);
};
</script>
`

var randomgroupAdminTemplate = template.Must(template.New("randomtextConfig").Funcs(template.FuncMap{"addOne": func(i int) int { return i + 1 }}).Parse(randomgroupAdmin))

type randomgroupAdminStruct struct {
	Headline    string
	Chart       template.HTML
	Answers     []template.HTML
	Translation translation.Translation
}

type randomgroupAdminChartUpdate struct {
	Data []int
}

type randomgroup struct {
	adminHTML  chan<- template.HTML
	userHTML   chan<- template.HTML
	adminInput <-chan []byte
	userInput  <-chan []byte
	adminData  chan<- []byte
	userData   chan<- []byte
	ctx        context.Context
	cancel     context.CancelFunc

	numAnswers     int
	userSelectMap  map[int]int
	userHTMLcache  template.HTML
	adminHTMLcache template.HTML

	l sync.Mutex
}

type randomgroupGetConfig struct {
	Title     string
	Text      string
	Seperator string
}

func (rg *randomgroup) ConfigHTML() (string, template.HTML) {
	tl := translation.GetDefaultTranslation()
	td := randomgroupConfigStruct{
		Translation: tl,
	}
	var buf bytes.Buffer
	err := randomgroupConfigTemplate.Execute(&buf, td)
	if err != nil {
		log.Printf("error executing randomgroup config: %s", err.Error())
	}

	return tl.DisplayRandomGroup, template.HTML(buf.Bytes())
}

func (rg *randomgroup) UserHTMLChannel(c chan<- template.HTML) {
	rg.userHTML = c
}

func (rg *randomgroup) AdminHTMLChannel(c chan<- template.HTML) {
	rg.adminHTML = c
}

func (rg *randomgroup) ReceiveUserChannel(c <-chan []byte) {
	rg.userInput = c
}

func (rg *randomgroup) ReceiveAdminChannel(c <-chan []byte) {
	rg.adminInput = c
}

func (rg *randomgroup) AdminDataChannel(c chan<- []byte) {
	rg.adminData = c
}

func (rg *randomgroup) UserDataChannel(c chan<- []byte) {
	rg.userData = c
}

func (rg *randomgroup) Activate(b []byte) error {
	tl := translation.GetDefaultTranslation()

	// Parse input
	var config randomgroupGetConfig
	err := json.Unmarshal(b, &config)
	if err != nil {
		return err
	}
	directSplit := strings.Split(config.Text, config.Seperator)
	split := make([]template.HTML, 0, len(directSplit))
	for i := range directSplit {
		directSplit[i] = strings.TrimSpace(directSplit[i])
		if directSplit[i] != "" {
			f := helper.Format([]byte(directSplit[i]))
			if f != "" {
				split = append(split, template.HTML(strings.ReplaceAll(string(f), "'", "\"")))
			}
		}
	}

	if len(split) == 0 {
		return errors.New("randomgroup: at least one group must have valid text")
	}

	rg.numAnswers = len(split)

	// Create map
	rg.userSelectMap = make(map[int]int, len(split))

	// User HTML
	{
		td := randomgroupUserStruct{
			Headline:    config.Title,
			Texts:       split,
			Translation: tl,
		}
		var buf bytes.Buffer
		err := randomgroupUserTemplate.Execute(&buf, td)
		if err != nil {
			log.Printf("error executing randomgroup user: %s", err.Error())
			return err
		}
		rg.userHTMLcache = template.HTML(buf.Bytes())
	}
	// Admin HTML
	{
		cv := make([]helper.ChartValue, len(split))
		for i := range split {
			cv[i].Label = strconv.Itoa(i + 1)
		}
		td := randomgroupAdminStruct{
			Headline:    config.Title,
			Chart:       helper.PieChart(cv, "RandomGroup", tl.DisplayRandomGroup),
			Answers:     split,
			Translation: tl,
		}
		var buf bytes.Buffer
		err := randomgroupAdminTemplate.Execute(&buf, td)
		if err != nil {
			log.Printf("error executing randomgroup admin: %s", err.Error())
			return err
		}
		rg.adminHTMLcache = template.HTML(buf.Bytes())
	}

	// Start plugin
	go func() { rg.userHTML <- rg.userHTMLcache }()
	go func() { rg.adminHTML <- rg.adminHTMLcache }()
	rg.ctx = context.Background()
	rg.ctx, rg.cancel = context.WithCancel(rg.ctx)
	go rg.worker(rg.ctx)

	// Return
	return nil
}

func (rg *randomgroup) GetLastHTMLUser() template.HTML {
	return rg.userHTMLcache
}

func (rg *randomgroup) GetLastHTMLAdmin() template.HTML {
	return rg.adminHTMLcache
}

func (rg *randomgroup) Deactivate() {
	if rg.cancel != nil {
		rg.cancel()
	}
}

func (rg *randomgroup) worker(ctx context.Context) {
	done := rg.ctx.Done()
	t := time.NewTicker(5 * time.Second)
	for {
		select {
		case <-rg.adminInput:
			// Do nothing since no action is here
		case b := <-rg.userInput:
			rg.l.Lock()
			i, err := strconv.Atoi(string(b))
			if err == nil && i < rg.numAnswers {
				count, _ := rg.userSelectMap[i]
				rg.userSelectMap[i] = count + 1
			}
			rg.l.Unlock()
		case <-t.C:
			data := randomgroupAdminChartUpdate{
				Data: make([]int, rg.numAnswers),
			}
			rg.l.Lock()
			for i := 0; i < rg.numAnswers; i++ {
				data.Data[i] = rg.userSelectMap[i]
			}
			rg.l.Unlock()

			b, err := json.Marshal(data)
			if err != nil {
				log.Printf("randogroup: Error marshaling update: (%s)", err.Error())
				continue
			}
			rg.adminData <- b
		case <-done:
			t.Stop()
			return
		}
	}
}
