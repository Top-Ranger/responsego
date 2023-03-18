// SPDX-License-Identifier: Apache-2.0
// Copyright 2020,2021,2023 Marcus Soll
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
	"log"
)

// ChartValue represents a single data point in a chart.
type ChartValue struct {
	Label string
	Value float64
}

var chartTemplate = template.Must(template.New("chartTemplate").Parse(`
<div class="chart {{.ExtraClass}}">
	<canvas id="{{.ID}}"></canvas>
</div>
<script>
var ctx = document.getElementById('{{.ID}}').getContext('2d');
var chartData = {
	type: {{.Type}},
	data: {
		datasets: [{
			data: [
				{{range $i, $e := .Data }}
				{{$e.Value}},
				{{end}}
			],
			{{if .SingleColour}}
			backgroundColor: {{.SingleColour}},
			{{else}}
			backgroundColor: [
				{{range $i, $e := .Colour }}
				{{$e}},
				{{end}}
				],
			{{end}}
			label: {{.Label}}
		}],
		labels: [
			{{range $i, $e := .Data }}
			{{$e.Label}},
			{{end}}
		],

	},
	options: {
		plugins: {
			title: {
				display: true,
				text: {{.Label}}
			}
		},
		responsive: true,
		{{if .Scales}}
		scales: {
			y: {
				beginAtZero: true
			}
		},
		{{end}}
	}
};
var chart = new Chart(ctx, chartData);
</script>
`))

type chartTemplateStruct struct {
	Data         []ChartValue
	Colour       []string
	SingleColour string
	ID           string
	Type         string
	Label        string
	Scales       bool
	ExtraClass   string
}

func getColours(n int) []string {
	// Error case.
	if n <= 0 {
		return nil
	}

	// Special case: just one data type. Just return a fitting colour.
	if n == 1 {
		return []string{"#503050"}
	}

	// Generate colours based on hsl colour scheme. This allows deterministic distinct colours.
	h := 0
	s := 100
	l := 75

	c := make([]string, n)
	for i := range c {
		c[i] = fmt.Sprintf("hsl(%d,%d%%,%d%%)", h, s, l)
		h = h + 60
		if h >= 360 {
			h = h - 360
			s = s - 50
			l = l - 25
			if s <= 0 {
				s = 100
				l = 75
			}
		}
	}
	return c
}

// PieChart returns a save HTML fragment of the data as a pie chart.
// User must embed chart.js.
func PieChart(v []ChartValue, id, label string) template.HTML {
	td := chartTemplateStruct{
		Data:       v,
		Colour:     getColours(len(v)),
		ID:         id,
		Type:       "pie",
		Label:      label,
		ExtraClass: "piechart",
	}
	output := bytes.NewBuffer(make([]byte, 0))
	err := chartTemplate.Execute(output, td)
	if err != nil {
		log.Printf("pie chart: Error executing template (%s)", err.Error())
	}
	return template.HTML(output.Bytes())
}

// BarChart returns a save HTML fragment of the data as a bar chart.
// User must embed chart.js.
func BarChart(v []ChartValue, id, label string) template.HTML {
	td := chartTemplateStruct{
		Data:         v,
		SingleColour: getColours(1)[0],
		ID:           id,
		Type:         "bar",
		Label:        label,
		Scales:       true,
		ExtraClass:   "barchart",
	}
	output := bytes.NewBuffer(make([]byte, 0))
	err := chartTemplate.Execute(output, td)
	if err != nil {
		log.Printf("bar chart: Error executing template (%s)", err.Error())
	}
	return template.HTML(output.Bytes())
}
