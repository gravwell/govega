// Copyright (c) 2026, Gravwell, Inc.
// All rights reserved.
//
// Use of this source code is governed by a BSD 2-Clause license that can be
// found in the LICENSE file.

package govega

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dop251/goja"
)

// jsRuntime is one goja VM with Vega loaded. It is not safe for concurrent
// use; Renderer hands each one to a single render at a time.
type jsRuntime struct {
	r  *Renderer
	vm *goja.Runtime

	renderFn goja.Callable

	// Per-render state.
	ctx context.Context

	// broken is set when a render ends with JavaScript work still pending
	// (an unfinished View, unresolved promises). Such a runtime is discarded
	// instead of being returned to the pool.
	broken bool

	// Loads started by the current render and not yet delivered, and the
	// channel their results arrive on. stop is closed when the render ends so
	// that late loader goroutines drop their results instead of blocking.
	pendingLoads int
	loads        chan loadResult
	stop         chan struct{}

	// Timers registered through setTimeout/setInterval.
	timers   []*jsTimer
	timerSeq int

	// Errors Vega logged at error level during the current render. Vega only
	// logs dataflow errors and continues with an empty view, so these are
	// surfaced as render failures unless the Renderer is configured otherwise.
	loggedErrors []string

	canvas    *canvasHost
	fontCache map[string]Font
}

type loadResult struct {
	data    []byte
	err     error
	resolve goja.Callable
	reject  goja.Callable
}

type jsTimer struct {
	id       int
	fn       goja.Callable
	args     []goja.Value
	due      time.Time
	interval time.Duration
	repeat   bool
}

func newJSRuntime(r *Renderer) (*jsRuntime, error) {
	rt := &jsRuntime{r: r, vm: goja.New(), fontCache: map[string]Font{}}
	rt.canvas = newCanvasHost(rt)
	vm := rt.vm

	host := vm.NewObject()
	_ = host.Set("measureText", rt.hostMeasureText)
	_ = host.Set("loadStart", rt.hostLoadStart)
	rt.canvas.install(host)
	_ = vm.Set("__govega_host", host)

	console := vm.NewObject()
	for _, level := range []string{"log", "info", "warn", "error", "debug"} {
		_ = console.Set(level, func(call goja.FunctionCall) goja.Value {
			rt.log(level, call.Arguments)
			return goja.Undefined()
		})
	}
	_ = vm.Set("console", console)

	_ = vm.Set("setTimeout", rt.setTimer(false))
	_ = vm.Set("setInterval", rt.setTimer(true))
	_ = vm.Set("clearTimeout", rt.clearTimer)
	_ = vm.Set("clearInterval", rt.clearTimer)
	_ = vm.Set("requestAnimationFrame", func(call goja.FunctionCall) goja.Value {
		fn, ok := goja.AssertFunction(call.Argument(0))
		if !ok {
			panic(vm.NewTypeError("requestAnimationFrame: callback is not a function"))
		}
		return vm.ToValue(rt.addTimer(fn, nil, 16*time.Millisecond, false))
	})
	_ = vm.Set("cancelAnimationFrame", rt.clearTimer)

	for _, p := range []*goja.Program{r.pre, r.bundle, r.proj, r.lite, r.post} {
		if _, err := vm.RunProgram(p); err != nil {
			return nil, fmt.Errorf("vega: initializing runtime: %w", err)
		}
	}
	v, err := vm.RunString("__govega.render")
	if err != nil {
		return nil, fmt.Errorf("vega: initializing runtime: %w", err)
	}
	fn, ok := goja.AssertFunction(v)
	if !ok {
		return nil, errors.New("vega: initializing runtime: __govega.render is not a function")
	}
	rt.renderFn = fn
	rt.canvas.markBase()
	return rt, nil
}

// reset clears per-render state so the runtime can be pooled.
func (rt *jsRuntime) reset() {
	rt.ctx = nil
	rt.pendingLoads = 0
	rt.loads = nil
	rt.stop = nil
	rt.timers = nil
	rt.loggedErrors = nil
	rt.canvas.reset()
	rt.vm.ClearInterrupt()
	if len(rt.fontCache) > 4096 {
		rt.fontCache = map[string]Font{}
	}
}

