// Copyright (c) 2026, Gravwell, Inc.
// All rights reserved.
//
// Use of this source code is governed by a BSD 2-Clause license that can be
// found in the LICENSE file.

// Basic renders a bar chart from Go slices and writes bar.svg.
//
//	go run ./examples/basic
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gravwell/govega"
)

// The spec is a plain Vega specification. The data set named "table" has no
// values here; they are supplied from Go at render time.
const spec = `{
  "$schema": "https://vega.github.io/schema/vega/v6.json",
  "width": 400,
  "height": 200,
  "padding": 5,
  "data": [{"name": "table"}],
  "scales": [
    {"name": "x", "type": "band", "domain": {"data": "table", "field": "category"},
     "range": "width", "padding": 0.1},
    {"name": "y", "type": "linear", "domain": {"data": "table", "field": "amount"},
     "range": "height", "nice": true, "zero": true}
  ],
  "axes": [
    {"orient": "bottom", "scale": "x"},
    {"orient": "left", "scale": "y", "title": "Amount"}
  ],
  "marks": [{
    "type": "rect",
    "from": {"data": "table"},
    "encode": {"enter": {
      "x": {"scale": "x", "field": "category"},
      "width": {"scale": "x", "band": 1},
      "y": {"scale": "y", "field": "amount"},
      "y2": {"scale": "y", "value": 0},
      "fill": {"value": "steelblue"}
    }}
  }]
}`

func main() {
	r, err := govega.New()
	if err != nil {
		log.Fatal(err)
	}

	data := map[string]govega.Dataset{
		"table": govega.NewDataset(
			govega.Col("category", []string{"A", "B", "C", "D", "E"}),
			govega.Col("amount", []float64{28, 55, 43, 91, 81}),
		),
	}
	start := time.Now()
	svg, err := r.RenderSVG(context.Background(), []byte(spec), data)
	if err != nil {
		log.Fatal(err)
	}
	elapsed := time.Since(start).Round(time.Millisecond)
	if err := os.WriteFile("bar.svg", svg, 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wrote bar.svg (%d bytes) in %s\n", len(svg), elapsed)
}
