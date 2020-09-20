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

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Top-Ranger/responsego/registry"
	"github.com/Top-Ranger/responsego/translation"
	"github.com/gorilla/websocket"
)

func init() {
	b, err := ioutil.ReadFile("template/user.html")
	if err != nil {
		panic(err)
	}

	userTemplate, err = template.New("user").Parse(string(b))
	if err != nil {
		panic(err)
	}

	b, err = ioutil.ReadFile("template/admin.html")
	if err != nil {
		panic(err)
	}

	adminTemplate, err = template.New("admin").Parse(string(b))
	if err != nil {
		panic(err)
	}
}

const (
	actionActivate    = "activate"
	actionUserUpdate  = "user"
	actionAdminUpdate = "admin"
	actionResetIcons  = "resetIcon"
	actionIcon        = "icon"
	actionHTML        = "html"
)

const (
	iconSlower   = "slower"
	iconBreak    = "break"
	iconFaster   = "faster"
	iconQuestion = "question"
	iconGood     = "good"
)

const globalAction = "_global"

const (
	bufferSize = 10
)

var userTemplate *template.Template
var adminTemplate *template.Template

var pluginConfigCache = make([]struct {
	Name string
	HTML template.HTML
}, 0)
var pluginConfigCacheOnce = sync.Once{}

type message struct {
	From   string
	Action string
	Data   string
}

type response struct {
	l        sync.Mutex
	ctx      context.Context
	Stop     context.CancelFunc
	Password string
	Path     string

	admins            map[int]chan<- []byte
	users             map[int]chan<- []byte
	currentID         int
	currentPluginName string
	currentPlugin     registry.FeedbackPlugin
	readUser          chan []byte
	readAdmins        chan []byte
	adminHTML         chan template.HTML
	userHTML          chan template.HTML
	adminInput        chan []byte
	userInput         chan []byte

	nSlower   int
	nBreak    int
	nFaster   int
	nQuestion int
	nGood     int
}

type userTemplateStruct struct {
	Translation translation.Translation
	ServerPath  string
}

type adminTemplateStruct struct {
	URL      string
	QR       template.URL
	Password string
	Elements []struct {
		Name string
		HTML template.HTML
	}
	Translation translation.Translation
	ServerPath  string
}

func websocketReader(target chan<- []byte, ws *websocket.Conn, r *response) {
	for {
		_, b, err := ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("socket error (%s): %s", r.Path, err)
			}
			return
		}
		t := time.NewTimer(time.Second)
		select {
		case target <- b:
		case <-t.C:
			log.Printf("socket read (%s): can not write to channel", r.Path)
		}
	}
}

func websocketWriter(from <-chan []byte, ws *websocket.Conn, r *response, id int) {
	defer ws.Close()
	for b := range from {
		err := ws.WriteMessage(websocket.TextMessage, b)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("socket error (%s): %s", r.Path, err)
			}
			r.removeWS(id)
			return
		}
	}
}

func (r *response) removeWS(id int) {
	r.l.Lock()
	defer r.l.Unlock()
	delete(r.admins, id)
	delete(r.users, id)
}

// NewResponse creates a new response object (including startup of all required goroutines).
func NewResponse(path, password string) *response {
	var ctx context.Context
	var cancel context.CancelFunc
	ctx = context.Background()
	ctx, cancel = context.WithCancel(ctx)
	r := &response{
		l:        sync.Mutex{},
		ctx:      ctx,
		Stop:     cancel,
		Password: password,
		Path:     path,

		admins:            make(map[int]chan<- []byte),
		users:             make(map[int]chan<- []byte),
		currentID:         0,
		currentPluginName: "",
		readUser:          make(chan []byte, bufferSize),
		readAdmins:        make(chan []byte, bufferSize),
	}

	go r.responseMain()

	return r
}

func (r *response) AddUser(ws *websocket.Conn) {
	r.l.Lock()
	defer r.l.Unlock()

	w := make(chan []byte, bufferSize)
	r.users[r.currentID] = w
	go websocketReader(r.readUser, ws, r)
	go websocketWriter(w, ws, r, r.currentID)
	r.currentID++
	if r.currentPlugin != nil {
		m := message{From: r.currentPluginName, Action: actionHTML, Data: string(r.currentPlugin.GetLastHTMLUser())}
		b, err := json.Marshal(&m)
		if err != nil {
			log.Printf("user HTML (%s) plugin %s: %s", r.Path, r.currentPluginName, err.Error())
		} else {
			select {
			case w <- []byte(b):
			default:
			}
		}
	}
}

