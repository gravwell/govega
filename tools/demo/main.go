// Copyright (c) 2026, Gravwell, Inc.
// All rights reserved.
//
// Use of this source code is governed by a BSD 2-Clause license that can be
// found in the LICENSE file.

// Command demo downloads the Vega and Vega-Lite example galleries (specs,
// data and the reference images published with each example), renders every
// spec with govega and serves a page that lists the results and shows each
// rendering next to the official image.
//
//	go tool demo
//
// Downloads are cached under the user cache directory so that later runs
// start immediately; pass -refresh to download everything again.
package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gravwell/govega"
)

//go:embed page.html
var pageHTML string

// gallery describes where one upstream example set lives.
type gallery struct {
	id      string // "vega" or "vega-lite"; also the cache subdirectory
	label   string
	index   string // examples.json listing the gallery
	page    string // example page URL, %s is the name
	spec    string // spec URL, %s is the name
	image   []string
	dataURL string // base that relative data URLs are resolved against
	mode    govega.Mode
}

var galleries = []gallery{
	{
		id:      "vega",
		label:   "Vega",
		index:   "https://raw.githubusercontent.com/vega/vega/main/docs/_data/examples.json",
		page:    "https://vega.github.io/vega/examples/%s/",
		spec:    "https://vega.github.io/vega/examples/%s.vg.json",
		image:   []string{"https://vega.github.io/vega/examples/img/%s.png"},
		dataURL: "https://vega.github.io/vega/",
		mode:    govega.ModeVega,
	},
	{
		id:      "vega-lite",
		label:   "Vega-Lite",
		index:   "https://raw.githubusercontent.com/vega/vega-lite/main/site/_data/examples.json",
		page:    "https://vega.github.io/vega-lite/examples/%s.html",
		spec:    "https://vega.github.io/vega-lite/examples/%s.vl.json",
		image:   []string{"https://vega.github.io/vega-lite/examples/%s.svg", "https://vega.github.io/vega-lite/examples/%s.png"},
		dataURL: "https://vega.github.io/vega-lite/examples/",
		mode:    govega.ModeVegaLite,
	},
}

