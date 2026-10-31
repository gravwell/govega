// Copyright (c) 2026, Gravwell, Inc.
// All rights reserved.
//
// Use of this source code is governed by a BSD 2-Clause license that can be
// found in the LICENSE file.

// Package govega renders Vega visualization specifications to SVG in pure Go.
//
// The package embeds the upstream Vega JavaScript runtime and executes it
// with the goja interpreter, so it has no cgo dependency and does not require
// a browser or Node.js. Go supplies everything the headless runtime needs
// from its environment: text measurement (using embedded font metric
// tables), timers, logging and resource loading.
//
// Typical use:
//
//	r, err := vega.New()
//	...
//	svg, err := r.RenderSVG(ctx, specJSON, map[string]vega.Dataset{
//		"table": vega.NewDataset(
//			vega.Col("category", []string{"A", "B", "C"}),
//			vega.Col("amount", []float64{28, 55, 43}),
//		),
//	})
//
// Datasets are matched to the spec's data definitions by name: a matching
// definition has its values replaced (transforms are kept); an unmatched
// name is added as a new top-level data set.
//
// A Renderer is safe for concurrent use. Construct one and share it: it
// compiles the Vega bundle once and keeps a pool of JavaScript runtimes.
package govega

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	_ "embed"

	"github.com/dop251/goja"
	"github.com/gravwell/govega/internal/jsdist"
)

//go:embed pre.js
var preJS string

//go:embed post.js
var postJS string

// Version returns the embedded Vega version.
func Version() string { return jsdist.Version }

// LiteVersion returns the embedded Vega-Lite version.
func LiteVersion() string { return jsdist.LiteVersion }

// Mode selects how a spec is interpreted.
type Mode string

const (
	// ModeAuto detects Vega-Lite specs from their $schema URL, falling back to
	// the presence of Vega-Lite-only top-level properties such as `mark`.
	ModeAuto Mode = ""
	// ModeVega treats the spec as a Vega specification.
	ModeVega Mode = "vega"
	// ModeVegaLite compiles the spec with Vega-Lite before rendering.
	ModeVegaLite Mode = "vega-lite"
)

// Error is an error raised by the Vega runtime, for example an invalid spec
// or an expression that failed to evaluate.
type Error struct {
	Msg string
}

func (e *Error) Error() string { return "vega: " + e.Msg }

// Request describes one render.
type Request struct {
	// Spec is the Vega or Vega-Lite specification as JSON. Required.
	Spec []byte
	// Mode selects Vega or Vega-Lite interpretation; the default detects it.
	Mode Mode
	// Data supplies datasets by name; see the package documentation.
	Data map[string]Dataset
	// Signals sets initial signal values before rendering, overriding the
	// spec's `init`/`value`. Values are encoded with encoding/json.
	Signals map[string]any
	// Config is an optional Vega (or, for Vega-Lite specs, Vega-Lite) config
	// object (JSON) merged with the spec's own `config`, for theming.
	Config []byte
	// Locale is an optional JSON object {"number": ..., "time": ...} of
	// d3-format and d3-time-format locale definitions.
	Locale []byte
	// ScaleFactor scales the output; 0 means 1.
	ScaleFactor float64
	// Seed, when non-zero, seeds Vega's random number generator (used by
	// the random() expression function and the sample, wordcloud and force
	// transforms) so that the render is reproducible.
	Seed uint32
}

// Renderer renders Vega specs. Create one with New.
type Renderer struct {
	opts options

	pre, bundle *goja.Program
	proj, lite  *goja.Program
	post        *goja.Program

	pool chan *jsRuntime
}

var (
	sharedOnce   sync.Once
	sharedPre    *goja.Program
	sharedBundle *goja.Program
	sharedProj   *goja.Program
	sharedLite   *goja.Program
	sharedPost   *goja.Program
	sharedErr    error

	sharedMissingPatches []string
)

