/*************************************************************************
 * Copyright 2022 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package govega

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/dop251/goja"
	"github.com/dop251/goja/parser"
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

type VegaVM struct {
	sync.Mutex
	gvm *goja.Runtime
	res resp
	fn  func(string, string) string
}

// New creates a new VegaVM and loads the appropriate javascript files.
// VegaVMs are safe for concurrent use but are not parallel.
func New() (*VegaVM, error) {
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
		gvm: gvm,
		res: resp{
			done: make(chan error, 1),
		},
	}
	if err := vm.gvm.Set("set", vm.res.fill); err != nil {
		return nil, fmt.Errorf("failed to set fill function %w", err)
	} else if err = vm.gvm.Set("failure", vm.res.failure); err != nil {
		return nil, fmt.Errorf("failed to set fill function %w", err)
	} else if err = vm.gvm.ExportTo(vm.gvm.Get("makesvg"), &vm.fn); err != nil {
		return nil, fmt.Errorf("Failed to export stub makesvg function %w", err)
	} else if vm.fn == nil {
		return nil, fmt.Errorf("failed to get makesvg javascript function")
	}
	return vm, nil
}

// Render accepts a spe
func (vm *VegaVM) Render(spec []byte, data map[string]interface{}, ctx context.Context) (svg []byte, err error) {
	var res string
	var djson string
	if len(data) > 0 {
		d, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		djson = string(d)
	}
	vm.Lock()
	defer vm.Unlock()
	r := vm.fn(string(spec), djson)
	if r != `true` {
		err = errors.New(r)
		return
	}
	if res, err = vm.res.wait(ctx); err == nil {
		svg = []byte(res)
	}
	return
}

type resp struct {
	done chan error
	v    string
}

func (r *resp) fill(v interface{}) {
	if v == nil {
		r.done <- errors.New("nil")
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
		r.done <- errors.New("nil")
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