func (r *response) AddAdmin(ws *websocket.Conn) {
	r.l.Lock()
	defer r.l.Unlock()

	w := make(chan []byte, bufferSize)
	r.admins[r.currentID] = w
	go websocketReader(r.readAdmins, ws, r)
	go websocketWriter(w, ws, r, r.currentID)
	r.currentID++
	if r.currentPlugin != nil {
		m := message{From: r.currentPluginName, Action: actionHTML, Data: string(r.currentPlugin.GetLastHTMLAdmin())}
		b, err := json.Marshal(&m)
		if err != nil {
			log.Printf("admin HTML (%s) plugin %s: %s", r.Path, r.currentPluginName, err.Error())
		} else {
			select {
			case w <- []byte(b):
			default:
			}
		}
	}
	r.sendIconUpdate(iconSlower, r.nSlower)
	r.sendIconUpdate(iconBreak, r.nBreak)
	r.sendIconUpdate(iconFaster, r.nFaster)
	r.sendIconUpdate(iconQuestion, r.nQuestion)
	r.sendIconUpdate(iconGood, r.nGood)

}

func (r *response) HasUser() bool {
	r.l.Lock()
	defer r.l.Unlock()
	return len(r.admins) != 0 || len(r.users) != 0
}

func (r *response) WriteUserPage(rw http.ResponseWriter) {
	td := userTemplateStruct{
		Translation: translation.GetDefaultTranslation(),
		ServerPath:  config.ServerPath,
	}
	err := userTemplate.Execute(rw, td)
	if err != nil {
		log.Printf("can not write user page (%s): %s", r.Path, err.Error())
	}
}

func (r *response) WriteAdminPage(rw http.ResponseWriter) {
	fetchConfigCache()
	url := fmt.Sprintf("%s/%s", config.ServerName, r.Path)
	qr, err := GenerateQRSrc(url)
	if err != nil {
		tl := translation.GetDefaultTranslation()
		rw.WriteHeader(http.StatusInternalServerError)
		t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), tl, config.ServerPath}
		textTemplate.Execute(rw, t)
		return
	}
	td := adminTemplateStruct{
		URL:         url,
		QR:          template.URL(qr),
		Password:    r.Password,
		Elements:    pluginConfigCache,
		Translation: translation.GetDefaultTranslation(),
		ServerPath:  config.ServerPath,
	}
	err = adminTemplate.Execute(rw, td)
	if err != nil {
		log.Printf("can not write admin page (%s): %s", r.Path, err.Error())
	}
}

