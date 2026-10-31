// Copyright (c) 2026, Gravwell, Inc.
// All rights reserved.
//
// Use of this source code is governed by a BSD 2-Clause license that can be
// found in the LICENSE file.

// Loader renders a spec that refers to its data by URL. URL loading is off by
// default; this example enables it for an embedded file system, so the spec
// can be used unchanged from the Vega editor while the data ships inside
// the binary. HTTPLoader works the same way for http(s) URLs.
//
//	go run ./examples/loader
package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"os"
	"time"

	"github.com/gravwell/govega"
)

//go:embed assets
var assets embed.FS

func main() {
	spec, err := assets.ReadFile("assets/population.vg.json")
	if err != nil {
		log.Fatal(err)
	}

	// Every "url" in the spec is resolved against the embedded file system,
	// so "data/population.csv" reads assets/data/population.csv. Vega parses
	// the CSV itself using the spec's format directives.
	root, err := fs.Sub(assets, "assets")
	if err != nil {
		log.Fatal(err)
	}
	r, err := govega.New(govega.WithLoader(govega.FSLoader(root)))
	if err != nil {
		log.Fatal(err)
	}
	start := time.Now()
	svg, err := r.RenderSVG(context.Background(), spec, nil)
	if err != nil {
		log.Fatal(err)
	}
	elapsed := time.Since(start).Round(time.Millisecond)
	if err := os.WriteFile("population.svg", svg, 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wrote population.svg (%d bytes) in %s\n", len(svg), elapsed)
}
