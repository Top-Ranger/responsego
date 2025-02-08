// SPDX-License-Identifier: Apache-2.0
// Copyright 2020,2021,2025 Marcus Soll
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
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base32"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Top-Ranger/responsego/helper"
	"github.com/Top-Ranger/responsego/translation"
	"github.com/gorilla/websocket"
)

var serverMutex sync.Mutex
var serverStarted bool
var server http.Server
var rootPath string
var stopGC context.CancelFunc

var responseCache = make(map[string]*response)
var responseCacheLock = sync.Mutex{}
var upgrader = websocket.Upgrader{}

var authenticateTemplate *template.Template

var dsgvo []byte
var impressum []byte

//go:embed static font js css
var cachedFiles embed.FS
var cssTemplates *template.Template

var robottxt = []byte(`User-agent: *
Disallow: /`)

func init() {
	var err error

	upgrader.HandshakeTimeout = 5 * time.Second

	authenticateTemplate, err = template.ParseFS(templateFiles, "template/authenticate.html")
	if err != nil {
		panic(err)
	}

	cssTemplates, err = template.ParseFS(cachedFiles, "css/*")
	if err != nil {
		panic(err)
	}
}

type authenticateTemplateStruct struct {
	Key         string
	Translation translation.Translation
	ServerPath  string
}

func initialiseServer() error {
	if serverStarted {
		return nil
	}
	server = http.Server{Addr: config.Address}

	// Do setup
	rootPath = strings.Join([]string{config.ServerPath, "/"}, "")

	// DSGVO
	b, err := os.ReadFile(config.PathDSGVO)
	if err != nil {
		return err
	}
	text := textTemplateStruct{helper.Format(b), translation.GetDefaultTranslation(), config.ServerPath}
	output := bytes.NewBuffer(make([]byte, 0, len(text.Text)*2))
	textTemplate.Execute(output, text)
	dsgvo = output.Bytes()

	http.HandleFunc(strings.Join([]string{config.ServerPath, "/dsgvo.html"}, ""), func(rw http.ResponseWriter, r *http.Request) {
		rw.Write(dsgvo)
	})

	// Impressum
	b, err = os.ReadFile(config.PathImpressum)
	if err != nil {
		return err
	}
	text = textTemplateStruct{helper.Format(b), translation.GetDefaultTranslation(), config.ServerPath}
	output = bytes.NewBuffer(make([]byte, 0, len(text.Text)*2))
	textTemplate.Execute(output, text)
	impressum = output.Bytes()
	http.HandleFunc(strings.Join([]string{config.ServerPath, "/impressum.html"}, ""), func(rw http.ResponseWriter, r *http.Request) {
		rw.Write(impressum)
	})

	etag := fmt.Sprint("\"", strconv.FormatInt(time.Now().Unix(), 10), "\"")
	etagCompare := strings.TrimSuffix(etag, "\"")
	etagCompareApache := strings.Join([]string{etagCompare, "-"}, "")       // Dirty hack for apache2, who appends -gzip inside the quotes if the file is compressed, thus preventing If-None-Match matching the ETag
	etagCompareCaddy := strings.Join([]string{"W/", etagCompare, "\""}, "") // Dirty hack for caddy, who appends W/ before the quotes if the file is compressed, thus preventing If-None-Match matching the ETag

	staticHandle := func(rw http.ResponseWriter, r *http.Request) {
		// Check for ETag
		v, ok := r.Header["If-None-Match"]
		if ok {
			for i := range v {
				if v[i] == etag || v[i] == etagCompareCaddy || strings.HasPrefix(v[i], etagCompareApache) {
					rw.WriteHeader(http.StatusNotModified)
					return
				}
			}
		}

		// Send file if existing in cache
		path := r.URL.Path
		path = strings.TrimPrefix(path, config.ServerPath)
		path = strings.TrimPrefix(path, "/")

		if strings.HasPrefix(path, "css/") {
			// special case
			path = strings.TrimPrefix(path, "css/")
			rw.Header().Set("ETag", etag)
			rw.Header().Set("Cache-Control", "public, max-age=43200")
			rw.Header().Set("Content-Type", "text/css")
			err := cssTemplates.ExecuteTemplate(rw, path, struct{ ServerPath string }{config.ServerPath})
			if err != nil {
				rw.WriteHeader(http.StatusNotFound)
				log.Println("server:", err)
			}
			return
		}

		data, err := cachedFiles.Open(path)
		if err != nil {
			rw.WriteHeader(http.StatusNotFound)
		} else {
			rw.Header().Set("ETag", etag)
			rw.Header().Set("Cache-Control", "public, max-age=43200")
			switch {
			case strings.HasSuffix(path, ".svg"):
				rw.Header().Set("Content-Type", "image/svg+xml")
			case strings.HasSuffix(path, ".ttf"):
				rw.Header().Set("Content-Type", "application/x-font-truetype")
			case strings.HasSuffix(path, ".js"):
				rw.Header().Set("Content-Type", "application/javascript")
			default:
				rw.Header().Set("Content-Type", "text/plain")
			}
			io.Copy(rw, data)
		}
	}

	http.HandleFunc(strings.Join([]string{config.ServerPath, "/css/"}, ""), staticHandle)
	http.HandleFunc(strings.Join([]string{config.ServerPath, "/static/"}, ""), staticHandle)
	http.HandleFunc(strings.Join([]string{config.ServerPath, "/font/"}, ""), staticHandle)
	http.HandleFunc(strings.Join([]string{config.ServerPath, "/js/"}, ""), staticHandle)

	http.HandleFunc(strings.Join([]string{config.ServerPath, "/favicon.ico"}, ""), func(rw http.ResponseWriter, r *http.Request) {
		// Check for ETag
		v, ok := r.Header["If-None-Match"]
		if ok {
			for i := range v {
				if v[i] == etag || v[i] == etagCompareCaddy || strings.HasPrefix(v[i], etagCompareApache) {
					rw.WriteHeader(http.StatusNotModified)
					return
				}
			}
		}

		f, err := cachedFiles.ReadFile("static/favicon.ico")

		if err != nil {
			rw.WriteHeader(http.StatusNotFound)
			return
		}

		rw.Write(f)
	})

	// robots.txt
	http.HandleFunc(strings.Join([]string{config.ServerPath, "/robots.txt"}, ""), func(rw http.ResponseWriter, r *http.Request) {
		rw.Write(robottxt)
	})

	http.HandleFunc("/", rootHandle)
	return nil
}

