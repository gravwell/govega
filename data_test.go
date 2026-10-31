// Copyright (c) 2026, Gravwell, Inc.
// All rights reserved.
//
// Use of this source code is governed by a BSD 2-Clause license that can be
// found in the LICENSE file.

package govega

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

func TestEncodeDatasets(t *testing.T) {
	seven := 7.5
	ts := time.Date(2026, 1, 2, 3, 4, 5, 600_000_000, time.UTC)
	data := map[string]Dataset{
		"t": NewDataset(
			Col("f", []float64{1, math.NaN(), math.Inf(1)}),
			Col("s", []string{"a", `q"uote`, ""}),
			Col("i", []int32{1, 2, 3}),
			Col("u", []uint8{1, 2, 3}),
			Col("b", []bool{true, false, true}),
			Col("tm", []time.Time{ts, {}, ts}),
			Col("p", []*float64{&seven, nil, &seven}),
			Col("any", []any{1, "x", nil}),
			Col("obj", []map[string]int{{"k": 1}, {"k": 2}, {"k": 3}}),
		),
	}
	b, err := encodeDatasets(data)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]map[string][]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("invalid JSON %s: %v", b, err)
	}
	cols := got["t"]
	if cols["f"][1] != nil || cols["f"][2] != nil || cols["f"][0] != 1.0 {
		t.Errorf("floats: %v", cols["f"])
	}
	if cols["s"][1] != `q"uote` {
		t.Errorf("strings: %v", cols["s"])
	}
	if cols["i"][2] != 3.0 || cols["u"][0] != 1.0 {
		t.Errorf("ints: %v %v", cols["i"], cols["u"])
	}
	if cols["tm"][0] != float64(ts.UnixMilli()) || cols["tm"][1] != nil {
		t.Errorf("times: %v", cols["tm"])
	}
	if cols["p"][0] != 7.5 || cols["p"][1] != nil {
		t.Errorf("pointers: %v", cols["p"])
	}
	if cols["any"][0] != 1.0 || cols["any"][1] != "x" || cols["any"][2] != nil {
		t.Errorf("any: %v", cols["any"])
	}
	if m, ok := cols["obj"][1].(map[string]any); !ok || m["k"] != 2.0 {
		t.Errorf("objects: %v", cols["obj"])
	}
}

func TestEncodeDatasetsErrors(t *testing.T) {
	if _, err := encodeDatasets(map[string]Dataset{"t": NewDataset(Col("a", []int{1}), Col("a", []int{2}))}); err == nil {
		t.Error("duplicate column should fail")
	}
	if _, err := encodeDatasets(map[string]Dataset{"t": NewDataset(Col("a", nil))}); err == nil {
		t.Error("nil values should fail")
	}
	if b, err := encodeDatasets(nil); err != nil || b != nil {
		t.Errorf("empty datasets: %s %v", b, err)
	}
}

func TestFromRows(t *testing.T) {
	ds := FromRows([]map[string]any{{"b": 1, "a": "x"}, {"a": "y"}})
	if len(ds.Columns) != 2 || ds.Columns[0].Name != "a" || ds.Columns[1].Name != "b" {
		t.Fatalf("columns: %+v", ds.Columns)
	}
	if n, err := ds.Len(); err != nil || n != 2 {
		t.Fatalf("len: %d %v", n, err)
	}
	if ds.Columns[1].Values.([]any)[1] != nil {
		t.Errorf("missing key should be nil")
	}
}
