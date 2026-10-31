// Copyright (c) 2026, Gravwell, Inc.
// All rights reserved.
//
// Use of this source code is governed by a BSD 2-Clause license that can be
// found in the LICENSE file.

package govega

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestGalleryExamples renders the Vega example gallery specs vendored under
// testdata/examples (https://vega.github.io/vega/examples/) with their data
// files served from testdata/examples/data.
func TestGalleryExamples(t *testing.T) {
	runGallery(t, filepath.Join("testdata", "examples"), "*.vg.json", vegaExpectations,
		FSLoader(os.DirFS(filepath.Join("testdata", "examples"))))
}

// TestGalleryExamplesLite does the same for the Vega-Lite example gallery
// (https://vega.github.io/vega-lite/examples/) vendored under
// testdata/examples-lite. Data files shared with the Vega gallery live only
// under testdata/examples/data.
func TestGalleryExamplesLite(t *testing.T) {
	runGallery(t, filepath.Join("testdata", "examples-lite"), "*.vl.json", liteExpectations,
		MultiLoader(
			FSLoader(os.DirFS(filepath.Join("testdata", "examples-lite"))),
			FSLoader(os.DirFS(filepath.Join("testdata", "examples"))),
		))
}

// expectation lists what a correct rendering of an example must contain, or
// a failure message that a known limitation must produce.
type expectation struct {
	contains []string
	fails    string
	// textOnly marks charts that draw text marks exclusively (no paths).
	textOnly bool
}

var vegaExpectations = map[string]expectation{
	// Uses the d3-geo-projection types (airy, armadillo, ...) registered by
	// the embedded vega-projection-extended bundle.
	"projections": {contains: []string{`class="mark-shape role-mark`, ">airy<", ">mollweide<"}},

	"bar-chart":             {contains: []string{">A<", "steelblue"}},
	"stacked-bar-chart":     {contains: []string{`class="mark-rect role-mark`}},
	"county-unemployment":   {contains: []string{`class="mark-shape role-mark`, ">Unemployment<"}},
	"world-map":             {contains: []string{`class="mark-shape role-mark`}},
	"tree-layout":           {contains: []string{`class="mark-path role-mark`, `class="mark-text role-mark`, ">flare<"}},
	"treemap":               {contains: []string{">analytics<", ">flare<"}},
	"sunburst":              {contains: []string{`class="mark-arc role-mark`}},
	"force-directed-layout": {contains: []string{`class="mark-symbol role-mark`, `class="mark-path role-mark`}},
	"heatmap":               {contains: []string{`class="mark-rect role-mark`, ">12"}},
	"parallel-coordinates":  {contains: []string{">Cylinders<", `class="mark-line role-mark`}},
	"barley-trellis-plot":   {contains: []string{">Crookston<", ">Waseca<"}},
	"job-voyager":           {contains: []string{`class="mark-area role-mark`}},
	"contour-plot":          {contains: []string{`class="mark-path role-mark`, `<image`, "data:image/png;base64,"}},
	"density-heatmaps":      {contains: []string{`<image`, "data:image/png;base64,"}},
	"box-plot":              {contains: []string{`class="mark-rect role-mark`}},
	"violin-plot":           {contains: []string{`class="mark-area role-mark`}},
	"histogram":             {contains: []string{`class="mark-rect role-mark`}},
	"donut-chart":           {contains: []string{`class="mark-arc role-mark`}},
	"radar-chart":           {contains: []string{`class="mark-line role-mark`, `class="mark-text role-mark key-label"`}},
	"edge-bundling":         {contains: []string{`class="mark-line role-mark`}},
	"arc-diagram":           {contains: []string{`class="mark-path role-mark`}},
	"calendar-view":         {contains: []string{`class="mark-rect role-mark`, ">Jan<"}},
	"beeswarm-plot":         {contains: []string{`class="mark-symbol role-mark`}},
	"airport-connections":   {contains: []string{`class="mark-path role-mark`, `class="mark-symbol role-mark`}},
	"timelines":             {contains: []string{`class="mark-rect role-mark`, ">Washington<"}},
	"labeled-scatter-plot":  {contains: []string{`class="mark-text role-mark`, `class="mark-symbol role-mark`}},
	"word-cloud":            {contains: []string{">DECLARATIVE<", ">VISUALIZATION<", "rotate(45)"}, textOnly: true},
}