func rootHandle(rw http.ResponseWriter, r *http.Request) {
	if r.URL.Path == rootPath || r.URL.Path == config.ServerPath || r.URL.Path == "/" {
		tl := translation.GetDefaultTranslation()
		t := textTemplateStruct{"<h1>ResponseGo!</h1>", tl, config.ServerPath}
		textTemplate.Execute(rw, t)
		return
	}

	key := r.URL.Path
	key = strings.TrimLeft(key, "/")

	responseCacheLock.Lock()
	defer responseCacheLock.Unlock()

	response, ok := responseCache[key]
	if !ok {
		if config.NeedAuthenticationForNew {
			switch r.Method {
			case http.MethodGet:
				// Send authentification request
				td := authenticateTemplateStruct{Key: key, Translation: translation.GetDefaultTranslation(), ServerPath: config.ServerPath}
				authenticateTemplate.Execute(rw, td)
				return
			case http.MethodPost:
				// Verify authentification request
				err := r.ParseForm()
				if err != nil {
					rw.WriteHeader(http.StatusInternalServerError)
					t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), translation.GetDefaultTranslation(), config.ServerPath}
					textTemplate.Execute(rw, t)
					return
				}

				username, password := r.Form.Get("name"), r.Form.Get("password")

				if len(username) == 0 || len(password) == 0 {
					rw.WriteHeader(http.StatusForbidden)
					t := textTemplateStruct{"403 Forbidden", translation.GetDefaultTranslation(), config.ServerPath}
					textTemplate.Execute(rw, t)
					return
				}
				correct, err := authenticater.Authenticate(username, password)
				if err != nil {
					rw.WriteHeader(http.StatusInternalServerError)
					t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), translation.GetDefaultTranslation(), config.ServerPath}
					textTemplate.Execute(rw, t)
					return
				}
				if !correct {
					if config.LogLogin {
						log.Printf("Failed authentication from %s", GetRealIP(r))
					}
					rw.WriteHeader(http.StatusForbidden)
					t := textTemplateStruct{"403 Forbidden", translation.GetDefaultTranslation(), config.ServerPath}
					textTemplate.Execute(rw, t)
					return
				}
				// All ok - continue creation
				if config.LogLogin {
					log.Printf("Creating new response for '%s': %s", username, key)
				}

			default:
				rw.WriteHeader(http.StatusBadRequest)
				t := textTemplateStruct{"400 Bad Request", translation.GetDefaultTranslation(), config.ServerPath}
				textTemplate.Execute(rw, t)
				return
			}
		}
		b := make([]byte, 35)
		_, err := rand.Read(b)
		if err != nil {
			tl := translation.GetDefaultTranslation()
			rw.WriteHeader(http.StatusInternalServerError)
			t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), tl, config.ServerPath}
			textTemplate.Execute(rw, t)
			return
		}
		password := base32.StdEncoding.EncodeToString(b)
		response = NewResponse(key, password)
		responseCache[key] = response

		http.Redirect(rw, r, fmt.Sprintf("/%s?admin=%s", key, password), http.StatusFound)
		return
	}

	pw, ws := r.URL.Query().Get("admin"), r.URL.Query().Get("ws")
	if pw != "" {
		// Admin connection
		if subtle.ConstantTimeCompare([]byte(pw), []byte(response.Password)) == 0 {
			if config.LogLogin {
				log.Printf("Failed authentication from %s (%s)", GetRealIP(r), key)
			}
			rw.WriteHeader(http.StatusForbidden)
			t := textTemplateStruct{"403 Forbidden", translation.GetDefaultTranslation(), config.ServerPath}
			textTemplate.Execute(rw, t)
			return
		}

		if ws == "" {
			// no websocket
			response.WriteAdminPage(rw)
			return
		}

		// websocket - don't block ih waiting takes long
		responseCacheLock.Unlock()
		conn, err := upgrader.Upgrade(rw, r, nil)
		responseCacheLock.Lock()
		if err != nil {
			log.Println("upgrade:", err)
			return
		}
		response, ok := responseCache[key]
		if !ok {
			// something went wrong (e.g. gc)
			conn.Close()
			return
		}
		response.AddAdmin(conn)
		return
	}

	// User connection
	if ws == "" {
		// no websocket
		response.WriteUserPage(rw)
		return
	}

	// websocket - don't block ih waiting takes long
	responseCacheLock.Unlock()
	conn, err := upgrader.Upgrade(rw, r, nil)
	responseCacheLock.Lock()
	if err != nil {
		log.Println("upgrade:", err)
		return
	}
	response, ok = responseCache[key]
	if !ok {
		// something went wrong (e.g. gc)
		conn.Close()
		return
	}
	response.AddUser(conn)
}