func (r *response) responseMain() {
	log.Printf("starting %s", r.Path)

	done := r.ctx.Done()
	for {
		select {
		case b := <-r.readAdmins:
			// Function to use defer
			func() {
				r.l.Lock()
				defer r.l.Unlock()
				var m message
				err := json.Unmarshal(b, &m)
				if err != nil {
					log.Printf("read admin (%s): can not parse '%s': %s", r.Path, b, err.Error())
					return
				}
				switch m.Action {
				case actionResetIcons:
					r.nSlower = 0
					r.nBreak = 0
					r.nFaster = 0
					r.nQuestion = 0
					r.nGood = 0
					r.sendIconUpdate(iconSlower, r.nSlower)
					r.sendIconUpdate(iconBreak, r.nBreak)
					r.sendIconUpdate(iconFaster, r.nFaster)
					r.sendIconUpdate(iconQuestion, r.nQuestion)
					r.sendIconUpdate(iconGood, r.nGood)
				case actionActivate:
					if r.currentPlugin != nil {
						// Reset
						r.currentPlugin.Deactivate()
						r.currentPlugin = nil
						r.currentPluginName = ""
						r.adminHTML = nil
						r.userHTML = nil
						r.adminInput = nil
						r.userInput = nil
					}
					fp, ok := registry.GetFeedbackPlugins(m.From)
					if !ok {
						log.Printf("error unknown plugin %s (%s)", m.From, r.Path)
						return
					}
					r.adminHTML = make(chan template.HTML, bufferSize)
					r.userHTML = make(chan template.HTML, bufferSize)
					r.adminInput = make(chan []byte, bufferSize)
					r.userInput = make(chan []byte, bufferSize)
					p := fp()
					p.AdminHTMLChannel(r.adminHTML)
					p.UserHTMLChannel(r.userHTML)
					p.ReceiveAdminChannel(r.adminInput)
					p.ReceiveUserChannel(r.userInput)
					err := p.Activate([]byte(m.Data))
					if err != nil {
						log.Printf("error activating plugin %s (%s): %s", m.From, r.Path, err.Error())
						r.adminHTML = nil
						r.userHTML = nil
						r.adminInput = nil
						r.userInput = nil
						return
					}
					r.currentPlugin = p
					r.currentPluginName = m.From
				case actionAdminUpdate:
					if m.From == r.currentPluginName {
						select {
						case r.adminInput <- []byte(m.Data):
						default:
						}
					}
				default:
					// Invalid input - ignore
				}
			}()

		case b := <-r.readUser:
			// Function to use defer
			func() {
				r.l.Lock()
				defer r.l.Unlock()

				var m message
				err := json.Unmarshal(b, &m)
				if err != nil {
					log.Printf("read admin (%s): can not parse '%s': %s", r.Path, b, err.Error())
					return
				}
				switch m.Action {
				case actionIcon:
					switch m.Data {
					case iconSlower:
						r.nSlower++
						r.sendIconUpdate(iconSlower, r.nSlower)
					case iconBreak:
						r.nBreak++
						r.sendIconUpdate(iconBreak, r.nBreak)
					case iconFaster:
						r.nFaster++
						r.sendIconUpdate(iconFaster, r.nFaster)
					case iconQuestion:
						r.nQuestion++
						r.sendIconUpdate(iconQuestion, r.nQuestion)
					case iconGood:
						r.nGood++
						r.sendIconUpdate(iconGood, r.nGood)
					}
				case actionUserUpdate:
					if m.From == r.currentPluginName {
						select {
						case r.userInput <- []byte(m.Data):
						default:
						}
					}
				default:
					// Invalid input - ignore
				}

			}()
		case t := <-r.adminHTML:
			func() {
				r.l.Lock()
				defer r.l.Unlock()

				m := message{From: r.currentPluginName, Action: actionHTML, Data: string(t)}
				b, err := json.Marshal(&m)
				if err != nil {
					log.Printf("admin HTML (%s) plugin %s: %s", r.Path, r.currentPluginName, err.Error())
					return
				}

				for k := range r.admins {
					select {
					case r.admins[k] <- b:
					default:
					}
				}
			}()
		case t := <-r.userHTML:
			func() {
				r.l.Lock()
				defer r.l.Unlock()

				m := message{From: r.currentPluginName, Action: actionHTML, Data: string(t)}
				b, err := json.Marshal(&m)
				if err != nil {
					log.Printf("user HTML (%s) plugin %s: %s", r.Path, r.currentPluginName, err.Error())
					return
				}

				for k := range r.users {
					select {
					case r.users[k] <- b:
					default:
					}
				}
			}()
		case <-done:
			// Function to use defer
			func() {
				r.l.Lock()
				defer r.l.Unlock()
				if r.currentPlugin != nil {
					// Reset plugin
					r.currentPlugin.Deactivate()
					r.currentPlugin = nil
					r.currentPluginName = ""
					r.adminHTML = nil
					r.userHTML = nil
					r.adminInput = nil
					r.userInput = nil
				}
			}()
			log.Printf("stopping %s", r.Path)
			return
		}
	}
}

func (r *response) sendIconUpdate(icon string, data int) {
	m := message{From: globalAction, Action: icon, Data: strconv.Itoa(data)}
	b, err := json.Marshal(m)
	if err != nil {
		log.Printf("sending icons (%s): %s", r.Path, err.Error())
	}
	for k := range r.admins {
		select {
		case r.admins[k] <- b:
		default:
		}
	}
}

func fetchConfigCache() {
	pluginConfigCacheOnce.Do(func() {
		plugins := registry.GetNamesOfFeedbackPlugins()
		for i := range plugins {
			fp, ok := registry.GetFeedbackPlugins(plugins[i])
			if !ok {
				log.Printf("fetch config cache: Plugin %s should exist, but doesn't", plugins[i])
			}
			p := fp()
			n, h := p.ConfigHTML()
			if strings.HasPrefix(n, "_") {
				log.Printf("fetchConfigCache: Element name %s starts with '_' which is not allowed. Skipping it")
				continue
			}
			pluginConfigCache = append(pluginConfigCache, struct {
				Name string
				HTML template.HTML
			}{Name: n, HTML: h})
		}
	})
}
