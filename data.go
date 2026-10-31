// Copyright (c) 2026, Gravwell, Inc.
// All rights reserved.
//
// Use of this source code is governed by a BSD 2-Clause license that can be
// found in the LICENSE file.

package govega

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"time"
)

// Column is one named column of a Dataset.
//
// Values must be a slice. Element types are converted to JSON as follows:
//
//   - integers, floats, bools and strings map to their JSON equivalents;
//     NaN and ±Inf become null
//   - time.Time becomes the number of milliseconds since the Unix epoch,
//     which Vega's time scales and date functions accept directly
//   - nil pointers and nil interfaces become null; non-nil pointers are
//     dereferenced
//   - anything else is encoded with encoding/json
type Column struct {
	Name   string
	Values any
}

// Col is a convenience constructor for Column.
func Col(name string, values any) Column { return Column{Name: name, Values: values} }

// Dataset is column-oriented tabular data. Every column must have the same
// length. Datasets are handed to Vega as an array of row objects, one key per
// column.
type Dataset struct {
	Columns []Column
}

// NewDataset builds a Dataset from columns.
func NewDataset(cols ...Column) Dataset { return Dataset{Columns: cols} }

// FromRows builds a Dataset from row-oriented data. Column order follows
// the sorted union of all keys. Missing keys become null.
func FromRows(rows []map[string]any) Dataset {
	keys := map[string]struct{}{}
	for _, r := range rows {
		for k := range r {
			keys[k] = struct{}{}
		}
	}
	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	ds := Dataset{Columns: make([]Column, len(names))}
	for i, name := range names {
		vals := make([]any, len(rows))
		for j, r := range rows {
			vals[j] = r[name]
		}
		ds.Columns[i] = Column{Name: name, Values: vals}
	}
	return ds
}

// Len returns the number of rows, or an error if the columns are ragged or
// a column is not a slice.
func (d Dataset) Len() (int, error) {
	n := -1
	for _, c := range d.Columns {
		if c.Values == nil {
			return 0, fmt.Errorf("column %q has nil values", c.Name)
		}
		rv := reflect.ValueOf(c.Values)
		if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
			return 0, fmt.Errorf("column %q: values must be a slice, got %T", c.Name, c.Values)
		}
		if n >= 0 && rv.Len() != n {
			return 0, fmt.Errorf("column %q has %d values, expected %d", c.Name, rv.Len(), n)
		}
		n = rv.Len()
	}
	if n < 0 {
		n = 0
	}
	return n, nil
}

// encodeDatasets marshals name -> {column -> [values]} as JSON for the
// JavaScript side, which transposes it into rows.
func encodeDatasets(data map[string]Dataset) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(data))
	for n := range data {
		names = append(names, n)
	}
	sort.Strings(names)
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, name := range names {
		ds := data[name]
		if _, err := ds.Len(); err != nil {
			return nil, fmt.Errorf("dataset %q: %w", name, err)
		}
		if i > 0 {
			buf.WriteByte(',')
		}
		writeJSONString(&buf, name)
		buf.WriteByte(':')
		buf.WriteByte('{')
		seen := map[string]bool{}
		for j, c := range ds.Columns {
			if seen[c.Name] {
				return nil, fmt.Errorf("dataset %q: duplicate column %q", name, c.Name)
			}
			seen[c.Name] = true
			if j > 0 {
				buf.WriteByte(',')
			}
			writeJSONString(&buf, c.Name)
			buf.WriteByte(':')
			if err := encodeColumn(&buf, c.Values); err != nil {
				return nil, fmt.Errorf("dataset %q column %q: %w", name, c.Name, err)
			}
		}
		buf.WriteByte('}')
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func writeJSONString(buf *bytes.Buffer, s string) {
	b, _ := json.Marshal(s)
	buf.Write(b)
}

func encodeColumn(buf *bytes.Buffer, values any) error {
	// Fast paths for the most common slice types.
	switch v := values.(type) {
	case []float64:
		buf.WriteByte('[')
		for i, f := range v {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeFloat(buf, f)
		}
		buf.WriteByte(']')
		return nil
	case []string:
		buf.WriteByte('[')
		for i, s := range v {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeJSONString(buf, s)
		}
		buf.WriteByte(']')
		return nil
	case []time.Time:
		buf.WriteByte('[')
		for i, t := range v {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeTime(buf, t)
		}
		buf.WriteByte(']')
		return nil
	}
	rv := reflect.ValueOf(values)
	buf.WriteByte('[')
	for i := 0; i < rv.Len(); i++ {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := encodeValue(buf, rv.Index(i)); err != nil {
			return fmt.Errorf("row %d: %w", i, err)
		}
	}
	buf.WriteByte(']')
	return nil
}

var timeType = reflect.TypeFor[time.Time]()

func encodeValue(buf *bytes.Buffer, rv reflect.Value) error {
	for rv.Kind() == reflect.Interface || rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			buf.WriteString("null")
			return nil
		}
		rv = rv.Elem()
	}
	if rv.Type() == timeType {
		writeTime(buf, rv.Interface().(time.Time))
		return nil
	}
	switch rv.Kind() {
	case reflect.Float32, reflect.Float64:
		writeFloat(buf, rv.Float())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		fmt.Fprintf(buf, "%d", rv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		fmt.Fprintf(buf, "%d", rv.Uint())
	case reflect.Bool:
		if rv.Bool() {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case reflect.String:
		writeJSONString(buf, rv.String())
	default:
		b, err := json.Marshal(rv.Interface())
		if err != nil {
			return err
		}
		buf.Write(b)
	}
	return nil
}

func writeFloat(buf *bytes.Buffer, f float64) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		buf.WriteString("null")
		return
	}
	b, _ := json.Marshal(f)
	buf.Write(b)
}

// jsonMarshal encodes a value for the JavaScript side, encoding time.Time the
// same way as dataset columns (epoch milliseconds, zero time as null).
func jsonMarshal(v any) ([]byte, error) {
	if t, ok := v.(time.Time); ok {
		var buf bytes.Buffer
		writeTime(&buf, t)
		return buf.Bytes(), nil
	}
	return json.Marshal(v)
}

func writeTime(buf *bytes.Buffer, t time.Time) {
	if t.IsZero() {
		buf.WriteString("null")
		return
	}
	fmt.Fprintf(buf, "%d", t.UnixMilli())
}