// RunServer starts the actual server.
// It does nothing if a server is already started.
// It will return directly after the server is started.
func RunServer() {
	serverMutex.Lock()
	defer serverMutex.Unlock()
	if serverStarted {
		return
	}

	err := initialiseServer()
	if err != nil {
		log.Panicln("server:", err)
	}
	log.Println("server: Server starting at", config.Address)
	serverStarted = true

	ctx := context.Background()
	ctx, stopGC = context.WithCancel(ctx)

	go gc(ctx)

	go func() {
		err = server.ListenAndServe()
		if err != http.ErrServerClosed {
			log.Println("server:", err)
		}
	}()
}

// StopServer shuts the server down.
// It will do nothing if the server is not started.
// It will return after the shutdown is completed.
func StopServer() {
	serverMutex.Lock()
	defer serverMutex.Unlock()
	if !serverStarted {
		return
	}
	stopGC()
	err := server.Shutdown(context.Background())
	if err == nil {
		log.Println("server: stopped")
	} else {
		log.Println("server:", err)
	}
}

func gc(ctx context.Context) {
	done := ctx.Done()
	ticker := time.NewTicker(time.Duration(config.GCMinutes) * time.Minute)
	defer ticker.Stop()
	log.Println("server: starting gc")
	for {
		select {
		case <-ticker.C:
			responseCacheLock.Lock()
			i := 0
			for k := range responseCache {
				if !responseCache[k].HasUser() {
					responseCache[k].Stop()
					delete(responseCache, k)
					i++
				}
			}
			responseCacheLock.Unlock()
			log.Printf("server: gc freed %d ressources", i)
		case <-done:
			log.Println("server: stopping gc")
			return
		}
	}
}
