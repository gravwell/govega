// Copyright (c) 2026, Gravwell, Inc.
// All rights reserved.
//
// Use of this source code is governed by a BSD 2-Clause license that can be
// found in the LICENSE file.

package govega

import "time"

// LogLevel controls which Vega runtime messages are forwarded to the
// configured LogFunc.
type LogLevel string

const (
	LogNone  LogLevel = "none"
	LogError LogLevel = "error"
	LogWarn  LogLevel = "warn"
	LogInfo  LogLevel = "info"
	LogDebug LogLevel = "debug"
)

// LogFunc receives log messages emitted by the Vega runtime (for example
// "Infinite extent for field" warnings). level is the console method Vega
// used: "error", "warn", "info", "log" or "debug".
type LogFunc func(level string, msg string)

type options struct {
	poolSize int
	timeout  time.Duration
	loader   Loader
	log      LogFunc
	logLevel LogLevel
	measurer TextMeasurer

	failOnError bool
}

// Option configures a Renderer.
type Option func(*options)

// WithPoolSize sets the maximum number of idle JavaScript runtimes kept for
// reuse. Each runtime holds a loaded copy of Vega (tens of megabytes) and can
// serve one render at a time; when the pool is empty a new runtime is
// created on demand (about 25 ms) and discarded again if the pool is full.
// The default is runtime.NumCPU().
func WithPoolSize(n int) Option {
	return func(o *options) {
		if n > 0 {
			o.poolSize = n
		}
	}
}

// WithTimeout bounds the wall-clock time of a single render, independent of
// the caller's context. The default is 30 seconds; a negative value disables
// the built-in limit.
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithLoader enables loading of `url` data sources and image marks through
// the given Loader. See FSLoader and HTTPLoader.
func WithLoader(l Loader) Option {
	return func(o *options) { o.loader = l }
}

// WithLogger forwards Vega runtime log output to fn. Without a logger all
// output is discarded.
func WithLogger(fn LogFunc) Option {
	return func(o *options) { o.log = fn }
}

// WithLogLevel sets the Vega log level. The default is LogWarn.
func WithLogLevel(l LogLevel) Option {
	return func(o *options) { o.logLevel = l }
}

// WithTextMeasurer replaces the text width function used for layout. See
// DefaultTextMeasurer.
func WithTextMeasurer(m TextMeasurer) Option {
	return func(o *options) {
		if m != nil {
			o.measurer = m
		}
	}
}

// WithFailOnError controls what happens when Vega logs an error while
// evaluating the dataflow (an unknown projection type, a failing expression,
// an unsupported transform...). Vega itself only logs such errors and keeps
// rendering, typically producing an empty or partial chart. By default this
// package turns them into a render failure; pass false to get Vega's lenient
// behavior and receive the messages through WithLogger instead.
func WithFailOnError(fail bool) Option {
	return func(o *options) { o.failOnError = fail }
}
