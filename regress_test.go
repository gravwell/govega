// Copyright (c) 2026, Gravwell, Inc.
// All rights reserved.
//
// Use of this source code is governed by a BSD 2-Clause license that can be
// found in the LICENSE file.

package govega

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Regression tests for code-review fixes.

func TestManyHeatmapCanvases(t *testing.T) {
	r, err := New(WithPoolSize(1))
	if err != nil {
		t.Fatal(err)
	}
	var groups []int
	var xs, ys []float64
	for g := range 80 {
		for i := range 20 {
			groups = append(groups, g)
			xs = append(xs, float64(i))
			ys = append(ys, float64((i*7+g)%20))
		}
	}
	spec := []byte(`{"width": 100, "height": 100,
	 "data": [{"name": "pts"},
	  {"name": "density", "source": "pts", "transform": [
	    {"type": "kde2d", "groupby": ["g"], "size": [20, 20], "x": "x", "y": "y", "as": "grid"},
	    {"type": "heatmap", "field": "grid", "color": "red", "opacity": 1}]}],
	 "marks": [{"type": "image", "from": {"data": "density"},
	   "encode": {"update": {"image": {"field": "image"}, "width": {"value": 100}, "height": {"value": 100}}}}]}`)
	svg, err := r.RenderSVG(context.Background(), spec, map[string]Dataset{
		"pts": NewDataset(Col("g", groups), Col("x", xs), Col("y", ys)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(svg), "<image"); n != 80 {
		t.Errorf("expected 80 heatmap images, got %d", n)
	}
}

func TestSignalOverrideReplacesUpdate(t *testing.T) {
	r := newTestRenderer(t)
	spec := []byte(`{"width": 100, "height": 100,
	  "signals": [{"name": "base", "value": 5}, {"name": "label", "update": "'v=' + base"}],
	  "marks": [{"type": "text", "encode": {"update": {"text": {"signal": "label"}}}}]}`)
	svg, err := r.Render(context.Background(), Request{Spec: spec, Signals: map[string]any{"label": "OVERRIDE"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(svg), ">OVERRIDE<") {
		t.Errorf("signal override with an update expression was discarded")
	}
	// Built-in signals without a definition still go through view.signal.
	svg, err = r.Render(context.Background(), Request{Spec: spec, Signals: map[string]any{"width": 300}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(svg), `width="300"`) {
		t.Errorf("width override not applied: %.200s", svg)
	}
	if _, err := r.Render(context.Background(), Request{Spec: spec, Signals: map[string]any{"nope": 1}}); err == nil {
		t.Errorf("unknown signal should still fail")
	}
}

func TestFailOnErrorWithLogNone(t *testing.T) {
	var forwarded atomic.Int32
	r, err := New(WithPoolSize(1), WithLogLevel(LogNone), WithLogger(func(string, string) { forwarded.Add(1) }))
	if err != nil {
		t.Fatal(err)
	}
	spec := []byte(`{"width": 100, "height": 100,
	  "projections": [{"name": "p", "type": "no-such-projection"}],
	  "data": [{"name": "d", "values": [{"type": "Sphere"}]}],
	  "marks": [{"type": "shape", "from": {"data": "d"}, "transform": [{"type": "geoshape", "projection": "p"}]}]}`)
	if _, err := r.RenderSVG(context.Background(), spec, nil); err == nil || !strings.Contains(err.Error(), "Unrecognized projection") {
		t.Errorf("expected projection error with LogNone, got %v", err)
	}
	if n := forwarded.Load(); n != 0 {
		t.Errorf("LogNone forwarded %d messages to the logger", n)
	}
}

func TestConfigAndLocaleMustBeJSONObjects(t *testing.T) {
	r := newTestRenderer(t)
	spec := []byte(`{"width": 10, "height": 10}`)
	for _, cfg := range []string{`{"background": }`, `{}, "mode": "vega-lite"`, `[1]`} {
		_, err := r.Render(context.Background(), Request{Spec: spec, Config: []byte(cfg)})
		if err == nil || !strings.Contains(err.Error(), "Request.Config") {
			t.Errorf("Config %q: got %v", cfg, err)
		}
	}
	if _, err := r.Render(context.Background(), Request{Spec: spec, Locale: []byte(`"x"`)}); err == nil || !strings.Contains(err.Error(), "Request.Locale") {
		t.Errorf("Locale: got %v", err)
	}
}

func TestZeroTimeSignalIsNull(t *testing.T) {
	b, err := jsonMarshal(time.Time{})
	if err != nil || string(b) != "null" {
		t.Errorf("jsonMarshal(time.Time{}) = %s, %v", b, err)
	}
}

func TestGetImageDataGuardNegativeExtent(t *testing.T) {
	r := newTestRenderer(t)
	rt, err := newJSRuntime(r)
	if err != nil {
		t.Fatal(err)
	}
	_, err = rt.vm.RunString(`document.createElement('canvas').getContext('2d').getImageData(0, 0, -200000, 200000)`)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("expected size guard to reject, got %v", err)
	}
}

func TestLoadTimeCanvasSurvivesReset(t *testing.T) {
	r := newTestRenderer(t)
	rt, err := newJSRuntime(r)
	if err != nil {
		t.Fatal(err)
	}
	if rt.canvas.baseSeq == 0 {
		t.Fatal("expected Vega to create a canvas while loading")
	}
	if _, err := rt.render(context.Background(), string(readSpec(t, "bar.vg.json")), "", `{"logLevel":"warn"}`); err != nil {
		t.Fatal(err)
	}
	rt.reset()
	for id := 1; id <= rt.canvas.baseSeq; id++ {
		if _, ok := rt.canvas.canvases[id]; !ok {
			t.Errorf("load-time canvas %d dropped by reset", id)
		}
	}
	if len(rt.canvas.canvases) != rt.canvas.baseSeq {
		t.Errorf("render canvases not dropped: %d live, %d expected", len(rt.canvas.canvases), rt.canvas.baseSeq)
	}
}

func TestURLLoadsRunConcurrently(t *testing.T) {
	const delay = 300 * time.Millisecond
	var inFlight, peak atomic.Int32
	loader := LoaderFunc(func(ctx context.Context, uri string) ([]byte, error) {
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(delay)
		return []byte(`[{"v": 1}, {"v": 2}]`), nil
	})
	r, err := New(WithPoolSize(1), WithLoader(loader))
	if err != nil {
		t.Fatal(err)
	}
	spec := []byte(`{"width": 50, "height": 50,
	  "data": [{"name": "a", "url": "a.json"}, {"name": "b", "url": "b.json"}, {"name": "c", "url": "c.json"}],
	  "marks": [{"type": "rect", "from": {"data": "a"}}, {"type": "rect", "from": {"data": "b"}}, {"type": "rect", "from": {"data": "c"}}]}`)
	start := time.Now()
	if _, err := r.RenderSVG(context.Background(), spec, nil); err != nil {
		t.Fatal(err)
	}
	if el := time.Since(start); el > 2*delay {
		t.Errorf("three loads took %v; expected them to overlap", el)
	}
	if p := peak.Load(); p < 2 {
		t.Errorf("peak concurrent loads = %d, want >= 2", p)
	}
}

func TestCanvasPathRasterization(t *testing.T) {
	r := newTestRenderer(t)
	rt, err := newJSRuntime(r)
	if err != nil {
		t.Fatal(err)
	}
	v, err := rt.vm.RunString(`
	  var c = document.createElement('canvas'); c.width = 40; c.height = 40;
	  var ctx = c.getContext('2d');
	  ctx.fillStyle = 'red';
	  ctx.beginPath(); ctx.arc(20, 20, 10, 0, 2 * Math.PI); ctx.fill();
	  ctx.strokeStyle = '#0000ff'; ctx.lineWidth = 4;
	  ctx.beginPath(); ctx.moveTo(0, 38); ctx.lineTo(40, 38); ctx.stroke();
	  function px(x, y) { return Array.from(ctx.getImageData(x, y, 1, 1).data); }
	  JSON.stringify({ center: px(20, 20), edge: px(20, 11), outside: px(5, 5), line: px(10, 38), aboveLine: px(10, 33) });
	`)
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ Center, Edge, Outside, Line, AboveLine [4]int }
	if err := json.Unmarshal([]byte(v.String()), &got); err != nil {
		t.Fatal(err)
	}
	if got.Center != [4]int{255, 0, 0, 255} {
		t.Errorf("circle center = %v, want opaque red", got.Center)
	}
	if got.Edge[3] == 0 {
		t.Errorf("circle edge pixel is empty; arc was not flattened to the right radius")
	}
	if got.Outside[3] != 0 {
		t.Errorf("pixel outside the circle = %v, want transparent", got.Outside)
	}
	if got.Line != [4]int{0, 0, 255, 255} {
		t.Errorf("stroked line pixel = %v, want opaque blue", got.Line)
	}
	if got.AboveLine[3] != 0 {
		t.Errorf("pixel above the 4px line = %v, want transparent", got.AboveLine)
	}
}

func TestLabelTransformAvoidsMarks(t *testing.T) {
	// With rasterized symbols, the label transform must drop labels that
	// would cover other points; without rasterization every label fits.
	r := newTestRenderer(t)
	// An 8x8 grid of points about 25px apart with 60px-wide labels: some
	// labels fit between neighbours, many would cover a point.
	var xs, ys []float64
	var names []string
	for i := range 64 {
		xs = append(xs, float64(i%8)*10+float64(i%3))
		ys = append(ys, float64(i/8)*10+float64(i%5)/2)
		names = append(names, fmt.Sprintf("point number %d", i))
	}
	spec := []byte(`{"width": 200, "height": 200, "padding": 5,
	  "scales": [{"name": "x", "type": "linear", "range": "width", "domain": {"data": "pts", "field": "x"}},
	             {"name": "y", "type": "linear", "range": "height", "domain": {"data": "pts", "field": "y"}}],
	  "marks": [
	    {"name": "dots", "type": "symbol", "from": {"data": "pts"},
	     "encode": {"enter": {"x": {"scale": "x", "field": "x"}, "y": {"scale": "y", "field": "y"}, "size": {"value": 60}, "fill": {"value": "steelblue"}}}},
	    {"type": "text", "from": {"data": "dots"},
	     "encode": {"enter": {"text": {"field": "datum.name"}, "fontSize": {"value": 9}}},
	     "transform": [{"type": "label", "size": [200, 200], "avoidBaseMark": true, "avoidMarks": ["dots"]}]}
	  ]}`)
	svg, err := r.RenderSVG(context.Background(), spec, map[string]Dataset{
		"pts": NewDataset(Col("x", xs), Col("y", ys), Col("name", names)),
	})
	if err != nil {
		t.Fatal(err)
	}
	// The label transform keeps every text item and hides the ones it could
	// not place with opacity 0.
	texts := regexp.MustCompile(`<text[^>]*>point number`).FindAll(svg, -1)
	visible := 0
	for _, tx := range texts {
		if !bytes.Contains(tx, []byte(`opacity="0"`)) {
			visible++
		}
	}
	if len(texts) != 64 {
		t.Fatalf("expected 64 text items, found %d", len(texts))
	}
	if visible == 0 || visible >= 64 {
		t.Errorf("%d of 64 labels visible; expected some, but not all, to be suppressed by mark avoidance", visible)
	}
	t.Logf("%d of 64 labels visible", visible)
}

func TestSeedMakesRandomReproducible(t *testing.T) {
	r := newTestRenderer(t)
	spec := []byte(`{"width": 100, "height": 100,
	  "data": [{"name": "pts", "transform": [{"type": "formula", "as": "r", "expr": "random()"}]}],
	  "marks": [{"type": "text", "from": {"data": "pts"}, "encode": {"enter": {"text": {"signal": "format(datum.r, '.6f')"}}}}]}`)
	data := map[string]Dataset{"pts": NewDataset(Col("i", []int{1, 2, 3}))}
	a, err := r.Render(context.Background(), Request{Spec: spec, Data: data, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.Render(context.Background(), Request{Spec: spec, Data: data, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	c, err := r.Render(context.Background(), Request{Spec: spec, Data: data, Seed: 8})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("same seed produced different output")
	}
	if bytes.Equal(a, c) {
		t.Errorf("different seeds produced identical output")
	}
	// An unseeded render afterwards must not reuse the seeded generator.
	d1, _ := r.Render(context.Background(), Request{Spec: spec, Data: data})
	d2, _ := r.Render(context.Background(), Request{Spec: spec, Data: data})
	if bytes.Equal(d1, d2) {
		t.Errorf("unseeded renders were identical; Math.random was not restored")
	}
}

// Number.prototype.toLocaleString in goja takes its first argument as a
// radix; d3-format calls it with a locale tag for values of 1e21 and above
// (including Infinity), which must not throw.
func TestNumberToLocaleString(t *testing.T) {
	r := newTestRenderer(t)
	spec := []byte(`{"width": 100, "height": 100,
	  "marks": [{"type": "text", "encode": {"update": {
	    "text": {"signal": "format(1000/0, 'd') + ' ' + format(1e22, 'd')"}}}}]}`)
	svg, err := r.Render(context.Background(), Request{Spec: spec})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(svg), ">∞ 10000000000000000000000<") {
		t.Errorf("unexpected text: %.300s", svg)
	}
}

// Event streams with "source": "window" are common in the interactive
// gallery examples; they must not produce a warning for a static render.
func TestWindowEventSourceIsSilent(t *testing.T) {
	var logs []string
	r, err := New(WithPoolSize(1), WithLogger(func(level, msg string) { logs = append(logs, level+": "+msg) }))
	if err != nil {
		t.Fatal(err)
	}
	spec := []byte(`{"width": 100, "height": 100,
	  "signals": [{"name": "drag", "value": 0,
	    "on": [{"events": {"source": "window", "type": "mousemove"}, "update": "1"}]}],
	  "marks": [{"type": "rect", "encode": {"update": {"x": {"value": 0}, "y": {"value": 0},
	    "width": {"value": 10}, "height": {"value": 10}, "fill": {"value": "red"}}}}]}`)
	if _, err := r.Render(context.Background(), Request{Spec: spec}); err != nil {
		t.Fatal(err)
	}
	if len(logs) != 0 {
		t.Errorf("unexpected log output: %v", logs)
	}
}
