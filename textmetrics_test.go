// Copyright (c) 2026, Gravwell, Inc.
// All rights reserved.
//
// Use of this source code is governed by a BSD 2-Clause license that can be
// found in the LICENSE file.

package govega

import (
	"math"
	"testing"
)

func TestParseCSSFont(t *testing.T) {
	cases := []struct {
		in   string
		want Font
	}{
		{"11px sans-serif", Font{Family: "sans-serif", Size: 11, Weight: 400}},
		{"bold 12px sans-serif", Font{Family: "sans-serif", Size: 12, Weight: 700}},
		{"italic 500 10px Helvetica, Arial", Font{Family: "Helvetica, Arial", Size: 10, Weight: 500, Italic: true}},
		{`italic small-caps bold 14px/1.2 "Helvetica Neue"`, Font{Family: `"Helvetica Neue"`, Size: 14, Weight: 700, Italic: true}},
		{"9pt serif", Font{Family: "serif", Size: 12, Weight: 400}},
		{"", Font{Family: "sans-serif", Size: 11, Weight: 400}},
		{"monospace", Font{Family: "monospace", Size: 11, Weight: 400}},
	}
	for _, c := range cases {
		got := ParseCSSFont(c.in)
		if got != c.want {
			t.Errorf("ParseCSSFont(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestDefaultTextMeasurer(t *testing.T) {
	f := Font{Family: "sans-serif", Size: 10, Weight: 400}
	// Sans-serif digits are 556/1000 em.
	if w := DefaultTextMeasurer("100", f); math.Abs(w-16.68) > 1e-9 {
		t.Errorf("width(100) = %v", w)
	}
	// Bold is wider than regular for lowercase text.
	reg := DefaultTextMeasurer("hello world", f)
	bold := DefaultTextMeasurer("hello world", Font{Family: "sans-serif", Size: 10, Weight: 700})
	if bold <= reg {
		t.Errorf("bold %v should exceed regular %v", bold, reg)
	}
	// Serif is narrower than sans-serif.
	serif := DefaultTextMeasurer("hello world", Font{Family: "serif", Size: 10, Weight: 400})
	if serif >= reg {
		t.Errorf("serif %v should be narrower than sans %v", serif, reg)
	}
	// Monospace: 0.6 em per rune.
	if w := DefaultTextMeasurer("abcd", Font{Family: "Menlo, monospace", Size: 10}); math.Abs(w-24) > 1e-9 {
		t.Errorf("mono width = %v", w)
	}
	// Wide characters count as 1 em; combining marks as 0.
	if w := DefaultTextMeasurer("日本", f); math.Abs(w-20) > 1e-9 {
		t.Errorf("CJK width = %v", w)
	}
	if w := DefaultTextMeasurer("é", f); math.Abs(w-5.56) > 1e-9 {
		t.Errorf("combining width = %v", w)
	}
	// Accented Latin maps to base letters.
	if a, b := DefaultTextMeasurer("café", f), DefaultTextMeasurer("cafe", f); a != b {
		t.Errorf("accented %v != base %v", a, b)
	}
	if DefaultTextMeasurer("", f) != 0 || DefaultTextMeasurer("x", Font{Size: 0}) != 0 {
		t.Errorf("empty/zero-size text should measure 0")
	}
	// Browsers report about 51.7px for "Hello World" in 10px Arial.
	if w := DefaultTextMeasurer("Hello World", f); w < 48 || w > 56 {
		t.Errorf("implausible width %v", w)
	}
}
