// Copyright (c) 2026, Gravwell, Inc.
// All rights reserved.
//
// Use of this source code is governed by a BSD 2-Clause license that can be
// found in the LICENSE file.

package govega

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

var (
	testRendererOnce sync.Once
	testRenderer     *Renderer
	testLogMu        sync.Mutex
	testLogs         []string
)

func newTestRenderer(t *testing.T) *Renderer {
	t.Helper()
	testRendererOnce.Do(func() {
		var err error
		testRenderer, err = New(WithLogger(func(level, msg string) {
			testLogMu.Lock()
			defer testLogMu.Unlock()
			testLogs = append(testLogs, level+": "+msg)
		}))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
	})
	return testRenderer
}

func readSpec(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func writeGolden(t *testing.T, name string, svg []byte) {
	t.Helper()
	if os.Getenv("GOVEGA_WRITE_SVG") == "" {
		return
	}
	if err := os.WriteFile(filepath.Join("testdata", name), svg, 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertSVG(t *testing.T, svg []byte) {
	t.Helper()
	if !bytes.HasPrefix(svg, []byte("<svg")) {
		t.Fatalf("output does not start with <svg: %.80s", svg)
	}
	if !bytes.HasSuffix(bytes.TrimSpace(svg), []byte("</svg>")) {
		t.Fatalf("output does not end with </svg>: ...%s", svg[len(svg)-40:])
	}
}

func TestRenderInlineData(t *testing.T) {
	r := newTestRenderer(t)
	svg, err := r.RenderSVG(context.Background(), readSpec(t, "bar.vg.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	assertSVG(t, svg)
	writeGolden(t, "bar.svg", svg)
	if n := bytes.Count(svg, []byte(`fill="steelblue"`)); n != 8 {
		t.Errorf("expected 8 steelblue bars, found %d", n)
	}
	for _, label := range []string{">A<", ">H<", ">100<"} {
		if !bytes.Contains(svg, []byte(label)) {
			t.Errorf("missing axis label %s", label)
		}
	}
}

func TestRenderReplacesValues(t *testing.T) {
	r := newTestRenderer(t)
	data := map[string]Dataset{
		"table": NewDataset(
			Col("category", []string{"x", "y", "z"}),
			Col("amount", []int{10, 20, 30}),
		),
	}
	svg, err := r.RenderSVG(context.Background(), readSpec(t, "bar.vg.json"), data)
	if err != nil {
		t.Fatal(err)
	}
	assertSVG(t, svg)
	if n := bytes.Count(svg, []byte(`fill="steelblue"`)); n != 3 {
		t.Errorf("expected 3 bars after replacing data, found %d", n)
	}
	if bytes.Contains(svg, []byte(">A<")) || !bytes.Contains(svg, []byte(">z<")) {
		t.Errorf("axis labels do not reflect injected data")
	}
}

func TestRenderTimeSeriesFromColumns(t *testing.T) {
	r := newTestRenderer(t)
	base := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	var ts []time.Time
	var hosts []string
	var vals []float64
	for i := range 24 {
		for _, h := range []string{"web-1", "web-2", "db"} {
			ts = append(ts, base.Add(time.Duration(i)*10*time.Minute))
			hosts = append(hosts, h)
			v := 100 + 50*math.Sin(float64(i)/3) + float64(len(h))*7
			if h == "db" && i == 5 {
				v = math.NaN() // becomes null; the spec's "defined" channel turns it into a gap
			}
			vals = append(vals, v)
		}
	}
	data := map[string]Dataset{
		"series": NewDataset(Col("ts", ts), Col("host", hosts), Col("value", vals)),
	}
	// The spec declares a CSV url; injection must override it without a loader.
	svg, err := r.RenderSVG(context.Background(), readSpec(t, "timeseries.vg.json"), data)
	if err != nil {
		t.Fatal(err)
	}
	assertSVG(t, svg)
	writeGolden(t, "timeseries.svg", svg)
	if n := bytes.Count(svg, []byte(`class="mark-line role-mark"`)); n != 3 {
		t.Errorf("expected 3 line groups (one per host), found %d", n)
	}
	for _, want := range []string{"Requests per host", "web-1", "web-2", ">db<", "requests"} {
		if !bytes.Contains(svg, []byte(want)) {
			t.Errorf("missing %q in SVG", want)
		}
	}
	// Time axis labels use the %H:%M format (rendered in the process local time zone).
	if !regexp.MustCompile(`>\d\d:\d\d<`).Match(svg) {
		t.Errorf("no HH:MM time axis labels in SVG")
	}
}

func TestRenderTransformsAndSignals(t *testing.T) {
	r := newTestRenderer(t)
	rows := []map[string]any{
		{"proto": "tcp", "bytes": 4096},
		{"proto": "udp", "bytes": 2048},
		{"proto": "tcp", "bytes": 1024},
		{"proto": "icmp", "bytes": 0}, // filtered out
		{"proto": nil, "bytes": 8192}, // filtered out
		{"proto": "udp", "bytes": 512},
	}
	req := Request{
		Spec:    readSpec(t, "aggregate.vg.json"),
		Data:    map[string]Dataset{"events": FromRows(rows)},
		Signals: map[string]any{"innerRadius": 20},
	}
	svg, err := r.Render(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	assertSVG(t, svg)
	writeGolden(t, "aggregate.svg", svg)
	if n := bytes.Count(svg, []byte(`aria-roledescription="arc mark container"`)); n != 1 {
		t.Errorf("expected one arc mark container, found %d", n)
	}
	// Two arcs (tcp, udp) with paths.
	arcs := regexp.MustCompile(`<g class="mark-arc role-mark"[^>]*>((?:<path[^>]*>)+)</g>`).FindSubmatch(svg)
	if arcs == nil {
		t.Fatalf("no arc paths in SVG")
	}
	if n := bytes.Count(arcs[1], []byte("<path")); n != 2 {
		t.Errorf("expected 2 arcs, found %d", n)
	}
	if !bytes.Contains(svg, []byte("tcp (5)")) || !bytes.Contains(svg, []byte("udp (2.5)")) && !bytes.Contains(svg, []byte("udp (3)")) {
		t.Errorf("aggregate labels missing: %s", svg)
	}
	if bytes.Contains(svg, []byte("icmp")) {
		t.Errorf("filtered row leaked into output")
	}
}

func TestRenderConfigTheme(t *testing.T) {
	r := newTestRenderer(t)
	req := Request{
		Spec:   readSpec(t, "bar.vg.json"),
		Config: []byte(`{"background": "#123456", "rect": {"fill": "orange"}}`),
	}
	svg, err := r.Render(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(svg, []byte(`fill="#123456"`)) {
		t.Errorf("config background not applied")
	}
}

func TestRenderScaleFactor(t *testing.T) {
	r := newTestRenderer(t)
	svg, err := r.Render(context.Background(), Request{Spec: readSpec(t, "bar.vg.json"), ScaleFactor: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`<svg[^>]* width="8\d\d"`).Match(svg) {
		t.Errorf("scale factor not applied: %.200s", svg)
	}
}

func TestRenderURLWithLoader(t *testing.T) {
	fsys := fstest.MapFS{
		"data/series.csv": &fstest.MapFile{Data: []byte(
			"ts,host,value\n2026-09-20T12:00:00Z,a,1\n2026-09-20T12:10:00Z,a,3\n2026-09-20T12:20:00Z,b,2\n")},
	}
	var loaded []string
	loader := LoaderFunc(func(ctx context.Context, uri string) ([]byte, error) {
		loaded = append(loaded, uri)
		return FSLoader(fsys).Load(ctx, uri)
	})
	r, err := New(WithLoader(loader), WithPoolSize(1))
	if err != nil {
		t.Fatal(err)
	}
	svg, err := r.RenderSVG(context.Background(), readSpec(t, "timeseries.vg.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	assertSVG(t, svg)
	if len(loaded) != 1 || loaded[0] != "data/series.csv" {
		t.Errorf("loader calls = %v", loaded)
	}
	if n := bytes.Count(svg, []byte(`class="mark-line role-mark"`)); n != 2 {
		t.Errorf("expected 2 lines, found %d", n)
	}
}

func TestRenderURLWithoutLoaderFails(t *testing.T) {
	r := newTestRenderer(t)
	_, err := r.RenderSVG(context.Background(), readSpec(t, "timeseries.vg.json"), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "loading is disabled") {
		t.Errorf("unexpected error: %v", err)
	}
	// The runtime must still be usable afterwards.
	if _, err := r.RenderSVG(context.Background(), readSpec(t, "bar.vg.json"), nil); err != nil {
		t.Fatalf("render after failure: %v", err)
	}
}

func TestRenderErrors(t *testing.T) {
	r := newTestRenderer(t)
	cases := map[string]string{
		"empty":         ``,
		"not json":      `{"width": `,
		"array":         `[1,2]`,
		"bad scale":     `{"scales":[{"name":"x","type":"nope"}]}`,
		"bad transform": `{"data":[{"name":"t","values":[{"a":1}],"transform":[{"type":"bogus"}]}]}`,
		"bad expr":      `{"signals":[{"name":"s","update":"1 +"}]}`,
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := r.RenderSVG(context.Background(), []byte(spec), nil)
			if err == nil {
				t.Fatal("expected error")
			}
			if _, ok := errors.AsType[*Error](err); !ok {
				t.Errorf("expected *vega.Error, got %T: %v", err, err)
			}
			t.Logf("error: %v", err)
		})
	}
}

func TestDatasetErrors(t *testing.T) {
	r := newTestRenderer(t)
	spec := readSpec(t, "bar.vg.json")
	_, err := r.RenderSVG(context.Background(), spec, map[string]Dataset{
		"table": NewDataset(Col("category", []string{"a", "b"}), Col("amount", []int{1})),
	})
	if err == nil || !strings.Contains(err.Error(), "expected 2") {
		t.Errorf("ragged columns: %v", err)
	}
	_, err = r.RenderSVG(context.Background(), spec, map[string]Dataset{
		"table": NewDataset(Col("category", "notaslice")),
	})
	if err == nil || !strings.Contains(err.Error(), "must be a slice") {
		t.Errorf("non-slice: %v", err)
	}
}

func TestNewDatasetAddedWhenMissing(t *testing.T) {
	r := newTestRenderer(t)
	spec := []byte(`{
	  "width": 100, "height": 100,
	  "marks": [{"type": "symbol", "from": {"data": "pts"},
	    "encode": {"enter": {"x": {"field": "x"}, "y": {"field": "y"}, "size": {"value": 50}}}}]
	}`)
	svg, err := r.RenderSVG(context.Background(), spec, map[string]Dataset{
		"pts": NewDataset(Col("x", []float64{10, 50, 90}), Col("y", []float64{90, 50, 10})),
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(svg, []byte("<path")); n < 3 {
		t.Errorf("expected at least 3 symbols, got %d paths", n)
	}
}

func TestContextCancel(t *testing.T) {
	r := newTestRenderer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := r.RenderSVG(ctx, readSpec(t, "bar.vg.json"), nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestTimeoutInterruptsJavaScript(t *testing.T) {
	// A static force layout with a huge iteration count is CPU-bound and
	// allocates little, so it reliably outlives the timeout.
	r, err := New(WithTimeout(2*time.Second), WithPoolSize(1))
	if err != nil {
		t.Fatal(err)
	}
	n := 2000
	vals := make([]int, n)
	for i := range vals {
		vals[i] = i
	}
	spec := []byte(`{
	  "width": 10, "height": 10,
	  "data": [{"name": "nodes", "transform": [
	     {"type": "force", "static": true, "iterations": 100000000,
	      "forces": [{"force": "center", "x": 5, "y": 5}, {"force": "collide", "radius": 1}, {"force": "nbody"}]}]}],
	  "marks": [{"type": "symbol", "from": {"data": "nodes"}}]
	}`)
	start := time.Now()
	_, err = r.RenderSVG(context.Background(), spec, map[string]Dataset{"nodes": NewDataset(Col("v", vals))})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded after %v, got %v", time.Since(start), err)
	}
	if el := time.Since(start); el > 6*time.Second {
		t.Errorf("interrupt took too long: %v", el)
	}
	// The interrupted runtime is discarded; the renderer keeps working.
	if _, err := r.RenderSVG(context.Background(), readSpec(t, "bar.vg.json"), nil); err != nil {
		t.Fatalf("render after interrupt: %v", err)
	}
}

func TestConcurrentRenders(t *testing.T) {
	r := newTestRenderer(t)
	spec := readSpec(t, "bar.vg.json")
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := range 32 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			n := 1 + i%5
			cats := make([]string, n)
			amts := make([]float64, n)
			for j := range cats {
				cats[j] = string(rune('a' + j))
				amts[j] = float64(j + 1)
			}
			svg, err := r.RenderSVG(context.Background(), spec, map[string]Dataset{
				"table": NewDataset(Col("category", cats), Col("amount", amts)),
			})
			if err != nil {
				errs <- err
				return
			}
			if got := bytes.Count(svg, []byte(`fill="steelblue"`)); got != n {
				errs <- errors.New("wrong bar count in concurrent render")
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestLoggerReceivesWarnings(t *testing.T) {
	var mu sync.Mutex
	var msgs []string
	r, err := New(WithPoolSize(1), WithLogger(func(level, msg string) {
		mu.Lock()
		defer mu.Unlock()
		msgs = append(msgs, level+": "+msg)
	}))
	if err != nil {
		t.Fatal(err)
	}
	// A scale with an empty domain produces a Vega warning.
	spec := []byte(`{"width": 100, "height": 100,
	  "data": [{"name": "t", "values": []}],
	  "scales": [{"name": "y", "type": "linear", "domain": {"data": "t", "field": "v"}, "range": "height"}],
	  "axes": [{"orient": "left", "scale": "y"}]}`)
	if _, err := r.RenderSVG(context.Background(), spec, nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(msgs) == 0 {
		t.Errorf("expected at least one log message")
	}
	t.Log(msgs)
}

func TestCustomTextMeasurer(t *testing.T) {
	calls := 0
	r, err := New(WithPoolSize(1), WithTextMeasurer(func(text string, f Font) float64 {
		calls++
		return float64(len(text)) * f.Size * 3 // absurdly wide
	}))
	if err != nil {
		t.Fatal(err)
	}
	wide, err := r.RenderSVG(context.Background(), readSpec(t, "bar.vg.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls == 0 {
		t.Fatal("custom measurer never called")
	}
	normal, err := newTestRenderer(t).RenderSVG(context.Background(), readSpec(t, "bar.vg.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	// autosize "pad" grows the canvas to fit the wider y-axis labels.
	re := regexp.MustCompile(`<svg[^>]* width="(\d+)"`)
	w1, w2 := re.FindSubmatch(wide), re.FindSubmatch(normal)
	if w1 == nil || w2 == nil || string(w1[1]) == string(w2[1]) {
		t.Errorf("text measurer did not influence layout: wide=%s normal=%s", w1, w2)
	}
}

func TestVersion(t *testing.T) {
	if Version() != "6.4.0" {
		t.Errorf("Version() = %q", Version())
	}
}

func BenchmarkRenderBar(b *testing.B) {
	r, err := New()
	if err != nil {
		b.Fatal(err)
	}
	spec, _ := os.ReadFile("testdata/bar.vg.json")
	ctx := context.Background()
	if _, err := r.RenderSVG(ctx, spec, nil); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.RenderSVG(ctx, spec, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRenderTimeSeries1k(b *testing.B) {
	r, err := New()
	if err != nil {
		b.Fatal(err)
	}
	spec, _ := os.ReadFile("testdata/timeseries.vg.json")
	base := time.Now()
	n := 1000
	ts := make([]time.Time, n)
	hosts := make([]string, n)
	vals := make([]float64, n)
	for i := range ts {
		ts[i] = base.Add(time.Duration(i) * time.Minute)
		hosts[i] = []string{"a", "b", "c", "d"}[i%4]
		vals[i] = math.Sin(float64(i)/50) * 100
	}
	data := map[string]Dataset{"series": NewDataset(Col("ts", ts), Col("host", hosts), Col("value", vals))}
	ctx := context.Background()
	if _, err := r.RenderSVG(ctx, spec, data); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.RenderSVG(ctx, spec, data); err != nil {
			b.Fatal(err)
		}
	}
}
