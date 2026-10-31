// Copyright (c) 2026, Gravwell, Inc.
// All rights reserved.
//
// Use of this source code is governed by a BSD 2-Clause license that can be
// found in the LICENSE file.

//go:build ignore

// genwidths generates fontwidths.go from the Liberation Sans and Liberation
// Serif fonts. Run it from the repository root with:
//
//	go run genwidths.go -fonts /usr/share/fonts/truetype/liberation
//
// The directory must contain LiberationSans-Regular.ttf,
// LiberationSans-Bold.ttf, LiberationSerif-Regular.ttf and
// LiberationSerif-Bold.ttf. fontwidths.go was generated from release 2.1.5
// (https://github.com/liberationfonts/liberation-fonts/releases/tag/2.1.5).
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"

	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// tables maps each generated variable to its source font file.
var tables = []struct {
	name, file, desc string
}{
	{"sansRegular", "LiberationSans-Regular.ttf", "sans-serif, regular weight"},
	{"sansBold", "LiberationSans-Bold.ttf", "sans-serif, bold weight"},
	{"serifRegular", "LiberationSerif-Regular.ttf", "serif, regular weight"},
	{"serifBold", "LiberationSerif-Bold.ttf", "serif, bold weight"},
}

func main() {
	dir := flag.String("fonts", "/usr/share/fonts/truetype/liberation", "directory containing the Liberation TTF files")
	out := flag.String("o", "fontwidths.go", "output file")
	flag.Parse()

	var buf bytes.Buffer
	buf.WriteString(header)
	for _, t := range tables {
		w, kern, version, err := advanceWidths(filepath.Join(*dir, t.file))
		if err != nil {
			log.Fatal(err)
		}
		fmt.Fprintf(&buf, "\n// %s: %s. Derived from %s (%s).\nvar %s = [95]uint16{\n", t.name, t.desc, t.file, version, t.name)
		for i, v := range w {
			if i%16 == 0 {
				buf.WriteString("\t")
			}
			fmt.Fprintf(&buf, "%d,", v)
			if i%16 == 15 || i == len(w)-1 {
				fmt.Fprintf(&buf, " // %q\n", string(rune(32+i-i%16))+"..")
			} else {
				buf.WriteString(" ")
			}
		}
		buf.WriteString("}\n")
		fmt.Fprintf(&buf, "\n// %sExt: advance widths of %s for characters outside ASCII (Latin-1\n// Supplement, Latin Extended-A and common punctuation), in thousandths of an em.\nvar %sExt = map[rune]uint16{\n", t.name, t.file, t.name)
		ext := extendedWidths(filepath.Join(*dir, t.file))
		extKeys := make([]int, 0, len(ext))
		for r := range ext {
			extKeys = append(extKeys, int(r))
		}
		sort.Ints(extKeys)
		for i, r := range extKeys {
			if i%8 == 0 {
				buf.WriteString("\t")
			}
			fmt.Fprintf(&buf, "0x%04x: %d,", r, ext[rune(r)])
			if i%8 == 7 || i == len(extKeys)-1 {
				buf.WriteString("\n")
			} else {
				buf.WriteString(" ")
			}
		}
		buf.WriteString("}\n")
		fmt.Fprintf(&buf, "\n// %sKern: kerning adjustments of %s for pairs of ASCII characters, keyed\n// by first<<8 | second, in thousandths of an em.\nvar %sKern = map[uint16]int16{\n", t.name, t.file, t.name)
		keys := make([]int, 0, len(kern))
		for k := range kern {
			keys = append(keys, int(k))
		}
		sort.Ints(keys)
		for i, k := range keys {
			if i%6 == 0 {
				buf.WriteString("\t")
			}
			fmt.Fprintf(&buf, "0x%04x: %d,", k, kern[uint16(k)])
			if i%6 == 5 || i == len(keys)-1 {
				buf.WriteString("\n")
			} else {
				buf.WriteString(" ")
			}
		}
		buf.WriteString("}\n")
	}
	src, err := format.Source(buf.Bytes())
	if err != nil {
		log.Fatalf("formatting output: %v", err)
	}
	if err := os.WriteFile(*out, src, 0o644); err != nil {
		log.Fatal(err)
	}
}

