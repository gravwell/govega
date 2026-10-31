// Copyright (c) 2026, Gravwell, Inc.
// All rights reserved.
//
// Use of this source code is governed by a BSD 2-Clause license that can be
// found in the LICENSE file.

package govega

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/dop251/goja"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/f64"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// pixelCanvas backs the JavaScript <canvas> shim with a real RGBA bitmap.
// Vega needs pixels for a few transforms: `heatmap` writes an image and
// exports it as a PNG data URI, `wordcloud` rasterizes glyphs to pack words,
// and `label` rasterizes marks into occupancy bitmaps. Text, path fills and
// strokes, rectangles and raw image data are rasterized; clipping and
// drawImage are not.
type pixelCanvas struct {
	img *image.NRGBA
	ras *vector.Rasterizer // reused across path fills
}

func newPixelCanvas(w, h int) *pixelCanvas {
	return &pixelCanvas{img: image.NewNRGBA(image.Rect(0, 0, clampDim(w), clampDim(h)))}
}

// clampDim bounds canvas dimensions so a hostile spec cannot allocate an
// unbounded bitmap.
func clampDim(v int) int {
	if v < 0 {
		return 0
	}
	if v > 8192 {
		return 8192
	}
	return v
}

func (c *pixelCanvas) width() int  { return c.img.Rect.Dx() }
func (c *pixelCanvas) height() int { return c.img.Rect.Dy() }

func (c *pixelCanvas) resize(w, h int) {
	c.img = image.NewNRGBA(image.Rect(0, 0, clampDim(w), clampDim(h)))
}

// getImageData copies the region into a fresh RGBA byte slice; pixels
// outside the canvas are transparent black.
func (c *pixelCanvas) getImageData(x, y, w, h int) []byte {
	if w < 0 {
		x, w = x+w, -w
	}
	if h < 0 {
		y, h = y+h, -h
	}
	out := make([]byte, w*h*4)
	for row := 0; row < h; row++ {
		sy := y + row
		if sy < 0 || sy >= c.height() {
			continue
		}
		for col := 0; col < w; col++ {
			sx := x + col
			if sx < 0 || sx >= c.width() {
				continue
			}
			si := c.img.PixOffset(sx, sy)
			di := (row*w + col) * 4
			copy(out[di:di+4], c.img.Pix[si:si+4])
		}
	}
	return out
}

func (c *pixelCanvas) putImageData(data []byte, sw, sh, dx, dy int) {
	for row := range sh {
		ty := dy + row
		if ty < 0 || ty >= c.height() {
			continue
		}
		for col := range sw {
			tx := dx + col
			if tx < 0 || tx >= c.width() {
				continue
			}
			si := (row*sw + col) * 4
			if si+4 > len(data) {
				return
			}
			di := c.img.PixOffset(tx, ty)
			copy(c.img.Pix[di:di+4], data[si:si+4])
		}
	}
}

// affine is a canvas transformation matrix [a b c d e f]:
// x' = a*x + c*y + e, y' = b*x + d*y + f.
type affine [6]float64

func (m affine) apply(x, y float64) (float64, float64) {
	return m[0]*x + m[2]*y + m[4], m[1]*x + m[3]*y + m[5]
}

// rectBounds returns the axis-aligned integer bounds of a transformed rect.
func (m affine) rectBounds(x, y, w, h float64) image.Rectangle {
	minx, miny := math.Inf(1), math.Inf(1)
	maxx, maxy := math.Inf(-1), math.Inf(-1)
	for _, p := range [][2]float64{{x, y}, {x + w, y}, {x, y + h}, {x + w, y + h}} {
		px, py := m.apply(p[0], p[1])
		minx, maxx = math.Min(minx, px), math.Max(maxx, px)
		miny, maxy = math.Min(miny, py), math.Max(maxy, py)
	}
	return image.Rect(int(math.Floor(minx)), int(math.Floor(miny)), int(math.Ceil(maxx)), int(math.Ceil(maxy)))
}