var liteExpectations = map[string]expectation{
	"bar":                          {contains: []string{`class="mark-rect role-mark`, ">A<"}},
	"trellis_barley":               {contains: []string{">Crookston<", `class="mark-symbol role-mark`}},
	"geo_choropleth":               {contains: []string{`class="mark-shape role-mark`}},
	"arc_pie":                      {contains: []string{`class="mark-arc role-mark`}},
	"rect_heatmap":                 {contains: []string{`class="mark-rect role-mark`}},
	"layer_line_co2_concentration": {contains: []string{`class="mark-line role-mark`}},
	"concat_marginal_histograms":   {contains: []string{`class="mark-rect role-mark`}},
	"boxplot_minmax_2D_vertical":   {contains: []string{`class="mark-rect role-mark`}},
	"interactive_brush":            {contains: []string{`class="mark-symbol role-mark`}},
	"layer_text_heatmap":           {contains: []string{`class="mark-text role-mark`}},
	"wheat_wages":                  {contains: []string{`class="mark-area role-mark`}},
}

func runGallery(t *testing.T, root, pattern string, expectations map[string]expectation, loader Loader) {
	specs, err := filepath.Glob(filepath.Join(root, pattern))
	if err != nil || len(specs) == 0 {
		t.Fatalf("no example specs found in %s: %v", root, err)
	}
	sort.Strings(specs)

	var logs []string
	r, err := New(
		WithLoader(loader),
		WithLogger(func(level, msg string) { logs = append(logs, level+": "+msg) }),
		WithPoolSize(1),
		WithTimeout(120*time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}

	type outcome struct {
		name  string
		dur   time.Duration
		bytes int
	}
	var results []outcome
	outDir := os.Getenv("GOVEGA_WRITE_SVG")
	for _, path := range specs {
		name := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(path), ".vg.json"), ".vl.json")
		t.Run(name, func(t *testing.T) {
			spec, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			exp := expectations[name]
			logs = logs[:0]
			start := time.Now()
			svg, err := r.RenderSVG(context.Background(), spec, nil)
			dur := time.Since(start)
			if exp.fails != "" {
				if err == nil {
					t.Fatalf("expected failure containing %q, but render succeeded", exp.fails)
				}
				if !strings.Contains(err.Error(), exp.fails) {
					t.Fatalf("expected failure containing %q, got: %v", exp.fails, err)
				}
				t.Skipf("known limitation: %v", err)
			}
			if err != nil {
				t.Fatalf("render: %v\nlogs: %s", err, strings.Join(logs, "\n"))
			}
			if outDir != "" {
				_ = os.MkdirAll(outDir, 0o755)
				_ = os.WriteFile(filepath.Join(outDir, name+".svg"), svg, 0o644)
			}
			assertSVG(t, svg)
			checkExample(t, spec, svg, exp)
			for _, l := range logs {
				if strings.HasPrefix(l, "error") {
					t.Errorf("vega error logged: %s", l)
				}
			}
			results = append(results, outcome{name, dur, len(svg)})
		})
	}
	for _, o := range results {
		t.Logf("%-36s %7.1f ms %8d bytes", o.name, float64(o.dur.Microseconds())/1000, o.bytes)
	}
}

var (
	pathRe   = regexp.MustCompile(`<path[ >]`)
	textRe   = regexp.MustCompile(`<text[ >]`)
	svgDimRe = regexp.MustCompile(`<svg[^>]* width="([\d.]+)" height="([\d.]+)"`)
)

func count(re *regexp.Regexp, b []byte) int { return len(re.FindAllIndex(b, -1)) }

// checkExample asserts structural properties that a correct rendering of an
// example must have. Thresholds are deliberately loose: they catch a chart
// that silently rendered without its data (no marks, no labels), not
// pixel-level differences.
func checkExample(t *testing.T, spec, svg []byte, exp expectation) {
	t.Helper()
	m := svgDimRe.FindSubmatch(svg)
	if m == nil {
		t.Fatalf("no width/height on <svg>: %.200s", svg)
	}
	if string(m[1]) == "0" || string(m[2]) == "0" {
		t.Fatalf("zero-sized output %sx%s", m[1], m[2])
	}
	// The root group always contributes background/foreground paths;
	// require more so that data marks must be present.
	if paths := count(pathRe, svg); paths < 3 && !exp.textOnly {
		t.Errorf("only %d <path> elements; expected data marks", paths)
	}
	// Specs with axes, legends or a title must produce text.
	if bytes.Contains(spec, []byte(`"axes"`)) || bytes.Contains(spec, []byte(`"legends"`)) ||
		bytes.Contains(spec, []byte(`"title"`)) || bytes.Contains(spec, []byte(`"encoding"`)) {
		if count(textRe, svg) == 0 {
			t.Errorf("no <text> elements; expected axis/legend/title labels")
		}
	}
	for _, want := range exp.contains {
		if !bytes.Contains(svg, []byte(want)) {
			t.Errorf("missing %q in output", want)
		}
	}
}
