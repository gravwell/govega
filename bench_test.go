// Copyright (c) 2026, Gravwell, Inc.
// All rights reserved.
//
// Use of this source code is governed by a BSD 2-Clause license that can be
// found in the LICENSE file.

package govega

import (
	"context"
	"math"
	"os"
	"testing"
	"time"
)

// BenchmarkRenderLine50k renders a single line mark with 50,000 points. It
// exercises path string construction for very long paths.
func BenchmarkRenderLine50k(b *testing.B) {
	r, err := New()
	if err != nil {
		b.Fatal(err)
	}
	n := 50000
	xs := make([]float64, n)
	ys := make([]float64, n)
	for i := range xs {
		xs[i] = float64(i)
		ys[i] = math.Sin(float64(i)/500) + math.Cos(float64(i)/77)/3
	}
	spec := []byte(`{
	  "width": 800, "height": 300, "padding": 5,
	  "scales": [
	    {"name": "x", "type": "linear", "range": "width", "domain": {"data": "pts", "field": "x"}},
	    {"name": "y", "type": "linear", "range": "height", "domain": {"data": "pts", "field": "y"}}],
	  "axes": [{"orient": "bottom", "scale": "x"}, {"orient": "left", "scale": "y"}],
	  "marks": [{"type": "line", "from": {"data": "pts"},
	    "encode": {"enter": {"x": {"scale": "x", "field": "x"}, "y": {"scale": "y", "field": "y"}, "stroke": {"value": "steelblue"}}}}]
	}`)
	data := map[string]Dataset{"pts": NewDataset(Col("x", xs), Col("y", ys))}
	ctx := context.Background()
	start := time.Now()
	svg, err := r.RenderSVG(ctx, spec, data)
	if err != nil {
		b.Fatal(err)
	}
	b.Logf("first render: %v, %d bytes", time.Since(start), len(svg))
	if os.Getenv("GOVEGA_WRITE_SVG") != "" {
		_ = os.WriteFile(os.Getenv("GOVEGA_WRITE_SVG")+"/line50k.svg", svg, 0o644)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.RenderSVG(ctx, spec, data); err != nil {
			b.Fatal(err)
		}
	}
}