func (c *pixelCanvas) clearRect(m affine, x, y, w, h float64) {
	r := m.rectBounds(x, y, w, h).Intersect(c.img.Rect)
	draw.Draw(c.img, r, image.Transparent, image.Point{}, draw.Src)
}

func (c *pixelCanvas) fillRect(m affine, x, y, w, h float64, col color.NRGBA) {
	r := m.rectBounds(x, y, w, h).Intersect(c.img.Rect)
	draw.Draw(c.img, r, image.NewUniform(col), image.Point{}, draw.Over)
}

// subpath is a flattened polyline in canvas space.
type subpath struct {
	pts    []float64 // x0, y0, x1, y1, ...
	closed bool
}

// decodePath parses the host path encoding [count, closed, x0, y0, ...]*.
func decodePath(data []byte) ([]subpath, error) {
	if len(data)%8 != 0 {
		return nil, fmt.Errorf("canvas: path buffer length %d is not a multiple of 8", len(data))
	}
	vals := make([]float64, len(data)/8)
	for i := range vals {
		vals[i] = math.Float64frombits(binary.LittleEndian.Uint64(data[8*i:]))
	}
	var out []subpath
	for i := 0; i < len(vals); {
		if i+2 > len(vals) {
			return nil, fmt.Errorf("canvas: truncated path header")
		}
		n := int(vals[i])
		closed := vals[i+1] != 0
		i += 2
		if n < 0 || i+2*n > len(vals) {
			return nil, fmt.Errorf("canvas: truncated subpath")
		}
		if n > 0 {
			out = append(out, subpath{pts: vals[i : i+2*n], closed: closed})
		}
		i += 2 * n
	}
	return out, nil
}

// fillPolygons rasterizes polygons (each implicitly closed) with the non-zero
// winding rule and composites col over the canvas.
func (c *pixelCanvas) fillPolygons(polys [][]float64, col color.NRGBA) {
	minx, miny := math.Inf(1), math.Inf(1)
	maxx, maxy := math.Inf(-1), math.Inf(-1)
	for _, p := range polys {
		for i := 0; i+1 < len(p); i += 2 {
			x, y := p[i], p[i+1]
			if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
				return
			}
			minx, maxx = math.Min(minx, x), math.Max(maxx, x)
			miny, maxy = math.Min(miny, y), math.Max(maxy, y)
		}
	}
	if minx > maxx {
		return
	}
	r := image.Rect(int(math.Floor(minx)), int(math.Floor(miny)), int(math.Ceil(maxx))+1, int(math.Ceil(maxy))+1).Intersect(c.img.Rect)
	if r.Empty() {
		return
	}
	if c.ras == nil {
		c.ras = vector.NewRasterizer(r.Dx(), r.Dy())
	} else {
		c.ras.Reset(r.Dx(), r.Dy())
	}
	ox, oy := float32(r.Min.X), float32(r.Min.Y)
	for _, p := range polys {
		if len(p) < 6 {
			continue
		}
		c.ras.MoveTo(float32(p[0])-ox, float32(p[1])-oy)
		for i := 2; i+1 < len(p); i += 2 {
			c.ras.LineTo(float32(p[i])-ox, float32(p[i+1])-oy)
		}
		c.ras.ClosePath()
	}
	c.ras.Draw(c.img, r, image.NewUniform(col), image.Point{})
}

// fillPath fills the subpaths with the non-zero winding rule.
func (c *pixelCanvas) fillPath(subs []subpath, col color.NRGBA) {
	polys := make([][]float64, 0, len(subs))
	for _, s := range subs {
		polys = append(polys, s.pts)
	}
	c.fillPolygons(polys, col)
}

