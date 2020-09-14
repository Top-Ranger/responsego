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

package helper

import (
	"bytes"
	"fmt"
	"html/template"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

var policy *bluemonday.Policy

func init() {
	policy = bluemonday.NewPolicy()
	policy.AllowElements("a", "b", "blockquote", "br", "caption", "code", "del", "em", "h1", "h2", "h3", "h4", "h5", "h6", "hr", "i", "ins", "kbd", "mark", "p", "pre", "q", "s", "samp", "strong", "sub", "sup", "u")
	policy.AllowLists()
	policy.AllowStandardURLs()
	policy.AllowAttrs("href").OnElements("a")
	policy.RequireNoReferrerOnLinks(true)
	policy.AllowTables()
	policy.AddTargetBlankToFullyQualifiedLinks(true)
}

// Format returns a save html version of the Markdown input.
func Format(b []byte) template.HTML {
	buf := bytes.NewBuffer(make([]byte, 0, len(b)*2))
	md := goldmark.New(goldmark.WithExtensions(extension.GFM), goldmark.WithRendererOptions(html.WithHardWraps()))
	err := md.Convert(b, buf)
	if err != nil {
		return template.HTML(policy.Sanitize(fmt.Sprintf("Error rendering markdown: %s", err.Error())))
	}

	return template.HTML(policy.SanitizeBytes(buf.Bytes()))
}
