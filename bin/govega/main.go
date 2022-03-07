package main

import (
	"context"
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"

	"github.com/gravwell/govega"
)

var (
	vegaSpec = flag.String("vega-spec", "", "Path to vega specification file")
	data     = flag.String("data", "", "Optional data JSON file to load for vega")
	svgOut   = flag.String("output", "", "Path to output SVG file")
)

func main() {
	flag.Parse()
	if *vegaSpec == `` {
		log.Fatal("missing -vega-spec value")
	} else if *svgOut == `` {
		log.Fatal("missing -output value")
	}
	specBytes, err := ioutil.ReadFile(*vegaSpec)
	if err != nil {
		log.Fatal("Failed to load spec file", *vegaSpec, err)
		return
	}
	var dataObj map[string]interface{}
	if *data != `` {
		dataBytes, err := ioutil.ReadFile(*data)
		if err != nil {
			log.Fatal("Failed to load datafile")
		}
		if err := json.Unmarshal(dataBytes, &dataObj); err != nil {
			log.Fatal("Failed to parse datafile")
		}
	}
	vm, err := govega.New()
	if err != nil {
		log.Fatal("Failed to build govega VM", err)
	}
	ctx := context.Background()
	svg, err := vm.Render(specBytes, dataObj, ctx)
	if err != nil {
		log.Fatalf("Failed to render %q %v\n", *vegaSpec, err)
	}
	if err := ioutil.WriteFile(*svgOut, svg, 0660); err != nil {
		log.Fatalf("Failed to write %q %v\n", *svgOut, err)
	}
}