// strokePath approximates stroking: every segment becomes a quad of the
// line width and every joint a polygonal disc, all oriented the same way so
// that the non-zero rule unions them.
func (c *pixelCanvas) strokePath(subs []subpath, col color.NRGBA, width float64) {
	hw := math.Max(width, 0.5) / 2
	var polys [][]float64
	disc := func(x, y float64) {
		const n = 8
		p := make([]float64, 0, 2*n)
		for i := range n {
			a := 2 * math.Pi * float64(i) / n
			p = append(p, x+hw*math.Cos(a), y+hw*math.Sin(a))
		}
		polys = append(polys, p)
	}
	for _, s := range subs {
		pts := s.pts
		n := len(pts) / 2
		if n == 1 {
			continue
		}
		segs := n - 1
		if s.closed {
			segs = n
		}
		for i := 0; i < segs; i++ {
			x0, y0 := pts[2*i], pts[2*i+1]
			j := (i + 1) % n
			x1, y1 := pts[2*j], pts[2*j+1]
			dx, dy := x1-x0, y1-y0
			l := math.Hypot(dx, dy)
			if l == 0 {
				continue
			}
			nx, ny := -dy/l*hw, dx/l*hw
			polys = append(polys, ccw([]float64{x0 + nx, y0 + ny, x1 + nx, y1 + ny, x1 - nx, y1 - ny, x0 - nx, y0 - ny}))
			if hw > 0.75 && (i > 0 || s.closed) {
				disc(x0, y0)
			}
		}
	}
	c.fillPolygons(polys, col)
}

// ccw returns p oriented counter-clockwise (positive signed area).
func ccw(p []float64) []float64 {
	var area float64
	n := len(p) / 2
	for i := range n {
		j := (i + 1) % n
		area += p[2*i]*p[2*j+1] - p[2*j]*p[2*i+1]
	}
	if area < 0 {
		for i, j := 0, n-1; i < j; i, j = i+1, j-1 {
			p[2*i], p[2*j] = p[2*j], p[2*i]
			p[2*i+1], p[2*j+1] = p[2*j+1], p[2*i+1]
		}
	}
	return p
}

func withAlpha(col color.NRGBA, alpha float64) color.NRGBA {
	if alpha >= 1 || math.IsNaN(alpha) {
		return col
	}
	if alpha < 0 {
		alpha = 0
	}
	col.A = uint8(math.Round(float64(col.A) * alpha))
	return col
}