func (rt *jsRuntime) render(ctx context.Context, spec, data, opts string) ([]byte, error) {
	rt.ctx = ctx
	rt.timers = rt.timers[:0]
	rt.loggedErrors = rt.loggedErrors[:0]
	rt.pendingLoads = 0
	rt.loads = make(chan loadResult)

	// Interrupt the VM if the context ends while JavaScript is running. The
	// watcher must have exited before we return, otherwise a late Interrupt
	// could hit the next render on this runtime; reset() then clears any
	// interrupt that landed after JavaScript had already finished.
	done := make(chan struct{})
	rt.stop = done
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		select {
		case <-ctx.Done():
			rt.vm.Interrupt(ctx.Err())
		case <-done:
		}
	}()
	defer func() {
		close(done)
		<-exited
	}()

	vm := rt.vm
	resultV, err := rt.renderFn(goja.Undefined(), vm.ToValue(spec), vm.ToValue(data), vm.ToValue(opts))
	if err != nil {
		return nil, wrapJSError(err)
	}
	result, ok := resultV.(*goja.Object)
	if !ok {
		return nil, errors.New("vega: render returned no result object")
	}

	// Promise reactions run when control returns to Go. Keep delivering load
	// results and firing timers until the render settles. Returning before
	// that leaves JavaScript work pending, so the runtime is marked broken.
	for !result.Get("done").ToBoolean() {
		if err := rt.step(ctx); err != nil {
			rt.broken = true
			return nil, err
		}
	}
	if errV := result.Get("error"); errV != nil && !goja.IsNull(errV) && !goja.IsUndefined(errV) {
		return nil, &Error{Msg: errV.String()}
	}
	if le, ok := result.Get("loadErrors").Export().([]any); ok && len(le) > 0 {
		msgs := make([]string, len(le))
		for i, m := range le {
			msgs[i] = fmt.Sprint(m)
		}
		return nil, &Error{Msg: strings.Join(msgs, "; ")}
	}
	if rt.r.opts.failOnError && len(rt.loggedErrors) > 0 {
		return nil, &Error{Msg: strings.Join(rt.loggedErrors, "; ")}
	}
	svg := result.Get("svg")
	if svg == nil || goja.IsNull(svg) || goja.IsUndefined(svg) {
		return nil, errors.New("vega: render produced no SVG")
	}
	return []byte(svg.String()), nil
}

func wrapJSError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*goja.InterruptedError](err); ok {
		return err
	}
	if exc, ok := errors.AsType[*goja.Exception](err); ok {
		msg := exc.Value().String()
		if obj, ok := exc.Value().(*goja.Object); ok {
			if m := obj.Get("message"); m != nil && !goja.IsUndefined(m) {
				msg = m.String()
			}
		}
		return &Error{Msg: msg}
	}
	return fmt.Errorf("vega: %w", err)
}

// --- host functions ------------------------------------------------------

func (rt *jsRuntime) hostMeasureText(call goja.FunctionCall) goja.Value {
	text := call.Argument(0).String()
	font := rt.parseFont(call.Argument(1).String())
	return rt.vm.ToValue(rt.r.opts.measurer(text, font))
}

// parseFont parses a CSS font string, caching results per runtime since the
// same few font strings recur for every label.
func (rt *jsRuntime) parseFont(fontStr string) Font {
	font, ok := rt.fontCache[fontStr]
	if !ok {
		font = ParseCSSFont(fontStr)
		if len(rt.fontCache) > 4096 {
			rt.fontCache = map[string]Font{}
		}
		rt.fontCache[fontStr] = font
	}
	return font
}

// hostLoadStart(uri, resolve, reject) starts loading uri on a goroutine so
// that a spec's data sources are fetched concurrently. The result is
// delivered back on the VM goroutine by step, which calls resolve with the
// text or reject with an error message.
func (rt *jsRuntime) hostLoadStart(call goja.FunctionCall) goja.Value {
	uri := call.Argument(0).String()
	resolve, ok1 := goja.AssertFunction(call.Argument(1))
	reject, ok2 := goja.AssertFunction(call.Argument(2))
	if !ok1 || !ok2 {
		panic(rt.vm.NewTypeError("loadStart: resolve and reject must be functions"))
	}
	loader := rt.r.opts.loader
	if loader == nil {
		msg := fmt.Sprintf("%s (url: %s)", strings.TrimPrefix(ErrLoadingDisabled.Error(), "vega: "), uri)
		if _, err := reject(goja.Undefined(), rt.vm.ToValue(msg)); err != nil {
			panic(err)
		}
		return goja.Undefined()
	}
	ctx, loads, stop := rt.ctx, rt.loads, rt.stop
	if ctx == nil {
		ctx = context.Background()
	}
	rt.pendingLoads++
	go func() {
		b, err := loader.Load(ctx, uri)
		if err != nil {
			err = fmt.Errorf("loading %s: %w", uri, err)
		}
		select {
		case loads <- loadResult{data: b, err: err, resolve: resolve, reject: reject}:
		case <-stop:
		}
	}()
	return goja.Undefined()
}

