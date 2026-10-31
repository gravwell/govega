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
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
)

// Loader resolves external resources referenced by a spec, such as
// `{"data": [{"name": "x", "url": "data/cars.json"}]}`. It returns the raw
// resource text; Vega performs any CSV/TSV/JSON/TopoJSON parsing itself.
// Load is called concurrently, both for the data sources of one spec and
// across renders, so implementations must be safe for concurrent use.
//
// The default Renderer has no Loader and rejects every URL, since fetching
// arbitrary URLs named in an untrusted spec is a server-side request forgery
// risk. Supply one with WithLoader to opt in.
type Loader interface {
	Load(ctx context.Context, uri string) ([]byte, error)
}

// LoaderFunc adapts a function to the Loader interface.
type LoaderFunc func(ctx context.Context, uri string) ([]byte, error)

// Load implements Loader.
func (f LoaderFunc) Load(ctx context.Context, uri string) ([]byte, error) { return f(ctx, uri) }

// ErrLoadingDisabled is returned when a spec references a URL but no Loader
// is configured.
var ErrLoadingDisabled = errors.New("vega: external data loading is disabled; provide data via Datasets or configure a Loader")

// FSLoader serves spec URLs from a file system. URIs are cleaned and must
// stay within the file system root; "file://" prefixes are stripped.
func FSLoader(fsys fs.FS) Loader {
	return LoaderFunc(func(_ context.Context, uri string) ([]byte, error) {
		uri = strings.TrimPrefix(uri, "file://")
		uri = strings.TrimPrefix(uri, "/")
		if i := strings.IndexAny(uri, "?#"); i >= 0 {
			uri = uri[:i]
		}
		if !fs.ValidPath(uri) {
			return nil, fmt.Errorf("vega: invalid path %q", uri)
		}
		return fs.ReadFile(fsys, uri)
	})
}

// HTTPLoader fetches http(s) URLs with the given client (http.DefaultClient
// if nil). Only absolute http and https URLs are accepted. maxBytes caps the
// response size; zero means 64 MiB.
func HTTPLoader(client *http.Client, maxBytes int64) Loader {
	if client == nil {
		client = http.DefaultClient
	}
	if maxBytes <= 0 {
		maxBytes = 64 << 20
	}
	return LoaderFunc(func(ctx context.Context, uri string) ([]byte, error) {
		u, err := url.Parse(uri)
		if err != nil {
			return nil, err
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, fmt.Errorf("vega: unsupported URL scheme %q", u.Scheme)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return nil, fmt.Errorf("vega: GET %s: %s", uri, resp.Status)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
		if err != nil {
			return nil, err
		}
		if int64(len(body)) > maxBytes {
			return nil, fmt.Errorf("vega: GET %s: response exceeds %d bytes", uri, maxBytes)
		}
		return body, nil
	})
}

// MultiLoader tries each loader in turn until one succeeds.
func MultiLoader(loaders ...Loader) Loader {
	return LoaderFunc(func(ctx context.Context, uri string) ([]byte, error) {
		var errs []error
		for _, l := range loaders {
			b, err := l.Load(ctx, uri)
			if err == nil {
				return b, nil
			}
			errs = append(errs, err)
		}
		if len(errs) == 0 {
			return nil, ErrLoadingDisabled
		}
		return nil, errors.Join(errs...)
	})
}