func (c *pixelCanvas) toDataURL() string {
	var buf bytes.Buffer
	if err := png.Encode(&buf, c.img); err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// --- text rasterization -------------------------------------------------------

var (
	glyphFontsOnce sync.Once
	glyphRegular   *opentype.Font
	glyphBold      *opentype.Font
	glyphFontsErr  error
)

func loadGlyphFonts() error {
	glyphFontsOnce.Do(func() {
		glyphRegular, glyphFontsErr = opentype.Parse(goregular.TTF)
		if glyphFontsErr != nil {
			return
		}
		glyphBold, glyphFontsErr = opentype.Parse(gobold.TTF)
	})
	return glyphFontsErr
}

type faceKey struct {
	bold bool
	size float64
}

// faceCache caches opentype faces per runtime; faces are not safe for
// concurrent use, and each runtime serves one render at a time.
type faceCache map[faceKey]font.Face

func (fc faceCache) get(bold bool, size float64) (font.Face, error) {
	if err := loadGlyphFonts(); err != nil {
		return nil, err
	}
	size = math.Round(size*2) / 2
	if size < 1 {
		size = 1
	}
	if size > 512 {
		size = 512
	}
	k := faceKey{bold, size}
	if f, ok := fc[k]; ok {
		return f, nil
	}
	if len(fc) > 256 {
		for k := range fc {
			delete(fc, k)
		}
	}
	src := glyphRegular
	if bold {
		src = glyphBold
	}
	f, err := opentype.NewFace(src, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return nil, err
	}
	fc[k] = f
	return f, nil
}

// textParams describes a fillText/strokeText call.
type textParams struct {
	text     string
	font     Font
	color    color.NRGBA
	align    string  // start|end|left|right|center
	baseline string  // alphabetic|top|hanging|middle|ideographic|bottom
	m        affine  // current transformation
	dilate   float64 // stroke half-width in px, 0 for fill
	measurer TextMeasurer
	faces    faceCache
}

// fillText rasterizes text with the embedded Go fonts. The glyph image is
// scaled horizontally so its advance width matches the width the configured
// TextMeasurer reports, keeping layout consistent with measureText.
func (c *pixelCanvas) fillText(p textParams) error {
	if p.text == "" || p.font.Size <= 0 {
		return nil
	}
	face, err := p.faces.get(p.font.Bold(), p.font.Size)
	if err != nil {
		return err
	}
	metrics := face.Metrics()
	ascent := float64(metrics.Ascent) / 64
	descent := float64(metrics.Descent) / 64
	natural := float64(font.MeasureString(face, p.text)) / 64
	if natural <= 0 {
		return nil
	}
	target := p.measurer(p.text, p.font)
	sx := 1.0
	if target > 0 {
		sx = target / natural
		if sx < 0.5 {
			sx = 0.5
		} else if sx > 2 {
			sx = 2
		}
	}
	width := natural * sx

	pad := int(math.Ceil(p.dilate)) + 1
	gw := int(math.Ceil(natural)) + 2*pad
	gh := int(math.Ceil(ascent+descent)) + 2*pad
	mask := image.NewAlpha(image.Rect(0, 0, gw, gh))
	d := font.Drawer{Dst: mask, Src: image.Opaque, Face: face,
		Dot: fixed.Point26_6{X: fixed.I(pad), Y: fixed.Int26_6(math.Round((ascent + float64(pad)) * 64))}}
	d.DrawString(p.text)
	if p.dilate > 0 {
		mask = dilateAlpha(mask, int(math.Ceil(p.dilate)))
	}
	glyph := image.NewNRGBA(mask.Rect)
	draw.DrawMask(glyph, glyph.Rect, image.NewUniform(p.color), image.Point{}, mask, image.Point{}, draw.Src)

	// Local text-space origin: x per textAlign, y per textBaseline.
	var ax float64
	switch p.align {
	case "center":
		ax = -width / 2
	case "end", "right":
		ax = -width
	}
	var by float64
	switch p.baseline {
	case "top", "hanging":
		by = ascent
	case "middle":
		by = (ascent - descent) / 2
	case "bottom", "ideographic":
		by = -descent
	}
	// mask pixel (u, v) -> local (ax + (u - pad) * sx, by + (v - pad) - ascent)
	// -> canvas via m.
	local := affine{sx, 0, 0, 1, ax - float64(pad)*sx, by - float64(pad) - ascent}
	total := compose(p.m, local)
	m3 := f64.Aff3{total[0], total[2], total[4], total[1], total[3], total[5]}
	xdraw.BiLinear.Transform(c.img, m3, glyph, glyph.Rect, xdraw.Over, nil)
	return nil
}

// compose returns outer ∘ inner.
func compose(outer, inner affine) affine {
	return affine{
		outer[0]*inner[0] + outer[2]*inner[1],
		outer[1]*inner[0] + outer[3]*inner[1],
		outer[0]*inner[2] + outer[2]*inner[3],
		outer[1]*inner[2] + outer[3]*inner[3],
		outer[0]*inner[4] + outer[2]*inner[5] + outer[4],
		outer[1]*inner[4] + outer[3]*inner[5] + outer[5],
	}
}

// dilateAlpha grows opaque regions by r pixels (square kernel), used to
// approximate strokeText for word-cloud padding.
func dilateAlpha(src *image.Alpha, r int) *image.Alpha {
	dst := image.NewAlpha(src.Rect)
	w, h := src.Rect.Dx(), src.Rect.Dy()
	for y := range h {
		for x := range w {
			var maxA uint8
			for dy := -r; dy <= r && maxA < 255; dy++ {
				yy := y + dy
				if yy < 0 || yy >= h {
					continue
				}
				for dx := -r; dx <= r; dx++ {
					xx := x + dx
					if xx < 0 || xx >= w {
						continue
					}
					if a := src.Pix[yy*src.Stride+xx]; a > maxA {
						maxA = a
					}
				}
			}
			dst.Pix[y*dst.Stride+x] = maxA
		}
	}
	return dst
}

// --- colors ---------------------------------------------------------------------

var namedColors = map[string]color.NRGBA{
	"black": {0, 0, 0, 255}, "white": {255, 255, 255, 255}, "red": {255, 0, 0, 255},
	"green": {0, 128, 0, 255}, "blue": {0, 0, 255, 255}, "yellow": {255, 255, 0, 255},
	"cyan": {0, 255, 255, 255}, "aqua": {0, 255, 255, 255}, "magenta": {255, 0, 255, 255},
	"fuchsia": {255, 0, 255, 255}, "gray": {128, 128, 128, 255}, "grey": {128, 128, 128, 255},
	"silver": {192, 192, 192, 255}, "maroon": {128, 0, 0, 255}, "olive": {128, 128, 0, 255},
	"lime": {0, 255, 0, 255}, "teal": {0, 128, 128, 255}, "navy": {0, 0, 128, 255},
	"purple": {128, 0, 128, 255}, "orange": {255, 165, 0, 255}, "steelblue": {70, 130, 180, 255},
	"transparent": {0, 0, 0, 0},
}

// parseCSSColor parses #rgb, #rgba, #rrggbb, #rrggbbaa, rgb()/rgba() and a
// few named colors. Unknown values are opaque black.
func parseCSSColor(s string) color.NRGBA {
	s = strings.TrimSpace(strings.ToLower(s))
	if c, ok := namedColors[s]; ok {
		return c
	}
	if strings.HasPrefix(s, "#") {
		h := s[1:]
		var digits []byte
		switch len(h) {
		case 3, 4:
			for i := 0; i < len(h); i++ {
				digits = append(digits, h[i], h[i])
			}
		case 6, 8:
			digits = []byte(h)
		default:
			return color.NRGBA{A: 255}
		}
		v := make([]uint8, len(digits)/2)
		for i := range v {
			n, err := strconv.ParseUint(string(digits[2*i:2*i+2]), 16, 8)
			if err != nil {
				return color.NRGBA{A: 255}
			}
			v[i] = uint8(n)
		}
		c := color.NRGBA{R: v[0], G: v[1], B: v[2], A: 255}
		if len(v) == 4 {
			c.A = v[3]
		}
		return c
	}
	if strings.HasPrefix(s, "rgb") {
		i, j := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
		if i < 0 || j < i {
			return color.NRGBA{A: 255}
		}
		parts := strings.FieldsFunc(s[i+1:j], func(r rune) bool { return r == ',' || r == ' ' || r == '/' })
		if len(parts) < 3 {
			return color.NRGBA{A: 255}
		}
		ch := func(p string) uint8 {
			if before, ok := strings.CutSuffix(p, "%"); ok {
				f, _ := strconv.ParseFloat(before, 64)
				return uint8(math.Round(f * 2.55))
			}
			f, _ := strconv.ParseFloat(p, 64)
			return uint8(math.Max(0, math.Min(255, math.Round(f))))
		}
		c := color.NRGBA{R: ch(parts[0]), G: ch(parts[1]), B: ch(parts[2]), A: 255}
		if len(parts) > 3 {
			a := parts[3]
			if before, ok := strings.CutSuffix(a, "%"); ok {
				f, _ := strconv.ParseFloat(before, 64)
				c.A = uint8(math.Round(f * 2.55))
			} else {
				f, _ := strconv.ParseFloat(a, 64)
				c.A = uint8(math.Max(0, math.Min(255, math.Round(f*255))))
			}
		}
		return c
	}
	return color.NRGBA{A: 255}
}

// --- host bindings -----------------------------------------------------------------

// Limits on canvas allocation within one render. Canvases cannot be freed
// early (Vega's heatmap transform keeps each canvas as the image value and
// encodes it when the SVG is written), so memory is bounded by total pixels
// rather than by canvas count. maxCanvases only bounds map overhead.
const (
	maxCanvasPixels = 64 << 20 // 256 MiB of RGBA
	maxCanvases     = 4096
)

// canvasHost holds the canvases created by one runtime. Canvases created
// while the bundles load (ids up to baseSeq, such as Vega's text-measurement
// canvas) live as long as the runtime; the rest are dropped after each
// render.
type canvasHost struct {
	rt       *jsRuntime
	canvases map[int]*pixelCanvas
	seq      int
	baseSeq  int
	pixels   int64 // total pixels of all live canvases
	faces    faceCache
}

func newCanvasHost(rt *jsRuntime) *canvasHost {
	return &canvasHost{rt: rt, canvases: map[int]*pixelCanvas{}, faces: faceCache{}}
}

// markBase records that every canvas created so far belongs to the runtime
// itself and must survive reset.
func (h *canvasHost) markBase() { h.baseSeq = h.seq }

func (h *canvasHost) reset() {
	h.pixels = 0
	for id, c := range h.canvases {
		if id > h.baseSeq {
			delete(h.canvases, id)
			continue
		}
		h.pixels += int64(c.width()) * int64(c.height())
	}
	if len(h.faces) > 64 {
		h.faces = faceCache{}
	}
}

// reserve accounts for a change of delta pixels in live canvases, failing
// if the render would exceed maxCanvasPixels.
func (h *canvasHost) reserve(delta int64) {
	if delta > 0 && h.pixels+delta > maxCanvasPixels {
		panic(h.rt.vm.NewGoError(fmt.Errorf("canvas: render exceeds the %d pixel canvas budget", int64(maxCanvasPixels))))
	}
	h.pixels += delta
}

func (h *canvasHost) get(call goja.FunctionCall) *pixelCanvas {
	c, ok := h.canvases[int(call.Argument(0).ToInteger())]
	if !ok {
		panic(h.rt.vm.NewTypeError("canvas: unknown canvas id"))
	}
	return c
}

func argMatrix(call goja.FunctionCall, i int) affine {
	var m affine
	for k := range m {
		m[k] = call.Argument(i + k).ToFloat()
	}
	return m
}

func (h *canvasHost) install(host *goja.Object) {
	vm := h.rt.vm
	num := func(v goja.Value) float64 {
		f := v.ToFloat()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return 0
		}
		return f
	}
	// coord converts a JavaScript number to an int coordinate, clamping it so
	// that the float-to-int conversion and later products cannot overflow.
	coord := func(v goja.Value) int {
		const lim = 1 << 30
		return int(math.Max(-lim, math.Min(lim, num(v))))
	}
	_ = host.Set("canvasCreate", func(call goja.FunctionCall) goja.Value {
		if len(h.canvases) >= maxCanvases {
			panic(vm.NewGoError(fmt.Errorf("canvas: too many canvases in one render")))
		}
		w, hh := clampDim(coord(call.Argument(0))), clampDim(coord(call.Argument(1)))
		h.reserve(int64(w) * int64(hh))
		h.seq++
		h.canvases[h.seq] = newPixelCanvas(w, hh)
		return vm.ToValue(h.seq)
	})
	_ = host.Set("canvasResize", func(call goja.FunctionCall) goja.Value {
		c := h.get(call)
		w, hh := clampDim(coord(call.Argument(1))), clampDim(coord(call.Argument(2)))
		h.reserve(int64(w)*int64(hh) - int64(c.width())*int64(c.height()))
		c.resize(w, hh)
		return goja.Undefined()
	})
	_ = host.Set("canvasGetImageData", func(call goja.FunctionCall) goja.Value {
		c := h.get(call)
		w, hh := coord(call.Argument(3)), coord(call.Argument(4))
		// Check the normalized size: getImageData flips negative extents.
		aw, ah := int64(w), int64(hh)
		if aw < 0 {
			aw = -aw
		}
		if ah < 0 {
			ah = -ah
		}
		if aw*ah > 64<<20 {
			panic(vm.NewGoError(fmt.Errorf("canvas: getImageData region too large")))
		}
		return vm.ToValue(vm.NewArrayBuffer(c.getImageData(coord(call.Argument(1)), coord(call.Argument(2)), w, hh)))
	})
	_ = host.Set("canvasPutImageData", func(call goja.FunctionCall) goja.Value {
		c := h.get(call)
		ab, ok := call.Argument(1).Export().(goja.ArrayBuffer)
		if !ok {
			panic(vm.NewTypeError("canvas: putImageData expects an ArrayBuffer"))
		}
		data := ab.Bytes()
		off, n := int(num(call.Argument(2))), int(num(call.Argument(3)))
		if off < 0 || n < 0 || off+n > len(data) {
			panic(vm.NewTypeError("canvas: putImageData buffer range out of bounds"))
		}
		c.putImageData(data[off:off+n], int(num(call.Argument(4))), int(num(call.Argument(5))), int(num(call.Argument(6))), int(num(call.Argument(7))))
		return goja.Undefined()
	})
	_ = host.Set("canvasClearRect", func(call goja.FunctionCall) goja.Value {
		h.get(call).clearRect(argMatrix(call, 1), num(call.Argument(7)), num(call.Argument(8)), num(call.Argument(9)), num(call.Argument(10)))
		return goja.Undefined()
	})
	_ = host.Set("canvasFillRect", func(call goja.FunctionCall) goja.Value {
		h.get(call).fillRect(argMatrix(call, 1), num(call.Argument(7)), num(call.Argument(8)), num(call.Argument(9)), num(call.Argument(10)), parseCSSColor(call.Argument(11).String()))
		return goja.Undefined()
	})
	// canvasFillText(id, a,b,c,d,e,f, text, font, color, align, baseline, strokeWidth)
	_ = host.Set("canvasFillText", func(call goja.FunctionCall) goja.Value {
		c := h.get(call)
		p := textParams{
			m:        argMatrix(call, 1),
			text:     call.Argument(7).String(),
			font:     h.rt.parseFont(call.Argument(8).String()),
			color:    parseCSSColor(call.Argument(9).String()),
			align:    call.Argument(10).String(),
			baseline: call.Argument(11).String(),
			dilate:   num(call.Argument(12)) / 2,
			measurer: h.rt.r.opts.measurer,
			faces:    h.faces,
		}
		if err := c.fillText(p); err != nil {
			panic(vm.NewGoError(err))
		}
		return goja.Undefined()
	})
	pathArg := func(call goja.FunctionCall) []subpath {
		ab, ok := call.Argument(1).Export().(goja.ArrayBuffer)
		if !ok {
			panic(vm.NewTypeError("canvas: path must be an ArrayBuffer"))
		}
		subs, err := decodePath(ab.Bytes())
		if err != nil {
			panic(vm.NewGoError(err))
		}
		return subs
	}
	// canvasFillPath(id, path, color, alpha)
	_ = host.Set("canvasFillPath", func(call goja.FunctionCall) goja.Value {
		c := h.get(call)
		c.fillPath(pathArg(call), withAlpha(parseCSSColor(call.Argument(2).String()), num(call.Argument(3))))
		return goja.Undefined()
	})
	// canvasStrokePath(id, path, color, alpha, lineWidth)
	_ = host.Set("canvasStrokePath", func(call goja.FunctionCall) goja.Value {
		c := h.get(call)
		c.strokePath(pathArg(call), withAlpha(parseCSSColor(call.Argument(2).String()), num(call.Argument(3))), num(call.Argument(4)))
		return goja.Undefined()
	})
	_ = host.Set("canvasToDataURL", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(h.get(call).toDataURL())
	})
}