// step waits for the next load result or due timer and runs its callback.
func (rt *jsRuntime) step(ctx context.Context) error {
	if len(rt.timers) == 0 && rt.pendingLoads == 0 {
		return errors.New("vega: render did not complete (pending asynchronous work without timers)")
	}
	var timerC <-chan time.Time
	next := -1
	if len(rt.timers) > 0 {
		next = 0
		for i, t := range rt.timers {
			if t.due.Before(rt.timers[next].due) {
				next = i
			}
		}
		wait := time.Until(rt.timers[next].due)
		if wait <= 0 && rt.pendingLoads == 0 {
			return wrapJSError(rt.fireTimer(next))
		}
		timer := time.NewTimer(max(wait, 0))
		defer timer.Stop()
		timerC = timer.C
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case res := <-rt.loads:
		rt.pendingLoads--
		var err error
		if res.err != nil {
			_, err = res.reject(goja.Undefined(), rt.vm.ToValue(res.err.Error()))
		} else {
			_, err = res.resolve(goja.Undefined(), rt.vm.ToValue(string(res.data)))
		}
		return wrapJSError(err)
	case <-timerC:
		return wrapJSError(rt.fireTimer(next))
	}
}

func (rt *jsRuntime) log(level string, args []goja.Value) {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = a.String()
	}
	msg := strings.Join(parts, " ")
	if level == "error" {
		rt.loggedErrors = append(rt.loggedErrors, strings.TrimPrefix(msg, "ERROR "))
	}
	// With LogNone, Vega still logs errors so that failOnError works (see
	// encodeRenderOptions); they are not forwarded to the caller.
	if fn := rt.r.opts.log; fn != nil && rt.r.opts.logLevel != LogNone {
		fn(level, msg)
	}
}

// --- timers ----------------------------------------------------------------

func (rt *jsRuntime) setTimer(repeat bool) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		fn, ok := goja.AssertFunction(call.Argument(0))
		if !ok {
			panic(rt.vm.NewTypeError("timer callback is not a function"))
		}
		delay := max(time.Duration(call.Argument(1).ToFloat()*float64(time.Millisecond)), 0)
		var args []goja.Value
		if len(call.Arguments) > 2 {
			args = append(args, call.Arguments[2:]...)
		}
		return rt.vm.ToValue(rt.addTimer(fn, args, delay, repeat))
	}
}

func (rt *jsRuntime) addTimer(fn goja.Callable, args []goja.Value, delay time.Duration, repeat bool) int {
	rt.timerSeq++
	t := &jsTimer{id: rt.timerSeq, fn: fn, args: args, due: time.Now().Add(delay), interval: delay, repeat: repeat}
	rt.timers = append(rt.timers, t)
	return t.id
}

func (rt *jsRuntime) clearTimer(call goja.FunctionCall) goja.Value {
	id := int(call.Argument(0).ToInteger())
	for i, t := range rt.timers {
		if t.id == id {
			rt.timers = append(rt.timers[:i], rt.timers[i+1:]...)
			break
		}
	}
	return goja.Undefined()
}

// fireTimer runs the timer at index best, rescheduling it if it repeats.
func (rt *jsRuntime) fireTimer(best int) error {
	t := rt.timers[best]
	if t.repeat {
		t.due = t.due.Add(t.interval)
		if t.interval <= 0 {
			t.due = time.Now().Add(time.Millisecond)
		}
	} else {
		rt.timers = append(rt.timers[:best], rt.timers[best+1:]...)
	}
	_, err := t.fn(goja.Undefined(), t.args...)
	return err
}