// example is one gallery entry together with everything downloaded for it
// and, once rendered, the result.
type example struct {
	ID          string `json:"id"` // gallery/name
	Gallery     string `json:"gallery"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Category    string `json:"category"`
	Description string `json:"description"`
	PageURL     string `json:"page_url"`
	DataBytes   int    `json:"data_bytes"` // spec plus every data file it references
	RefExt      string `json:"ref_ext"`    // extension of the cached reference image, "" when none

	mode  govega.Mode
	spec  []byte
	data  map[string][]byte // data URL as written in the spec -> contents
	ref   []byte
	fetch []string // problems while downloading, shown on the page

	mu       sync.Mutex
	State    string   `json:"state"` // waiting, rendering, ok, error
	Error    string   `json:"error,omitempty"`
	RenderMs float64  `json:"render_ms"`
	SVGBytes int      `json:"svg_bytes"`
	Logs     []string `json:"logs,omitempty"` // Vega warnings and errors logged during the render
	svg      []byte
}

func (e *example) snapshot() example {
	e.mu.Lock()
	defer e.mu.Unlock()
	return example{ID: e.ID, Gallery: e.Gallery, Name: e.Name, Title: e.Title, Category: e.Category,
		Description: e.Description, PageURL: e.PageURL, DataBytes: e.DataBytes, RefExt: e.RefExt,
		State: e.State, Error: e.Error, RenderMs: e.RenderMs, SVGBytes: e.SVGBytes, Logs: e.Logs}
}

func main() {
	addr := flag.String("addr", "localhost:8080", "address to serve on")
	cacheDir := flag.String("cache", defaultCacheDir(), "directory for downloaded specs, data and images")
	which := flag.String("gallery", "both", "galleries to load: vega, vega-lite or both")
	filter := flag.String("filter", "", "regexp selecting example names")
	refresh := flag.Bool("refresh", false, "download everything again instead of using the cache")
	jobs := flag.Int("jobs", 1, "concurrent renders; more than one finishes sooner but inflates the per-example times")
	timeout := flag.Duration("timeout", time.Minute, "per-render timeout")
	flag.Parse()

	if err := run(*addr, *cacheDir, *which, *filter, *refresh, *jobs, *timeout); err != nil {
		log.Fatal(err)
	}
}

func defaultCacheDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "govega-demo")
}

func run(addr, cacheDir, which, filter string, refresh bool, jobs int, timeout time.Duration) error {
	var re *regexp.Regexp
	if filter != "" {
		var err error
		if re, err = regexp.Compile(filter); err != nil {
			return fmt.Errorf("bad -filter: %w", err)
		}
	}
	var selected []gallery
	for _, g := range galleries {
		if which == "both" || which == g.id {
			selected = append(selected, g)
		}
	}
	if len(selected) == 0 {
		return fmt.Errorf("unknown -gallery %q (want vega, vega-lite or both)", which)
	}

	fetcher := &fetcher{dir: cacheDir, refresh: refresh, client: &http.Client{Timeout: 2 * time.Minute}}
	var examples []*example
	for _, g := range selected {
		list, err := loadGallery(fetcher, g, re)
		if err != nil {
			return fmt.Errorf("%s gallery: %w", g.label, err)
		}
		examples = append(examples, list...)
	}
	if len(examples) == 0 {
		return errors.New("no examples matched")
	}
	log.Printf("%d examples ready (cache: %s)", len(examples), cacheDir)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: newServer(examples)}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()
	log.Printf("serving on http://%s/", ln.Addr())

	if err := renderAll(examples, jobs, timeout); err != nil {
		return err
	}
	select {}
}

// fetcher downloads URLs into a disk cache.
type fetcher struct {
	dir     string
	refresh bool
	client  *http.Client
}

// get returns the contents of url, downloading it to <dir>/<name> unless
// that file already exists.
func (f *fetcher) get(url, name string) ([]byte, error) {
	p := filepath.Join(f.dir, filepath.FromSlash(name))
	if !f.refresh {
		if b, err := os.ReadFile(p); err == nil {
			return b, nil
		}
	}
	resp, err := f.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		return nil, err
	}
	return b, nil
}

// indexEntry is one example in a gallery's examples.json.
type indexEntry struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// loadGallery downloads the gallery index and then, concurrently, every
// example's spec, data files and reference image.
func loadGallery(f *fetcher, g gallery, re *regexp.Regexp) ([]*example, error) {
	raw, err := f.get(g.index, g.id+"/examples.json")
	if err != nil {
		return nil, err
	}
	entries, err := parseIndex(raw)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", g.index, err)
	}
	var list []*example
	seen := map[string]bool{}
	for _, in := range entries {
		if seen[in.entry.Name] || (re != nil && !re.MatchString(in.entry.Name)) {
			continue
		}
		seen[in.entry.Name] = true
		list = append(list, &example{
			ID:          g.id + "/" + in.entry.Name,
			Gallery:     g.label,
			Name:        in.entry.Name,
			Title:       in.entry.Title,
			Category:    in.category,
			Description: plainText(in.entry.Description),
			PageURL:     fmt.Sprintf(g.page, in.entry.Name),
			mode:        g.mode,
			data:        map[string][]byte{},
			State:       "waiting",
		})
	}
	log.Printf("%s: fetching %d examples", g.label, len(list))

	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	var mu sync.Mutex
	var firstErr error
	for _, ex := range list {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if err := fetchExample(f, g, ex); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: %w", ex.Name, err)
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return list, firstErr
}

type indexed struct {
	category string
	entry    indexEntry
}

// parseIndex flattens a gallery index. Vega's is {category: [entry]};
// Vega-Lite's nests one more level, {group: {category: [entry]}}.
func parseIndex(raw []byte) ([]indexed, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, err
	}
	var out []indexed
	var walk func(category string, v json.RawMessage) error
	walk = func(category string, v json.RawMessage) error {
		var entries []indexEntry
		if err := json.Unmarshal(v, &entries); err == nil {
			for _, e := range entries {
				if e.Name != "" {
					out = append(out, indexed{category, e})
				}
			}
			return nil
		}
		var sub map[string]json.RawMessage
		if err := json.Unmarshal(v, &sub); err != nil {
			return fmt.Errorf("category %q: %w", category, err)
		}
		for _, k := range sortedKeys(sub) {
			if err := walk(k, sub[k]); err != nil {
				return err
			}
		}
		return nil
	}
	// Preserve the gallery's own category order where the JSON decoder
	// lost it: decode again as an ordered list of keys.
	for _, k := range orderedKeys(raw) {
		if v, ok := top[k]; ok {
			if err := walk(k, v); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// orderedKeys returns the top-level keys of a JSON object in document order.
func orderedKeys(raw []byte) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var keys []string
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return keys
		}
		keys = append(keys, tok.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return keys
		}
	}
	return keys
}

func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// fetchExample downloads an example's spec, description, data and image.
// Only a missing spec is an error; anything else is noted on the page.
func fetchExample(f *fetcher, g gallery, ex *example) error {
	spec, err := f.get(fmt.Sprintf(g.spec, ex.Name), path.Join(g.id, "specs", ex.Name+".json"))
	if err != nil {
		return err
	}
	ex.spec = spec
	ex.DataBytes = len(spec)

	var meta struct {
		Description string `json:"description"`
	}
	_ = json.Unmarshal(spec, &meta)
	if g.id == "vega" {
		// The Vega gallery keeps titles and descriptions in each example's
		// page source rather than in the index.
		md, err := f.get("https://raw.githubusercontent.com/vega/vega/main/docs/examples/"+ex.Name+".md",
			path.Join(g.id, "pages", ex.Name+".md"))
		if err == nil {
			title, desc := parseExamplePage(md)
			ex.Title, ex.Description = title, desc
		}
	}
	if ex.Title == "" {
		ex.Title = strings.ReplaceAll(strings.ReplaceAll(ex.Name, "_", " "), "-", " ")
	}
	if ex.Description == "" {
		ex.Description = meta.Description
	}

	for _, u := range dataURLs(spec) {
		src, name := u, path.Join(g.id, u)
		if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
			name = path.Join("remote", fmt.Sprintf("%x", sha1.Sum([]byte(u)))+path.Ext(u))
		} else {
			src = g.dataURL + u
		}
		b, err := f.get(src, name)
		if err != nil {
			ex.fetch = append(ex.fetch, err.Error())
			continue
		}
		ex.data[u] = b
		ex.DataBytes += len(b)
	}

	for _, pattern := range g.image {
		u := fmt.Sprintf(pattern, ex.Name)
		b, err := f.get(u, path.Join(g.id, "img", ex.Name+path.Ext(u)))
		if err == nil {
			ex.ref, ex.RefExt = b, path.Ext(u)
			break
		}
	}
	if ex.RefExt == "" {
		ex.fetch = append(ex.fetch, "no reference image published for this example")
	}
	return nil
}

var (
	frontMatterTitle = regexp.MustCompile(`(?m)^title:\s*(.+?)\s*$`)
	mdLink           = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	liquidTag        = regexp.MustCompile(`\{%[^%]*%\}`)
)

// parseExamplePage extracts the title and first paragraph from a Vega
// example page (Jekyll front matter followed by Markdown).
func parseExamplePage(md []byte) (title, desc string) {
	s := string(md)
	if m := frontMatterTitle.FindStringSubmatch(s); m != nil {
		title = strings.TrimSuffix(strings.TrimSpace(m[1]), " Example")
	}
	if i := strings.Index(s, "\n---"); i >= 0 {
		s = s[i+4:]
	}
	s = liquidTag.ReplaceAllString(s, "")
	for _, para := range strings.Split(s, "\n\n") {
		para = strings.TrimSpace(para)
		if para != "" && !strings.HasPrefix(para, "<") {
			desc = plainText(para)
			break
		}
	}
	return title, desc
}

// plainText strips Markdown links and newlines from a description.
func plainText(s string) string {
	s = mdLink.ReplaceAllString(s, "$1")
	return strings.Join(strings.Fields(s), " ")
}

// relativeData matches the gallery's data file references, which some
// specs keep in signal values or bound select options rather than in a
// "url" property.
var relativeData = regexp.MustCompile(`^data/[\w./-]+$`)

// dataURLs returns every string-valued "url" property found anywhere in a
// spec, plus any other string that names a gallery data file, without
// duplicates.
func dataURLs(spec []byte) []string {
	var v any
	if err := json.Unmarshal(spec, &v); err != nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	add := func(u string) {
		if u != "" && !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	var walk func(any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if u, ok := v["url"].(string); ok {
				add(u)
			}
			for _, k := range sortedAnyKeys(v) {
				walk(v[k])
			}
		case []any:
			for _, e := range v {
				walk(e)
			}
		case string:
			if relativeData.MatchString(v) {
				add(v)
			}
		}
	}
	walk(v)
	return out
}

func sortedAnyKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// exampleKey carries the example being rendered to the loader through the
// render context, so one Renderer serves every example's data.
type exampleKey struct{}

func renderAll(examples []*example, jobs int, timeout time.Duration) error {
	loader := govega.LoaderFunc(func(ctx context.Context, uri string) ([]byte, error) {
		ex, _ := ctx.Value(exampleKey{}).(*example)
		if ex == nil {
			return nil, errors.New("no example in context")
		}
		if b, ok := ex.data[uri]; ok {
			return b, nil
		}
		return nil, fmt.Errorf("%s was not downloaded", uri)
	})

	// Each worker owns a Renderer so that the Vega log output it forwards
	// can be attributed to the example that worker is rendering.
	queue := make(chan *example)
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < jobs; i++ {
		var cur *example
		r, err := govega.New(
			govega.WithLoader(loader),
			govega.WithTimeout(timeout),
			govega.WithPoolSize(1),
			govega.WithLogger(func(level, msg string) {
				if cur != nil {
					cur.Logs = append(cur.Logs, level+": "+msg)
				}
			}),
		)
		if err != nil {
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ex := range queue {
				cur = ex
				renderOne(r, ex)
				cur = nil
			}
		}()
	}
	for _, ex := range examples {
		queue <- ex
	}
	close(queue)
	wg.Wait()

	var ok, failed, warned int
	for _, ex := range examples {
		if ex.State == "ok" {
			ok++
		} else {
			failed++
		}
		if len(ex.Logs) > 0 {
			warned++
		}
	}
	log.Printf("rendered %d examples in %s: %d ok, %d failed, %d with warnings",
		len(examples), time.Since(start).Round(time.Millisecond), ok, failed, warned)
	return nil
}

func renderOne(r *govega.Renderer, ex *example) {
	ex.mu.Lock()
	ex.State = "rendering"
	ex.mu.Unlock()

	ctx := context.WithValue(context.Background(), exampleKey{}, ex)
	t0 := time.Now()
	svg, err := r.Render(ctx, govega.Request{Spec: ex.spec, Mode: ex.mode, Seed: 42})
	ms := float64(time.Since(t0).Microseconds()) / 1000

	ex.mu.Lock()
	defer ex.mu.Unlock()
	ex.RenderMs = ms
	if err != nil {
		ex.State, ex.Error = "error", err.Error()
		log.Printf("%s: %v", ex.ID, err)
		return
	}
	ex.State, ex.svg, ex.SVGBytes = "ok", svg, len(svg)
}

// server serves the comparison page, a JSON status feed and the images.
type server struct {
	examples []*example
	byID     map[string]*example
	page     *template.Template
}

func newServer(examples []*example) http.Handler {
	s := &server{examples: examples, byID: map[string]*example{}}
	for _, ex := range examples {
		s.byID[ex.ID] = ex
	}
	s.page = template.Must(template.New("page").Funcs(template.FuncMap{
		"bytes": formatBytes,
		"ms":    formatMs,
	}).Parse(pageHTML))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("GET /status", s.status)
	mux.HandleFunc("GET /svg/{gallery}/{name}", s.svg)
	mux.HandleFunc("GET /ref/{gallery}/{name}", s.ref)
	mux.HandleFunc("GET /spec/{gallery}/{name}", s.spec)
	return mux
}

type pageExample struct {
	example
	Fetch []string
}

func (s *server) index(w http.ResponseWriter, _ *http.Request) {
	var rows []pageExample
	for _, ex := range s.examples {
		rows = append(rows, pageExample{ex.snapshot(), ex.fetch})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.page.Execute(w, map[string]any{
		"Examples": rows,
		"Version":  govega.Version(),
		"Lite":     govega.LiteVersion(),
	}); err != nil {
		log.Printf("page: %v", err)
	}
}

func (s *server) status(w http.ResponseWriter, _ *http.Request) {
	out := make([]example, 0, len(s.examples))
	for _, ex := range s.examples {
		out = append(out, ex.snapshot())
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *server) lookup(w http.ResponseWriter, req *http.Request, ext string) *example {
	name := strings.TrimSuffix(req.PathValue("name"), ext)
	ex := s.byID[req.PathValue("gallery")+"/"+name]
	if ex == nil {
		http.NotFound(w, req)
	}
	return ex
}

func (s *server) svg(w http.ResponseWriter, req *http.Request) {
	ex := s.lookup(w, req, ".svg")
	if ex == nil {
		return
	}
	ex.mu.Lock()
	svg, state := ex.svg, ex.State
	ex.mu.Unlock()
	if state != "ok" {
		http.Error(w, "not rendered: "+state, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(svg)
}

func (s *server) ref(w http.ResponseWriter, req *http.Request) {
	ex := s.lookup(w, req, path.Ext(req.PathValue("name")))
	if ex == nil {
		return
	}
	if ex.RefExt == "" {
		http.NotFound(w, req)
		return
	}
	ct := "image/png"
	if ex.RefExt == ".svg" {
		ct = "image/svg+xml"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "max-age=86400")
	_, _ = w.Write(ex.ref)
}

func (s *server) spec(w http.ResponseWriter, req *http.Request) {
	ex := s.lookup(w, req, ".json")
	if ex == nil {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(ex.spec)
}

func formatBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func formatMs(ms float64) string {
	if ms >= 1000 {
		return fmt.Sprintf("%.2f s", ms/1000)
	}
	return fmt.Sprintf("%.0f ms", ms)
}
