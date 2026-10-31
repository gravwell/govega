// Copyright (c) 2026, Gravwell, Inc.
// All rights reserved.
//
// Use of this source code is governed by a BSD 2-Clause license that can be
// found in the LICENSE file.

package govega

//go:generate go run genwidths.go -fonts /usr/share/fonts/truetype/liberation

import (
	"strconv"
	"strings"
	"unicode"
)

// Font describes the font a piece of text is drawn with, as parsed from a
// CSS font shorthand string such as "italic bold 11px sans-serif".
type Font struct {
	// Family is the raw font-family list, e.g. `"Helvetica Neue", Arial`.
	Family string
	// Size is the font size in CSS pixels.
	Size float64
	// Weight is the numeric CSS font weight (400 normal, 700 bold).
	Weight int
	// Italic is true for italic or oblique styles.
	Italic bool
}

// Bold reports whether the font weight is bold (600 or heavier).
func (f Font) Bold() bool { return f.Weight >= 600 }

// TextMeasurer returns the advance width, in CSS pixels, of text drawn with
// the given font. Vega uses it for axis/legend layout, label truncation and
// autosize computations. It is called frequently, so implementations should
// be cheap.
type TextMeasurer func(text string, font Font) float64

// DefaultTextMeasurer measures text using embedded width tables derived from
// the Liberation Sans (sans-serif and unknown families) and Liberation Serif
// (serif) fonts, regular and bold, and a fixed 0.6 em per character for
// monospace families. The Liberation fonts are metrically compatible with
// Arial and Times New Roman, and those match Helvetica and Times. Browsers
// use one of these metric-compatible families for the generic sans-serif and
// serif fonts on most platforms, so the result closely tracks what an SVG
// viewer will produce.
func DefaultTextMeasurer(text string, font Font) float64 {
	if font.Size <= 0 || text == "" {
		return 0
	}
	fam := strings.ToLower(font.Family)
	var table *[95]uint16
	var ext map[rune]uint16
	var kern map[uint16]int16
	switch {
	case strings.Contains(fam, "mono") || strings.Contains(fam, "courier") || strings.Contains(fam, "consolas") || strings.Contains(fam, "menlo"):
		return float64(countRunes(text)) * 0.6 * font.Size
	case isSerif(fam):
		if font.Bold() {
			table, ext, kern = &serifBold, serifBoldExt, serifBoldKern
		} else {
			table, ext, kern = &serifRegular, serifRegularExt, serifRegularKern
		}
	default:
		if font.Bold() {
			table, ext, kern = &sansBold, sansBoldExt, sansBoldKern
		} else {
			table, ext, kern = &sansRegular, sansRegularExt, sansRegularKern
		}
	}
	var total float64
	prev := rune(-1)
	for _, r := range text {
		total += runeWidth(r, table, ext)
		// Browsers apply the font's kerning pairs when measuring text.
		if prev >= 32 && prev <= 126 && r >= 32 && r <= 126 {
			total += float64(kern[uint16(prev)<<8|uint16(r)])
		}
		prev = r
	}
	return total * font.Size / 1000
}