// New creates a Renderer. Compilation of the JavaScript bundles (about 150 ms) is
// shared between all Renderers in the process and happens on first use.
func New(opts ...Option) (*Renderer, error) {
	o := options{
		poolSize: runtime.NumCPU(),
		timeout:  30 * time.Second,
		logLevel: LogWarn,
		measurer: DefaultTextMeasurer,

		failOnError: true,
	}
	for _, opt := range opts {
		opt(&o)
	}
	r := &Renderer{opts: o, pool: make(chan *jsRuntime, o.poolSize)}
	if err := r.compile(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Renderer) compile() error {
	sharedOnce.Do(func() {
		sharedPre, sharedErr = goja.Compile("govega-pre.js", preJS, true)
		if sharedErr != nil {
			return
		}
		patched, missing := applyPatches(jsdist.VegaJS, vegaPatches)
		sharedMissingPatches = missing
		sharedBundle, sharedErr = goja.Compile("vega.min.js", patched, true)
		if sharedErr != nil {
			return
		}
		sharedProj, sharedErr = goja.Compile("vega-projections.min.js", jsdist.ProjectionsJS, true)
		if sharedErr != nil {
			return
		}
		sharedLite, sharedErr = goja.Compile("vega-lite.min.js", jsdist.VegaLiteJS, true)
		if sharedErr != nil {
			return
		}
		sharedPost, sharedErr = goja.Compile("govega-post.js", postJS, true)
	})
	if sharedErr != nil {
		return fmt.Errorf("vega: compiling runtime: %w", sharedErr)
	}
	r.pre, r.bundle, r.proj, r.lite, r.post = sharedPre, sharedBundle, sharedProj, sharedLite, sharedPost
	return nil
}

// RenderSVG renders spec with the given datasets and returns the SVG
// document. data may be nil when the spec carries its own data.
func (r *Renderer) RenderSVG(ctx context.Context, spec []byte, data map[string]Dataset) ([]byte, error) {
	return r.Render(ctx, Request{Spec: spec, Data: data})
}

// Render renders a Request and returns the SVG document.
func (r *Renderer) Render(ctx context.Context, req Request) (svg []byte, err error) {
	if len(strings.TrimSpace(string(req.Spec))) == 0 {
		return nil, &Error{Msg: "empty spec"}
	}
	dataJSON, err := encodeDatasets(req.Data)
	if err != nil {
		return nil, err
	}
	optsJSON, err := encodeRenderOptions(r.opts, req)
	if err != nil {
		return nil, err
	}
	if r.opts.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.opts.timeout)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	rt, err := r.acquire()
	if err != nil {
		return nil, err
	}
	healthy := false
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("vega: panic during render: %v", p)
			healthy = false
		}
		if healthy {
			r.release(rt)
		}
	}()

	svg, err = rt.render(ctx, string(req.Spec), string(dataJSON), string(optsJSON))
	if err != nil {
		var ierr *goja.InterruptedError
		if errors.As(err, &ierr) || ctx.Err() != nil {
			// The runtime may be in an inconsistent state; do not reuse it.
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}
		// A render that stopped with JavaScript work pending is not reused.
		healthy = !rt.broken
		return nil, err
	}
	healthy = !rt.broken
	return svg, nil
}

// validObject reports whether b is a single, valid JSON object, so that it
// can be spliced into the options document without changing its structure.
func validObject(b []byte) bool {
	t := bytes.TrimSpace(b)
	return len(t) > 0 && t[0] == '{' && json.Valid(t)
}

func encodeRenderOptions(o options, req Request) ([]byte, error) {
	var b bytes.Buffer
	// failOnError relies on Vega logging errors, so LogNone still runs Vega
	// at error level; runtime.log keeps those messages from the caller.
	level := o.logLevel
	if level == LogNone && o.failOnError {
		level = LogError
	}
	b.WriteString(`{"logLevel":`)
	writeJSONString(&b, string(level))
	switch req.Mode {
	case ModeAuto:
	case ModeVega, ModeVegaLite:
		b.WriteString(`,"mode":`)
		writeJSONString(&b, string(req.Mode))
	default:
		return nil, fmt.Errorf("vega: unknown mode %q", req.Mode)
	}
	if req.ScaleFactor > 0 {
		fmt.Fprintf(&b, `,"scaleFactor":%g`, req.ScaleFactor)
	}
	if req.Seed != 0 {
		fmt.Fprintf(&b, `,"seed":%d`, req.Seed)
	}
	if len(req.Config) > 0 {
		if !validObject(req.Config) {
			return nil, errors.New("vega: Request.Config is not a valid JSON object")
		}
		b.WriteString(`,"config":`)
		b.Write(req.Config)
	}
	if len(req.Locale) > 0 {
		if !validObject(req.Locale) {
			return nil, errors.New("vega: Request.Locale is not a valid JSON object")
		}
		b.WriteString(`,"locale":`)
		b.Write(req.Locale)
	}
	if len(req.Signals) > 0 {
		names := make([]string, 0, len(req.Signals))
		for n := range req.Signals {
			names = append(names, n)
		}
		sort.Strings(names)
		b.WriteString(`,"signals":{`)
		for i, n := range names {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSONString(&b, n)
			b.WriteByte(':')
			v, err := jsonMarshal(req.Signals[n])
			if err != nil {
				return nil, fmt.Errorf("vega: signal %q: %w", n, err)
			}
			b.Write(v)
		}
		b.WriteByte('}')
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func (r *Renderer) acquire() (*jsRuntime, error) {
	select {
	case rt := <-r.pool:
		return rt, nil
	default:
	}
	return newJSRuntime(r)
}

func (r *Renderer) release(rt *jsRuntime) {
	rt.reset()
	select {
	case r.pool <- rt:
	default:
		// Pool is full; let the runtime be garbage collected.
	}
}
