// Copyright (c) 2026, Gravwell, Inc.
// All rights reserved.
//
// Use of this source code is governed by a BSD 2-Clause license that can be
// found in the LICENSE file.

// Vegalite renders a Vega-Lite specification. Vega-Lite is detected from the
// $schema URL and compiled to Vega before rendering. Data is supplied by
// name, so the spec refers to {"data": {"name": "events"}}, and a config
// object applies a dark theme.
//
//	go run ./examples/vegalite
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gravwell/govega"
)

const spec = `{
  "$schema": "https://vega.github.io/schema/vega-lite/v6.json",
  "title": "Bytes by protocol and direction",
  "width": 360,
  "height": 220,
  "data": {"name": "events"},
  "mark": {"type": "bar", "tooltip": true},
  "encoding": {
    "x": {"field": "proto", "type": "nominal", "title": "Protocol", "axis": {"labelAngle": 0}},
    "y": {"aggregate": "sum", "field": "bytes", "type": "quantitative", "title": "Bytes"},
    "color": {"field": "direction", "type": "nominal", "title": "Direction"},
    "xOffset": {"field": "direction"}
  }
}`

// config is a Vega-Lite config object. It is merged with the spec and
// applied by the Vega-Lite compiler.
const config = `{
  "background": "#1e1e2e",
  "title": {"color": "#cdd6f4", "fontSize": 14},
  "axis": {"labelColor": "#a6adc8", "titleColor": "#cdd6f4", "gridColor": "#313244", "domainColor": "#45475a", "tickColor": "#45475a"},
  "legend": {"labelColor": "#a6adc8", "titleColor": "#cdd6f4"},
  "range": {"category": ["#89b4fa", "#f38ba8"]}
}`

func main() {
	r, err := govega.New()
	if err != nil {
		log.Fatal(err)
	}

	// Row-oriented data, as it might come out of a query result, converted
	// to columns with FromRows.
	rows := []map[string]any{
		{"proto": "tcp", "direction": "in", "bytes": 48_120},
		{"proto": "tcp", "direction": "out", "bytes": 120_880},
		{"proto": "udp", "direction": "in", "bytes": 9_300},
		{"proto": "udp", "direction": "out", "bytes": 2_210},
		{"proto": "icmp", "direction": "in", "bytes": 640},
		{"proto": "icmp", "direction": "out", "bytes": 640},
		{"proto": "tcp", "direction": "in", "bytes": 31_000},
	}
	start := time.Now()
	svg, err := r.Render(context.Background(), govega.Request{
		Spec:   []byte(spec),
		Data:   map[string]govega.Dataset{"events": govega.FromRows(rows)},
		Config: []byte(config),
	})
	if err != nil {
		log.Fatal(err)
	}
	elapsed := time.Since(start).Round(time.Millisecond)
	if err := os.WriteFile("vegalite.svg", svg, 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wrote vegalite.svg (%d bytes) in %s\n", len(svg), elapsed)
}
