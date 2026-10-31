// Copyright (c) 2026, Gravwell, Inc.
// All rights reserved.
//
// Use of this source code is governed by a BSD 2-Clause license that can be
// found in the LICENSE file.

package govega

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"
)

const liteBarSpec = `{
  "$schema": "https://vega.github.io/schema/vega-lite/v6.json",
  "description": "A simple bar chart with embedded data.",
  "data": {"values": [
    {"a": "A", "b": 28}, {"a": "B", "b": 55}, {"a": "C", "b": 43}
  ]},
  "mark": "bar",
  "encoding": {
    "x": {"field": "a", "type": "nominal", "axis": {"labelAngle": 0}},
    "y": {"field": "b", "type": "quantitative"}
  }
}`

func TestLiteAutoDetect(t *testing.T) {
	r := newTestRenderer(t)
	svg, err := r.RenderSVG(context.Background(), []byte(liteBarSpec), nil)
	if err != nil {
		t.Fatal(err)
	}
	assertSVG(t, svg)
	if n := bytes.Count(svg, []byte(`aria-roledescription="bar"`)); n != 3 {
		t.Errorf("expected 3 bars, found %d", n)
	}
	for _, want := range []string{">A<", ">B<", ">C<", ">b<"} {
		if !bytes.Contains(svg, []byte(want)) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestLiteDetectWithoutSchema(t *testing.T) {
	r := newTestRenderer(t)
	spec := strings.Replace(liteBarSpec, `"$schema": "https://vega.github.io/schema/vega-lite/v6.json",`, "", 1)
	svg, err := r.RenderSVG(context.Background(), []byte(spec), nil)
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(svg, []byte(`aria-roledescription="bar"`)); n != 3 {
		t.Errorf("expected 3 bars, found %d", n)
	}
}

func TestLiteExplicitMode(t *testing.T) {
	r := newTestRenderer(t)
	// Vega ignores the Vega-Lite-only properties, so forcing ModeVega yields
	// an empty chart rather than an error; the point is that detection was
	// bypassed.
	svg, err := r.Render(context.Background(), Request{Spec: []byte(liteBarSpec), Mode: ModeVega})
	if err != nil {
		t.Fatalf("ModeVega: %v", err)
	}
	if bytes.Contains(svg, []byte(`aria-roledescription="bar"`)) {
		t.Errorf("ModeVega should not have compiled the Vega-Lite mark")
	}
	svg, err = r.Render(context.Background(), Request{Spec: []byte(liteBarSpec), Mode: ModeVegaLite})
	if err != nil {
		t.Fatalf("ModeVegaLite: %v", err)
	}
	if n := bytes.Count(svg, []byte(`aria-roledescription="bar"`)); n != 3 {
		t.Errorf("ModeVegaLite: expected 3 bars, found %d", n)
	}
	_, err = r.Render(context.Background(), Request{Spec: []byte(liteBarSpec), Mode: "bogus"})
	if err == nil || !strings.Contains(err.Error(), "unknown mode") {
		t.Errorf("bogus mode: %v", err)
	}
}

func TestLiteNamedDatasetInjection(t *testing.T) {
	r := newTestRenderer(t)
	spec := []byte(`{
	  "$schema": "https://vega.github.io/schema/vega-lite/v6.json",
	  "data": {"name": "table", "url": "data/does-not-exist.csv"},
	  "mark": "line",
	  "encoding": {
	    "x": {"field": "t", "type": "temporal"},
	    "y": {"field": "v", "type": "quantitative"},
	    "color": {"field": "series", "type": "nominal"}
	  }
	}`)
	data := map[string]Dataset{"table": FromRows([]map[string]any{
		{"t": 1758409200000, "v": 1, "series": "a"},
		{"t": 1758412800000, "v": 3, "series": "a"},
		{"t": 1758409200000, "v": 2, "series": "b"},
		{"t": 1758412800000, "v": 1, "series": "b"},
	})}
	svg, err := r.RenderSVG(context.Background(), spec, data)
	if err != nil {
		t.Fatal(err)
	}
	assertSVG(t, svg)
	if n := bytes.Count(svg, []byte(`class="mark-line role-mark`)); n != 2 {
		t.Errorf("expected 2 line groups, found %d", n)
	}
	if !bytes.Contains(svg, []byte(">series<")) {
		t.Errorf("legend title missing")
	}
}

func TestLiteDatasetInjectionIntoLayers(t *testing.T) {
	r := newTestRenderer(t)
	spec := []byte(`{
	  "$schema": "https://vega.github.io/schema/vega-lite/v6.json",
	  "layer": [
	    {"data": {"name": "bars"}, "mark": "bar",
	     "encoding": {"x": {"field": "k", "type": "nominal"}, "y": {"field": "v", "type": "quantitative"}}},
	    {"data": {"name": "rule"}, "mark": "rule",
	     "encoding": {"y": {"field": "y", "type": "quantitative"}, "color": {"value": "red"}}}
	  ]
	}`)
	data := map[string]Dataset{
		"bars": NewDataset(Col("k", []string{"x", "y", "z"}), Col("v", []int{1, 4, 2})),
		"rule": NewDataset(Col("y", []float64{2.5})),
	}
	svg, err := r.RenderSVG(context.Background(), spec, data)
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(svg, []byte(`aria-roledescription="bar"`)); n != 3 {
		t.Errorf("expected 3 bars, found %d", n)
	}
	if !bytes.Contains(svg, []byte(`class="mark-rule role-mark`)) || !bytes.Contains(svg, []byte(`stroke="red"`)) {
		t.Errorf("rule layer missing")
	}
}

func TestLiteConfig(t *testing.T) {
	r := newTestRenderer(t)
	svg, err := r.Render(context.Background(), Request{
		Spec:   []byte(liteBarSpec),
		Config: []byte(`{"background": "#abcdef", "bar": {"color": "#ff8800"}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(svg, []byte(`fill="#abcdef"`)) || !bytes.Contains(svg, []byte(`fill="#ff8800"`)) {
		t.Errorf("Vega-Lite config not applied")
	}
}

func TestLiteVersion(t *testing.T) {
	if !regexp.MustCompile(`^6\.\d+\.\d+$`).MatchString(LiteVersion()) {
		t.Errorf("LiteVersion() = %q", LiteVersion())
	}
}

func TestFailOnLoggedErrorCanBeDisabled(t *testing.T) {
	// An unknown projection type is logged by Vega, not thrown.
	spec := []byte(`{"width": 100, "height": 100,
	  "projections": [{"name": "p", "type": "no-such-projection"}],
	  "marks": [{"type": "shape", "from": {"data": "d"}, "transform": [{"type": "geoshape", "projection": "p"}]}],
	  "data": [{"name": "d", "values": [{"type": "Sphere"}]}]}`)
	strict := newTestRenderer(t)
	if _, err := strict.RenderSVG(context.Background(), spec, nil); err == nil || !strings.Contains(err.Error(), "Unrecognized projection") {
		t.Errorf("strict renderer: expected projection error, got %v", err)
	}
	lenient, err := New(WithPoolSize(1), WithFailOnError(false))
	if err != nil {
		t.Fatal(err)
	}
	svg, err := lenient.RenderSVG(context.Background(), spec, nil)
	if err != nil {
		t.Fatalf("lenient renderer: %v", err)
	}
	assertSVG(t, svg)
}
