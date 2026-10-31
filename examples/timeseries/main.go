// Copyright (c) 2026, Gravwell, Inc.
// All rights reserved.
//
// Use of this source code is governed by a BSD 2-Clause license that can be
// found in the LICENSE file.

// Timeseries renders a multi-series line chart from time.Time columns, the
// shape of data a metrics or log pipeline typically produces. It shows gaps
// for missing values, a time axis, a legend, and a title set through a
// signal override.
//
//	go run ./examples/timeseries
package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"time"

	"github.com/gravwell/govega"
)

const spec = `{
  "$schema": "https://vega.github.io/schema/vega/v6.json",
  "width": 600,
  "height": 240,
  "padding": 5,
  "signals": [{"name": "chartTitle", "value": "Requests per host"}],
  "title": {"text": {"signal": "chartTitle"}, "anchor": "start"},
  "data": [{"name": "series"}],
  "scales": [
    {"name": "x", "type": "time", "range": "width", "domain": {"data": "series", "field": "ts"}},
    {"name": "y", "type": "linear", "range": "height", "nice": true, "zero": true,
     "domain": {"data": "series", "field": "value"}},
    {"name": "color", "type": "ordinal", "range": "category",
     "domain": {"data": "series", "field": "host"}}
  ],
  "axes": [
    {"orient": "bottom", "scale": "x", "format": "%H:%M", "labelAngle": -30, "labelAlign": "right"},
    {"orient": "left", "scale": "y", "title": "requests / min", "grid": true}
  ],
  "legends": [{"stroke": "color", "title": "Host", "orient": "right"}],
  "marks": [{
    "type": "group",
    "from": {"facet": {"name": "byhost", "data": "series", "groupby": "host"}},
    "marks": [{
      "type": "line",
      "from": {"data": "byhost"},
      "encode": {"enter": {
        "x": {"scale": "x", "field": "ts"},
        "y": {"scale": "y", "field": "value"},
        "stroke": {"scale": "color", "field": "host"},
        "strokeWidth": {"value": 2},
        "defined": {"signal": "isValid(datum.value)"}
      }}
    }]
  }]
}`

func main() {
	r, err := govega.New()
	if err != nil {
		log.Fatal(err)
	}

	// Build three columns: timestamp, host and value. Column order and row
	// order do not matter to Vega; the facet groups rows by host.
	start := time.Date(2026, 10, 29, 8, 0, 0, 0, time.UTC)
	var ts []time.Time
	var hosts []string
	var values []float64
	for i := range 90 {
		t := start.Add(time.Duration(i) * time.Minute)
		for j, host := range []string{"web-1", "web-2", "db-1"} {
			v := 120 + 40*math.Sin(float64(i)/12+float64(j)) + 15*float64(j)
			if host == "web-2" && i >= 40 && i < 48 {
				v = math.NaN() // a gap in the data; NaN becomes null, and the
				// spec's "defined" channel breaks the line there
			}
			ts = append(ts, t)
			hosts = append(hosts, host)
			values = append(values, v)
		}
	}

	req := govega.Request{
		Spec: []byte(spec),
		Data: map[string]govega.Dataset{
			"series": govega.NewDataset(
				govega.Col("ts", ts), // time.Time is sent as epoch milliseconds
				govega.Col("host", hosts),
				govega.Col("value", values),
			),
		},
		// Signals override the spec's values; here the title.
		Signals: map[string]any{"chartTitle": "Requests per host (last 90 minutes)"},
	}
	t0 := time.Now()
	svg, err := r.Render(context.Background(), req)
	if err != nil {
		log.Fatal(err)
	}
	elapsed := time.Since(t0).Round(time.Millisecond)
	if err := os.WriteFile("timeseries.svg", svg, 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wrote timeseries.svg (%d bytes) in %s\n", len(svg), elapsed)
}
