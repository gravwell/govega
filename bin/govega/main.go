package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"github.com/gravwell/govega"
)

var (
	vegaSpecFilename = flag.String("spec", "-", "Path to vega specification file (or - for stdin)")
	dataFilename     = flag.String("data", "", "Optional data JSON file to load for vega")
	format           = flag.String("format", "svg", "Output image format (either svg or png)")
	outputFilename   = flag.String("output", "", "Path to output rendered spec (or omit for stdout)")
)

type renderFunc = func(specBytes []byte, data interface{}) (svg []byte, err error)

func main() {
	flag.Parse()

	specFile, err := getSpecFile()
	if err != nil {
		log.Fatal(err)
	}
	defer specFile.Close()

	outFile, err := getOutputFile()
	if err != nil {
		log.Fatal(err)
	}
	defer outFile.Close()

	var render renderFunc
	switch *format {
	case "svg":
		render = renderSvg
	case "png":
		render = renderPng
	default:
		log.Fatal("Unknown output format: ", *format)
	}

	data, err := parseDataFile()
	if err != nil {
		log.Fatal(err)
	}

	specBytes, err := ioutil.ReadAll(specFile)
	if err != nil {
		log.Fatal(err)
	}

	var dataobj interface{}
	if len(data) > 0 {
		dataobj = data
	}
	renderResult, err := render(specBytes, dataobj)
	if err != nil {
		log.Fatal(err)
	}

	if _, err := outFile.Write(renderResult); err != nil {
		log.Fatalf("Failed to write %q %v\n", *outputFilename, err)
	}
}

func getSpecFile() (inFile *os.File, err error) {
	switch *vegaSpecFilename {
	case "":
		err = fmt.Errorf("missing -vega-spec value")
	case "-":
		inFile = os.Stdin
	default:
		inFile, err = os.Open(*vegaSpecFilename)
		if err != nil {
			err = fmt.Errorf("Unable to open vega spec: %v", err)
		}
	}
	return
}

func getOutputFile() (outFile *os.File, err error) {
	outFile = os.Stdout
	if *outputFilename != `` {
		outFile, err = os.OpenFile(*outputFilename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0660)
		if err != nil {
			err = fmt.Errorf("Unable to open output file: %v", err)
		}
	}
	return
}

func parseDataFile() (dataObj json.RawMessage, err error) {
	if *dataFilename != `` {
		var dataBytes []byte
		dataBytes, err = ioutil.ReadFile(*dataFilename)
		if err != nil {
			err = fmt.Errorf("Unable to read data file: %v", err)
			return
		}

		err = json.Unmarshal(dataBytes, &dataObj)
		if err != nil {
			err = fmt.Errorf("Unable to parse data file as JSON: %v", err)
		}
	}
	return
}

func renderSvg(specBytes []byte, data interface{}) (svg []byte, err error) {
	var vm *govega.VegaVM
	vm, err = govega.New(govega.Config{})
	if err != nil {
		err = fmt.Errorf("Unable to create govega VM: %v", err)
		return
	}
	ctx := context.Background()
	svg, err = vm.RenderSVG(specBytes, data, ctx)
	if err != nil {
		err = fmt.Errorf("Unable to render vega spec as SVG: %v", err)
	}
	return
}

func renderPng(specBytes []byte, data interface{}) (png []byte, err error) {
	var vm *govega.VegaVM
	vm, err = govega.New(govega.Config{PNGResolution: govega.Resolution{Width: 1024, Height: 768}})
	if err != nil {
		err = fmt.Errorf("Unable to create govega VM: %v", err)
		return
	}
	ctx := context.Background()
	png, err = vm.RenderPNG(specBytes, data, ctx)
	if err != nil {
		err = fmt.Errorf("Unable to render vega spec as PNG: %v", err)
	}
	return
}