// extendedRunes lists the non-ASCII characters whose widths are tabulated:
// Latin-1 Supplement, Latin Extended-A and the general punctuation and
// symbols that commonly appear in chart labels.
func extendedRunes() []rune {
	var rs []rune
	for r := rune(0xa0); r <= 0x17f; r++ {
		rs = append(rs, r)
	}
	rs = append(rs, 0x2010, 0x2013, 0x2014, 0x2018, 0x2019, 0x201a, 0x201c, 0x201d, 0x201e,
		0x2020, 0x2021, 0x2022, 0x2026, 0x2030, 0x2032, 0x2033, 0x2039, 0x203a, 0x20ac, 0x2122,
		0x2126, 0x2190, 0x2191, 0x2192, 0x2193, 0x2206, 0x2211, 0x2212, 0x221e, 0x2248, 0x2260, 0x2264, 0x2265, 0x25cf)
	return rs
}

// extendedWidths returns the advance widths of extendedRunes that the font
// has glyphs for.
func extendedWidths(path string) map[rune]uint16 {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	f, err := sfnt.Parse(data)
	if err != nil {
		log.Fatal(err)
	}
	var b sfnt.Buffer
	upem := float64(f.UnitsPerEm())
	ppem := fixed.Int26_6(upem * 64)
	out := map[rune]uint16{}
	for _, r := range extendedRunes() {
		gi, err := f.GlyphIndex(&b, r)
		if err != nil || gi == 0 {
			continue
		}
		adv, err := f.GlyphAdvance(&b, gi, ppem, font.HintingNone)
		if err != nil {
			log.Fatal(err)
		}
		out[r] = uint16(math.Round(float64(adv) / 64 / upem * 1000))
	}
	return out
}

// advanceWidths returns the advance width of each character in ASCII
// 32..126 and the kerning adjustment of every ASCII pair that has one, both
// in thousandths of an em, rounded to the nearest integer.
func advanceWidths(path string) ([95]uint16, map[uint16]int16, string, error) {
	var w [95]uint16
	data, err := os.ReadFile(path)
	if err != nil {
		return w, nil, "", err
	}
	f, err := sfnt.Parse(data)
	if err != nil {
		return w, nil, "", fmt.Errorf("%s: %w", path, err)
	}
	var b sfnt.Buffer
	version, err := f.Name(&b, sfnt.NameIDVersion)
	if err != nil {
		return w, nil, "", fmt.Errorf("%s: reading version: %w", path, err)
	}
	upem := float64(f.UnitsPerEm())
	ppem := fixed.Int26_6(upem * 64) // one font unit per 26.6 unit
	var glyphs [95]sfnt.GlyphIndex
	for r := rune(32); r <= 126; r++ {
		gi, err := f.GlyphIndex(&b, r)
		if err != nil || gi == 0 {
			return w, nil, "", fmt.Errorf("%s: no glyph for %q", path, r)
		}
		glyphs[r-32] = gi
		adv, err := f.GlyphAdvance(&b, gi, ppem, font.HintingNone)
		if err != nil {
			return w, nil, "", fmt.Errorf("%s: advance for %q: %w", path, r, err)
		}
		w[r-32] = uint16(math.Round(float64(adv) / 64 / upem * 1000))
	}
	kern := map[uint16]int16{}
	for a := 0; a < 95; a++ {
		for c := 0; c < 95; c++ {
			k, err := f.Kern(&b, glyphs[a], glyphs[c], ppem, font.HintingNone)
			if err == sfnt.ErrNotFound {
				continue // no adjustment for this pair
			}
			if err != nil {
				return w, nil, "", fmt.Errorf("%s: kern: %w", path, err)
			}
			if v := int16(math.Round(float64(k) / 64 / upem * 1000)); v != 0 {
				kern[uint16(a+32)<<8|uint16(c+32)] = v
			}
		}
	}
	return w, kern, version, nil
}

const header = `// Code generated by genwidths.go from the Liberation fonts. DO NOT EDIT.

// The width tables in this file are derived from the Liberation Sans and
// Liberation Serif fonts, release 2.1.5. As a derivative of that Font
// Software, this file alone is distributed under the SIL Open Font License,
// Version 1.1; the full license text is in the LICENSES file at the
// repository root. The rest of this module is not covered by the OFL.
//
// Digitized data copyright (c) 2010 Google Corporation
//	with Reserved Font Arimo, Tinos and Cousine.
// Copyright (c) 2012 Red Hat, Inc.
//	with Reserved Font Name Liberation.
//
// The Liberation fonts are metrically compatible with Arial and Times New
// Roman, which in turn match the Helvetica and Times metrics, so these widths
// predict how SVG viewers lay out the generic sans-serif and serif families.

package govega

// Advance widths in thousandths of an em for the characters ASCII 32..126,
// indexed by character - 32; widths of common non-ASCII characters; and the
// kerning pairs from the font's kern table. Each ASCII width row comment
// names the first character of the row.
`
