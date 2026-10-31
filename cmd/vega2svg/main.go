// Copyright (c) 2026, Gravwell, Inc.
// All rights reserved.
//
// Use of this source code is governed by a BSD 2-Clause license that can be
// found in the LICENSE file.

// Command vega2svg renders a Vega specification to SVG.
//
//	vega2svg [-o out.svg] [-mode vega|vega-lite] [-data name=file.csv ...]
//	         [-signal name=json ...] [-config config.json] [-root dir]
//	         [-allow-http] spec.json
//
// The spec may be Vega or Vega-Lite; by default the kind is detected from
// its $schema. Datasets given with -data are parsed from CSV (header row required) and
// injected into the spec by name; numeric-looking cells become numbers and
// RFC 3339 timestamps become dates. With -root, `url` references in the spec
// are resolved relative to that directory; -allow-http additionally permits
// http(s) URLs.
package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gravwell/govega"
)

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

func main() {
	var (
		out       = flag.String("o", "", "output file (default stdout)")
		root      = flag.String("root", "", "directory that `url` data references are resolved against")
		allowHTTP = flag.Bool("allow-http", false, "allow the spec to load http(s) URLs")
		config    = flag.String("config", "", "Vega (or Vega-Lite) config JSON file")
		mode      = flag.String("mode", "", "spec kind: vega or vega-lite (default: detect from $schema)")
		scale     = flag.Float64("scale", 1, "scale factor")
		timeout   = flag.Duration("timeout", 30*time.Second, "render timeout")
		verbose   = flag.Bool("v", false, "print Vega log output to stderr")
		datasets  stringList
		signals   stringList
	)
	flag.Var(&datasets, "data", "dataset as name=file.csv (repeatable)")
	flag.Var(&signals, "signal", "initial signal value as name=JSON (repeatable)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: vega2svg [flags] spec.json\n\nflags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(flag.Arg(0), *out, *root, *allowHTTP, *config, govega.Mode(*mode), *scale, *timeout, *verbose, datasets, signals); err != nil {
		fmt.Fprintln(os.Stderr, "vega2svg:", err)
		os.Exit(1)
	}
}

func run(specPath, out, root string, allowHTTP bool, configPath string, mode govega.Mode, scale float64, timeout time.Duration, verbose bool, datasets, signals []string) error {
	spec, err := readInput(specPath)
	if err != nil {
		return err
	}
	var opts []govega.Option
	var loaders []govega.Loader
	if root != "" {
		loaders = append(loaders, govega.FSLoader(os.DirFS(root)))
	}
	if allowHTTP {
		loaders = append(loaders, govega.HTTPLoader(nil, 0))
	}
	if len(loaders) > 0 {
		opts = append(opts, govega.WithLoader(govega.MultiLoader(loaders...)))
	}
	if verbose {
		opts = append(opts, govega.WithLogger(func(level, msg string) {
			fmt.Fprintf(os.Stderr, "[vega %s] %s\n", level, msg)
		}), govega.WithLogLevel(govega.LogInfo))
	}
	opts = append(opts, govega.WithTimeout(timeout), govega.WithPoolSize(1))

	r, err := govega.New(opts...)
	if err != nil {
		return err
	}

	req := govega.Request{Spec: spec, Mode: mode, ScaleFactor: scale, Data: map[string]govega.Dataset{}, Signals: map[string]any{}}
	for _, d := range datasets {
		name, path, ok := strings.Cut(d, "=")
		if !ok {
			return fmt.Errorf("-data %q: expected name=file.csv", d)
		}
		ds, err := readCSV(path)
		if err != nil {
			return fmt.Errorf("-data %s: %w", name, err)
		}
		req.Data[name] = ds
	}
	for _, s := range signals {
		name, raw, ok := strings.Cut(s, "=")
		if !ok {
			return fmt.Errorf("-signal %q: expected name=JSON", s)
		}
		var v any
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			v = raw // treat unparseable values as strings
		}
		req.Signals[name] = v
	}
	if configPath != "" {
		if req.Config, err = os.ReadFile(configPath); err != nil {
			return err
		}
	}

	svg, err := r.Render(context.Background(), req)
	if err != nil {
		return err
	}
	if out == "" || out == "-" {
		_, err = os.Stdout.Write(svg)
		return err
	}
	return os.WriteFile(out, svg, 0o644)
}

func readInput(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

// readCSV parses a CSV file with a header row into a column-oriented
// dataset, inferring number and timestamp types per column.
func readCSV(path string) (govega.Dataset, error) {
	f, err := os.Open(path)
	if err != nil {
		return govega.Dataset{}, err
	}
	defer f.Close()
	rd := csv.NewReader(f)
	rd.ReuseRecord = false
	records, err := rd.ReadAll()
	if err != nil {
		return govega.Dataset{}, err
	}
	if len(records) == 0 {
		return govega.Dataset{}, fmt.Errorf("empty CSV")
	}
	header := records[0]
	rows := records[1:]
	ds := govega.Dataset{Columns: make([]govega.Column, len(header))}
	for c, name := range header {
		ds.Columns[c] = govega.Column{Name: name, Values: inferColumn(rows, c)}
	}
	return ds, nil
}

func inferColumn(rows [][]string, c int) any {
	raw := make([]string, len(rows))
	for i, r := range rows {
		if c < len(r) {
			raw[i] = r[c]
		}
	}
	// Numbers?
	nums := make([]any, len(raw))
	isNum := true
	for i, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			nums[i] = nil
			continue
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			isNum = false
			break
		}
		nums[i] = v
	}
	if isNum {
		return nums
	}
	// Timestamps?
	times := make([]any, len(raw))
	isTime := true
	for i, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			times[i] = nil
			continue
		}
		t, err := parseTime(s)
		if err != nil {
			isTime = false
			break
		}
		times[i] = t
	}
	if isTime {
		return times
	}
	return raw
}

func parseTime(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("not a timestamp: %q", s)
}
