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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const dataFileName = `data.json`
const badSpec = `examples/garbage.json`

var testFiles = []string{
	`examples/bar-chart.vg.json`,
	`examples/grouped-bar-chart.vg.json`,
	`examples/county-unemployment/county-unemployment.vg.json`,
	`examples/job-voyager/job-voyager.vg.json`,
	`examples/u-district-cuisine/u-district-cuisine.vg.json`,
}

func TestNew(t *testing.T) {
	if _, err := New(); err != nil {
		t.Error(err)
	}
}

func TestBadSpec(t *testing.T) {
	spec, data, err := loadSpecAndData(badSpec)
	if err != nil {
		t.Fatal(err)
	}
	vm, err := New()
	if err != nil {
		t.Error(err)
	}
	ctx, cf := context.WithTimeout(context.Background(), time.Second)
	defer cf()
	if svg, err := vm.Render(spec, data, ctx); err == nil {
		t.Fatal(err)
	} else if svg != nil {
		t.Fatal("returned SVG is not nil after bad spec")
	}

	//make sure the context isn't what fired
	if err = ctx.Err(); err != nil {
		t.Fatal("Context error fired", err)
	}
}

func TestGoodSpecs(t *testing.T) {
	vm, err := New()
	if err != nil {
		t.Error(err)
	}
	for _, s := range testFiles {
		spec, data, err := loadSpecAndData(s)
		if err != nil {
			t.Fatalf("Failed to load %q %v", s, err)
		}
		ts := time.Now()
		//some of these can take some time, make the timeout large so tests don't fail on slow machines
		ctx, cf := context.WithTimeout(context.Background(), 30*time.Second)
		if svg, err := vm.Render(spec, data, ctx); err != nil {
			t.Fatalf("Failed to render %q - %v", s, err)
		} else if svg == nil {
			t.Fatal("returned SVG is nil")
		} else if !bytes.HasPrefix(svg, []byte(`<svg`)) {
			t.Fatal("returned value not an SVG?")
		}
		cf()
		if testing.Verbose() {
			fmt.Printf("%q took %v\n", s, time.Since(ts))
		}
	}
}

func loadSpecAndData(sp string) (spec []byte, data map[string]interface{}, err error) {
	if spec, err = ioutil.ReadFile(sp); err != nil {
		return
	}
	//check if there is a data file
	datafile := filepath.Join(filepath.Dir(sp), dataFileName)
	var fin *os.File
	if fin, err = os.Open(datafile); err != nil {
		//no data file, clear error
		if os.IsNotExist(err) {
			err = nil
		}
		return
	}
	if err = json.NewDecoder(fin).Decode(&data); err != nil {
		fin.Close()
		return
	}

	err = fin.Close()
	return
}