func countRunes(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

func isSerif(fam string) bool {
	// "sans-serif" contains "serif"; look for serif families explicitly.
	for f := range strings.SplitSeq(fam, ",") {
		f = strings.Trim(strings.TrimSpace(f), `"'`)
		switch {
		case f == "serif", strings.HasPrefix(f, "times"), f == "georgia", f == "garamond",
			f == "cambria", f == "palatino", f == "book antiqua", f == "liberation serif",
			f == "dejavu serif", f == "noto serif":
			return true
		case f == "sans-serif":
			return false
		}
	}
	return false
}

// runeWidth returns the advance width of r in 1/1000 em units. ASCII and
// the tabulated non-ASCII characters use the font's real widths; anything
// else is approximated.
func runeWidth(r rune, table *[95]uint16, ext map[rune]uint16) float64 {
	if r >= 32 && r <= 126 {
		return float64(table[r-32])
	}
	if w, ok := ext[r]; ok {
		return float64(w)
	}
	switch {
	case r == '\t':
		return float64(table[0]) * 4
	case r < 32 || (r >= 0x7f && r < 0xa0):
		return 0
	case unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r):
		return 0 // combining marks and format characters
	case isWide(r):
		return 1000
	case r >= 0x180 && r <= 0x24f:
		return float64(table['a'-32]) // Latin Extended-B: letter-sized
	case r == 0x2026 || r == 0x2014: // ellipsis, em dash
		return 1000
	case unicode.IsDigit(r):
		return float64(table['0'-32])
	case unicode.IsUpper(r):
		return float64(table['H'-32])
	case unicode.IsLetter(r):
		return float64(table['n'-32])
	default:
		return 600
	}
}

func isWide(r rune) bool {
	return (r >= 0x1100 && r <= 0x115f) || // Hangul Jamo
		(r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) || // CJK radicals .. Yi
		(r >= 0xac00 && r <= 0xd7a3) || // Hangul syllables
		(r >= 0xf900 && r <= 0xfaff) || // CJK compatibility ideographs
		(r >= 0xfe30 && r <= 0xfe4f) || // CJK compatibility forms
		(r >= 0xff00 && r <= 0xff60) || // fullwidth forms
		(r >= 0xffe0 && r <= 0xffe6) ||
		(r >= 0x1f300 && r <= 0x1faff) || // emoji
		(r >= 0x20000 && r <= 0x3fffd)
}

// ParseCSSFont parses a CSS font shorthand string of the form emitted by
// Vega: "[style] [variant] [weight] <size>px <family>". Missing parts take
// CSS defaults (normal style, weight 400, 11px, sans-serif).
func ParseCSSFont(s string) Font {
	f := Font{Weight: 400, Size: 11, Family: "sans-serif"}
	fields := strings.Fields(s)
	for i, tok := range fields {
		if size, ok := parseFontSize(tok); ok {
			f.Size = size
			f.Family = strings.Join(fields[i+1:], " ")
			if f.Family == "" {
				f.Family = "sans-serif"
			}
			return f
		}
		switch lt := strings.ToLower(tok); lt {
		case "italic", "oblique":
			f.Italic = true
		case "bold":
			f.Weight = 700
		case "bolder":
			f.Weight = 900
		case "lighter":
			f.Weight = 300
		case "normal", "small-caps", "ultra-condensed", "extra-condensed", "condensed",
			"semi-condensed", "semi-expanded", "expanded", "extra-expanded", "ultra-expanded":
		default:
			if w, err := strconv.Atoi(lt); err == nil && w >= 1 && w <= 1000 {
				f.Weight = w
			}
		}
	}
	// No size token: treat the whole string as a family list.
	if len(fields) > 0 {
		f.Family = s
	}
	return f
}

func parseFontSize(tok string) (float64, bool) {
	// Strip an optional line-height suffix: "11px/1.2".
	if i := strings.IndexByte(tok, '/'); i >= 0 {
		tok = tok[:i]
	}
	unit := ""
	num := tok
	for _, u := range []string{"px", "pt", "em", "rem", "%"} {
		if strings.HasSuffix(tok, u) {
			unit = u
			num = strings.TrimSuffix(tok, u)
			break
		}
	}
	if num == "" || !(num[0] >= '0' && num[0] <= '9' || num[0] == '.') {
		return 0, false
	}
	if unit == "" && !strings.Contains(num, ".") {
		// A bare integer is a font weight (e.g. "500"), not a size; Vega
		// always emits sizes with a px unit.
		return 0, false
	}
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, false
	}
	switch unit {
	case "pt":
		v *= 4.0 / 3.0
	case "em", "rem":
		v *= 16
	case "%":
		v *= 0.16
	}
	return v, true
}
