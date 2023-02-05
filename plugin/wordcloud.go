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
	"fmt"
	"html/template"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/Top-Ranger/responsego/registry"
	"github.com/Top-Ranger/responsego/translation"
)

func init() {
	err := registry.RegisterFeedbackPlugin(func() registry.FeedbackPlugin { return new(wordcloud) }, "Wordcloud")
	if err != nil {
		panic(err)
	}
}

const wordcloudConfig = `
<h1>%s</h1>
<label for="wc_textarea">%s:</label> <input class="fullwidth" type="text" id="wc_textarea" rows="4" autocomplete="off">
<p><button onclick="sendActivate('Wordcloud', document.getElementById('wc_textarea').value)">%s</button></p>
<p><button onclick="saveElement('Wordcloud', document.getElementById('wc_textarea').value, '%s: '+document.getElementById('wc_textarea').value.substring(0,80)+(document.getElementById('wc_textarea').value.length>80?'[...]':''))">%s</button></p>`

var wordcloudHTML = template.Must(template.New("wordcloud").Parse(`
<h1 id="wc_bug">BUG</h1>
<div style="height: 40vh; width: 80vw;">
	<canvas id="ctx"></canvas>
</div>
<input class="fullwidth" type="text" id="InputData" rows="4" autocomplete="off" maxlength="25">
<p><button onclick="sendData('Wordcloud', document.getElementById('InputData').value); document.getElementById('InputData').value = ''">{{.Translation.Submit}}</button></p>
<script>
var ctx = document.getElementById('ctx').getContext('2d');
var wc = {
  type: 'wordCloud',
  data: {
    labels: [""],
    datasets: [
      {
		label: '{{.Title}}',
        data: [0],
      },
    ],
  },
  options: {
	plugins: {
		tooltip: {
		  enabled: false
		}
	  },
	  elements: {
      word: {
        maxRotation: 0,
		minRotation: 0
      },
    },
  },
};
var wcChart = new Chart(ctx, wc);
var wcUpdate = 0;

data_function = function(b) {
	setTimeout(updateWordcloud(b), 1000);
};

function updateWordcloud(b) {
   try {
	var data = JSON.parse(b);
	console.log(data);
	if(wcUpdate != data.Update){
  	  wc.data.labels = data.Labels;
      wc.data.datasets[0].data = data.Data;
	  wcUpdate = data.Update
	  wcChart.update();
	}
   } catch (e) {
    console.log(e);
   }
};

document.getElementById("wc_bug").classList.add("hidden")
</script>
`))

type wordcloudHTMLStruct struct {
	Title       string
	Translation translation.Translation
}

type wordcloud struct {
	adminHTML  chan<- template.HTML
	userHTML   chan<- template.HTML
	adminInput <-chan []byte
	userInput  <-chan []byte
	ctx        context.Context
	cancel     context.CancelFunc
	adminData  chan<- []byte
	userData   chan<- []byte

	update       int
	wordcloudMap map[string]int
	cacheHTML    template.HTML
	l            sync.Mutex
	htmlCache    template.HTML
}

type wordcloudUpdate struct {
	Labels []string
	Data   []int
	Update int
}

func (w *wordcloud) ConfigHTML() (string, template.HTML) {
	tl := translation.GetDefaultTranslation()
	return tl.DisplayWordcloud, template.HTML(fmt.Sprintf(wordcloudConfig, template.HTMLEscapeString(tl.DisplayWordcloud), template.HTMLEscapeString(tl.Title), template.HTMLEscapeString(tl.Activate), template.HTMLEscapeString(tl.DisplayWordcloud), template.HTMLEscapeString(tl.SaveElement)))
}

func (w *wordcloud) AdminHTMLChannel(c chan<- template.HTML) {
	w.adminHTML = c
}

func (w *wordcloud) UserHTMLChannel(c chan<- template.HTML) {
	w.userHTML = c
}

func (w *wordcloud) AdminDataChannel(c chan<- []byte) {
	w.adminData = c
}

func (w *wordcloud) UserDataChannel(c chan<- []byte) {
	w.userData = c
}

func (w *wordcloud) ReceiveUserChannel(c <-chan []byte) {
	w.userInput = c
}

func (w *wordcloud) ReceiveAdminChannel(c <-chan []byte) {
	w.adminInput = c
}

func (w *wordcloud) Activate(by []byte) error {
	w.l.Lock()
	defer w.l.Unlock()
	w.ctx = context.Background()
	w.ctx, w.cancel = context.WithCancel(w.ctx)
	w.wordcloudMap = make(map[string]int)

	go w.wordcloudWorker(w.ctx)
	var buf bytes.Buffer
	err := wordcloudHTML.Execute(&buf, wordcloudHTMLStruct{Title: string(by), Translation: translation.GetDefaultTranslation()})
	if err != nil {
		log.Printf("error executing wordcloud: %s", err.Error())
	}

	b := template.HTML(buf.Bytes())
	w.htmlCache = b
	w.adminHTML <- b
	w.userHTML <- b

	return nil
}

func (w *wordcloud) GetLastHTMLUser() template.HTML {
	return w.htmlCache
}

func (w *wordcloud) GetLastHTMLAdmin() template.HTML {
	return w.htmlCache
}

func (w *wordcloud) Deactivate() {
	w.l.Lock()
	defer w.l.Unlock()
	if w.cancel != nil {
		w.cancel()
	}
}

func (w *wordcloud) wordcloudWorker(ctx context.Context) {
	done := w.ctx.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case b := <-w.adminInput:
			w.l.Lock()
			word := string(b)
			word = strings.ToLower(word)
			word = strings.TrimSpace(word)
			if word != "" {
				i, _ := w.wordcloudMap[word]
				w.wordcloudMap[word] = i + 1
				w.update++
			}
			w.l.Unlock()
		case b := <-w.userInput:
			w.l.Lock()
			word := string(b)
			word = strings.ToLower(word)
			word = strings.TrimSpace(word)
			if word != "" {
				i, _ := w.wordcloudMap[word]
				w.wordcloudMap[word] = i + 1
				w.update++
			}
			w.l.Unlock()
		case <-ticker.C:
			w.l.Lock()
			wu := wordcloudUpdate{
				Labels: make([]string, 0, len(w.wordcloudMap)),
				Data:   make([]int, 0, len(w.wordcloudMap)),
				Update: w.update,
			}
			max := 0
			for k, v := range w.wordcloudMap {
				if v > max {
					max = v
				}
				wu.Labels = append(wu.Labels, k)
				wu.Data = append(wu.Data, v)
			}
			w.l.Unlock()
			if max <= 1 { // Needed due to log(1) = 0
				max = 2
			}
			factor := 36 / math.Log2(float64(max))
			for i := range wu.Data {
				if wu.Data[i] <= 1 { // Needed due to log(1) = 0
					wu.Data[i] = 2
				}
				wu.Data[i] = int(math.Log2(float64(wu.Data[i])) * factor)
				if wu.Data[i] == 0 {
					wu.Data[i] = 1
				}
			}
			j, err := json.Marshal(wu)
			if err != nil {
				log.Printf("wordcloud: Error marshaling update: (%s)", err.Error())
				continue
			}
			w.adminData <- j
			w.userData <- j
		case <-done:
			return
		}
	}
}
