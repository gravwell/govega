/*************************************************************************
 * Copyright 2022 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package govega

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"

	"image/png"

	"github.com/dop251/goja"
	"github.com/dop251/goja/parser"
)

const (
	defaultWidth  = 800
	defaultHeight = 600
)

//go:embed "assets/polyfill.min.js"
//go:embed "assets/runtime.min.js"
//go:embed "assets/vega.min.js"
//go:embed "assets/index.js"
var js embed.FS

var jsfiles = []string{
	"assets/polyfill.min.js",
	"assets/runtime.min.js",
	"assets/vega.min.js",
	"assets/index.js",
}

type Resolution struct {
	Width  int
	Height int
}

type Config struct {
	Logger        func(...interface{})
	PNGResolution Resolution //not used in SVG rendering
}

type VegaVM struct {
	sync.Mutex
	Config
	gvm *goja.Runtime
	res resp
	fn  func(string, string, int, int) string
}

// New creates a new VegaVM and loads the appropriate javascript files.
// VegaVMs are safe for concurrent use but are not parallel.
func New(c Config) (*VegaVM, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	gvm := goja.New()
	for _, s := range jsfiles {
		bb, err := js.ReadFile(s)
		if err != nil {
			return nil, fmt.Errorf("Failed to open embedded file %q %w", s, err)
		}
		if p, err := goja.Parse(s, string(bb), parser.WithDisableSourceMaps); err != nil {
			return nil, fmt.Errorf("Failed to parse embedded Vega JS file %q %w", s, err)
		} else if prgm, err := goja.CompileAST(p, true); err != nil {
			return nil, fmt.Errorf("Failed to compile embedded Vega JS file %q %w", s, err)
		} else if _, err := gvm.RunProgram(prgm); err != nil {
			return nil, fmt.Errorf("Failed to execute embedded Vega JS file %q %w", s, err)
		}
	}

	vm := &VegaVM{
		Config: c,
		gvm:    gvm,
		res: resp{
			done: make(chan error, 1),
		},
	}

	if err := vm.gvm.Set("log", c.Logger); err != nil {
		return nil, fmt.Errorf("failed to set log function %w", err)
	}
	var con console
	if err := vm.gvm.Set("console", con); err != nil {
		return nil, fmt.Errorf("failed to set console object %w", err)
	}

	if err := vm.gvm.Set("success", vm.res.success); err != nil {
		return nil, fmt.Errorf("failed to set success function %w", err)
	}

	if err := vm.gvm.Set("failure", vm.res.failure); err != nil {
		return nil, fmt.Errorf("failed to set failure function %w", err)
	}

	if err := vm.gvm.ExportTo(vm.gvm.Get("makesvg"), &vm.fn); err != nil {
		return nil, fmt.Errorf("Failed to export stub makesvg function %w", err)
	}

	if vm.fn == nil {
		return nil, fmt.Errorf("failed to get makesvg javascript function")
	}

	return vm, nil
}

// RenderSVG accepts a spe
func (vm *VegaVM) RenderSVG(spec []byte, data interface{}, ctx context.Context) (svg []byte, err error) {
	var res string
	var djson string
	if data != nil {
		d, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		djson = string(d)
	}

	// Nil canvas context implies we want an SVG
	if err := vm.gvm.Set("cxt", nil); err != nil {
		return nil, fmt.Errorf("failed to set ctx object %w", err)
	}

	vm.Lock()
	defer vm.Unlock()
	r := vm.fn(string(spec), djson, 0, 0)
	if r != `true` {
		err = errors.New(r)
		return
	}
	if res, err = vm.res.wait(ctx); err == nil {
		svg = []byte(res)
	}
	return
}

func (vm *VegaVM) RenderPNG(spec []byte, data interface{}, ctx context.Context) ([]byte, error) {
	var djson string
	if data != nil {
		d, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		djson = string(d)
	}
	vm.Lock()
	defer vm.Unlock()

	vm.gvm.SetFieldNameMapper(goja.UncapFieldNameMapper())

	c, err := mkCanvas(vm.PNGResolution.Width, vm.PNGResolution.Height)
	if err != nil {
		return nil, fmt.Errorf("Failed to create canvas %w", err)
	} else if err = initCanvas(vm.gvm, c); err != nil {
		return nil, err
	}

	width, height := vm.Config.PNGResolution.Width, vm.Config.PNGResolution.Height
	r := vm.fn(string(spec), djson, width, height)
	if r != `true` {
		return nil, fmt.Errorf(`Expected "true" as return value. Got: %+v`, r)
	}

	// Response will be nil
	if _, err = vm.res.wait(ctx); err != nil {
		return nil, fmt.Errorf(`Failed to wait for response "true" as return value. Got: %+v`, err)
	}

	img := c.GetImageData(0, 0, c.Canvas.Width(), c.Canvas.Height())
	w := new(bytes.Buffer)
	if err := png.Encode(w, img); err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}

type resp struct {
	done chan error
	v    string
}

func (r *resp) success(v interface{}) {
	if v == nil {
		r.done <- nil
		return
	} else if val, ok := v.(string); !ok {
		r.done <- fmt.Errorf("invalid response type %T", v)
	} else {
		r.v = val
		r.done <- nil
	}
	return
}

func (r *resp) failure(v interface{}) {
	if v == nil {
		r.done <- errors.New("Failure message is nil")
		return
	} else if val, ok := v.(string); !ok {
		r.done <- fmt.Errorf("invalid response type %T", v)
	} else {
		r.done <- errors.New(val)
	}
	return
}

func (r *resp) wait(ctx context.Context) (res string, err error) {
	select {
	case <-ctx.Done():
		err = ctx.Err()
	case err = <-r.done:
		res = r.v
	}
	return
}

func (c *Config) Validate() (err error) {
	if c == nil {
		return errors.New("nil config")
	}

	if c.Logger == nil {
		//set the default
		c.Logger = log.Println
	}

	if c.PNGResolution.IsZero() {
		c.PNGResolution.Width = defaultWidth
		c.PNGResolution.Height = defaultHeight
	} else if err = c.PNGResolution.Validate(); err != nil {
		return
	}

	return
}

func (r Resolution) Validate() (err error) {
	if r.Width <= 0 {
		err = errors.New("width is less than zero")
	} else if r.Height <= 0 {
		err = errors.New("height is less than zero")
	}
	return
}

func (r Resolution) IsZero() bool {
	//if either is, it's worthless, consider it zero
	return r.Width == 0 || r.Height == 0
}

type console struct {
	logger func(...interface{})
}

func (c console) Log(vals ...interface{}) {
	c.logger(vals...)
}
