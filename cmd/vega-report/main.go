// Copyright (c) 2026, Gravwell, Inc.
// All rights reserved.
//
// Use of this source code is governed by a BSD 2-Clause license that can be
// found in the LICENSE file.

// Command vega-report renders a directory of Vega example specs with govega,
// times each render, and, when a Chrome binary is available, renders the same
// specs with upstream Vega inside headless Chrome and scores the visual
// similarity of the two results. It writes a Markdown report, a JSON file
// with the raw numbers, and a side-by-side HTML gallery.
//
//	vega-report -specs specs/ -root . -out report/
//
// Data files referenced by the specs (for example "data/cars.json") are
// resolved relative to -root. Pass -chrome "" to skip the browser phase.
package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gravwell/govega"
	"github.com/gravwell/govega/internal/jsdist"
)

//go:embed harness.html
var harnessHTML string

type result struct {
	Name    string   `json:"name"`
	OK      bool     `json:"ok"`
	Error   string   `json:"error,omitempty"`
	GoMs    float64  `json:"go_ms"`
	Bytes   int      `json:"bytes"`
	OurW    float64  `json:"our_w,omitempty"`
	OurH    float64  `json:"our_h,omitempty"`
	RefMs   float64  `json:"ref_ms,omitempty"`
	RefErr  string   `json:"ref_error,omitempty"`
	RefW    float64  `json:"ref_w,omitempty"`
	RefH    float64  `json:"ref_h,omitempty"`
	Score   float64  `json:"score"`       // SSIM-based similarity, 0..100
	PixelPc float64  `json:"pixel_match"` // percentage of near-identical pixels
	Scored  bool     `json:"scored"`
	Note    string   `json:"note,omitempty"`
	Logs    []string `json:"logs,omitempty"`
}

// browserResult is what the harness posts back for one example.
type browserResult struct {
	RefMs    float64 `json:"refMs"`
	RefError string  `json:"refError"`
	RefSVG   string  `json:"refSvg"`
	RefW     float64 `json:"refW"`
	RefH     float64 `json:"refH"`
	OurW     float64 `json:"ourW"`
	OurH     float64 `json:"ourH"`
	Score    float64 `json:"score"`
	PixelPc  float64 `json:"pixelMatch"`
	Scored   bool    `json:"scored"`
	Note     string  `json:"note"`
	RefPNG   string  `json:"refPng"`
	OurPNG   string  `json:"ourPng"`
}

func main() {
	specDir := flag.String("specs", "specs", "directory of *.vg.json specs")
	root := flag.String("root", ".", "directory that spec data URLs are resolved against")
	out := flag.String("out", "report", "output directory")
	chrome := flag.String("chrome", "google-chrome", "Chrome binary for the reference render; empty to skip")
	extraJS := flag.String("extra-js", "", "additional JavaScript (e.g. vega-projection-extended) loaded after Vega in both renderers' reference page")
	filter := flag.String("filter", "", "regexp selecting example names")
	timeout := flag.Duration("timeout", 2*time.Minute, "per-render timeout")
	browserTimeout := flag.Duration("browser-timeout", 40*time.Minute, "overall timeout for the browser phase")
	seed := flag.Uint("seed", 42, "random seed used by both renderers so that specs using random() are comparable")
	flag.Parse()

	if err := run(*specDir, *root, *out, *chrome, *extraJS, *filter, *timeout, *browserTimeout, uint32(*seed)); err != nil {
		log.Fatal(err)
	}
}

func run(specDir, root, out, chrome, extraJS, filter string, timeout, browserTimeout time.Duration, seed uint32) error {
	specs, err := filepath.Glob(filepath.Join(specDir, "*.vg.json"))
	if err != nil || len(specs) == 0 {
		return fmt.Errorf("no specs in %s", specDir)
	}
	sort.Strings(specs)
	var re *regexp.Regexp
	if filter != "" {
		re = regexp.MustCompile(filter)
	}
	for _, d := range []string{"svg", "ref", "png"} {
		if err := os.MkdirAll(filepath.Join(out, d), 0o755); err != nil {
			return err
		}
	}

	// Phase 1: govega renders.
	var logMu sync.Mutex
	var curLogs []string
	r, err := govega.New(
		govega.WithLoader(govega.FSLoader(os.DirFS(root))),
		govega.WithTimeout(timeout),
		govega.WithPoolSize(1),
		govega.WithLogger(func(level, msg string) {
			logMu.Lock()
			defer logMu.Unlock()
			curLogs = append(curLogs, level+": "+msg)
		}),
	)
	if err != nil {
		return err
	}
	var results []*result
	for _, path := range specs {
		name := strings.TrimSuffix(filepath.Base(path), ".vg.json")
		if re != nil && !re.MatchString(name) {
			continue
		}
		spec, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		logMu.Lock()
		curLogs = nil
		logMu.Unlock()
		start := time.Now()
		svg, err := r.Render(context.Background(), govega.Request{Spec: spec, Seed: seed})
		res := &result{Name: name, GoMs: float64(time.Since(start).Microseconds()) / 1000}
		logMu.Lock()
		res.Logs = append([]string(nil), curLogs...)
		logMu.Unlock()
		if err != nil {
			res.Error = err.Error()
			fmt.Printf("%-32s FAIL %8.1f ms  %s\n", name, res.GoMs, res.Error)
		} else {
			res.OK = true
			res.Bytes = len(svg)
			if err := os.WriteFile(filepath.Join(out, "svg", name+".svg"), svg, 0o644); err != nil {
				return err
			}
			fmt.Printf("%-32s ok   %8.1f ms  %8d bytes\n", name, res.GoMs, len(svg))
		}
		results = append(results, res)
	}

	// Phase 2: reference renders and comparison in headless Chrome.
	if chrome != "" {
		if err := browserPhase(results, specDir, root, out, chrome, extraJS, browserTimeout, seed); err != nil {
			return err
		}
	}

	return writeReport(results, out)
}

func browserPhase(results []*result, specDir, root, out, chrome, extraJS string, overall time.Duration, seed uint32) error {
	byName := map[string]*result{}
	for _, r := range results {
		byName[r.Name] = r
	}
	var extra string
	if extraJS != "" {
		b, err := os.ReadFile(extraJS)
		if err != nil {
			return err
		}
		extra = string(b)
	}

	done := make(chan struct{})
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("/harness", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(harnessHTML))
	})
	mux.HandleFunc("/vega.min.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(jsdist.VegaJS))
	})
	mux.HandleFunc("/extra.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(extra))
	})
	mux.HandleFunc("/manifest.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"examples": results, "seed": seed})
	})
	mux.Handle("/specs/", http.StripPrefix("/specs/", http.FileServer(http.Dir(specDir))))
	mux.Handle("/data/", http.StripPrefix("/data/", http.FileServer(http.Dir(filepath.Join(root, "data")))))
	mux.Handle("/ours/", http.StripPrefix("/ours/", http.FileServer(http.Dir(filepath.Join(out, "svg")))))
	mux.HandleFunc("/log", func(w http.ResponseWriter, req *http.Request) {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(req.Body)
		log.Printf("harness: %s", buf.String())
	})
	mux.HandleFunc("/result/", func(w http.ResponseWriter, req *http.Request) {
		name := strings.TrimPrefix(req.URL.Path, "/result/")
		res, ok := byName[name]
		if !ok {
			http.Error(w, "unknown example", 404)
			return
		}
		var br browserResult
		if err := json.NewDecoder(req.Body).Decode(&br); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		res.RefMs, res.RefErr, res.RefW, res.RefH = br.RefMs, br.RefError, br.RefW, br.RefH
		res.OurW, res.OurH = br.OurW, br.OurH
		res.Score, res.PixelPc, res.Scored, res.Note = br.Score, br.PixelPc, br.Scored, br.Note
		if br.RefSVG != "" {
			_ = os.WriteFile(filepath.Join(out, "ref", name+".svg"), []byte(br.RefSVG), 0o644)
		}
		for suffix, data := range map[string]string{"-ref.png": br.RefPNG, "-ours.png": br.OurPNG} {
			if i := strings.Index(data, ","); i > 0 {
				if b, err := base64.StdEncoding.DecodeString(data[i+1:]); err == nil {
					_ = os.WriteFile(filepath.Join(out, "png", name+suffix), b, 0o644)
				}
			}
		}
		status := fmt.Sprintf("score %5.1f  pixels %5.1f%%", br.Score, br.PixelPc)
		if br.RefError != "" {
			status = "REF-FAIL " + br.RefError
		}
		fmt.Printf("%-32s chrome %8.1f ms  %s %s\n", name, br.RefMs, status, br.Note)
	})
	mux.HandleFunc("/done", func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() { close(done) })
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()
	url := fmt.Sprintf("http://%s/harness", ln.Addr())
	fmt.Println("harness:", url)

	// Font configuration: make the generic families resolve to the Liberation
	// fonts, whose metrics govega assumes, so that layout differences reflect
	// the library rather than the host's default fonts.
	fontsConf := filepath.Join(out, "fonts.conf")
	if err := os.WriteFile(fontsConf, []byte(fontsConfXML), 0o644); err != nil {
		return err
	}
	profile, err := os.MkdirTemp(out, "chrome-profile-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(profile)

	ctx, cancel := context.WithTimeout(context.Background(), overall)
	defer cancel()
	cmd := exec.CommandContext(ctx, chrome,
		"--headless=new", "--disable-gpu", "--no-sandbox", "--hide-scrollbars",
		"--window-size=1600,1200", "--user-data-dir="+profile,
		"--disable-extensions", "--no-first-run", "--disable-background-timer-throttling",
		url)
	cmd.Env = append(os.Environ(), "FONTCONFIG_FILE="+fontsConf)
	var chromeOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &chromeOut, &chromeOut
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting chrome: %w", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case <-done:
		_ = cmd.Process.Kill()
		<-exited
	case err := <-exited:
		return fmt.Errorf("chrome exited before the harness finished: %v\n%s", err, chromeOut.String())
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		return fmt.Errorf("browser phase timed out after %v", overall)
	}
	return nil
}

// fontsConfXML maps the generic families, and the Helvetica/Arial/Times
// names, to the Liberation fonts. The system rules are included afterwards
// so that families the host lacks (Tahoma, Segoe UI, ...) fall back through
// sans-serif to Liberation Sans too, which is what govega assumes for any
// family it does not know.
const fontsConfXML = `<?xml version="1.0"?>
<!DOCTYPE fontconfig SYSTEM "fonts.dtd">
<fontconfig>
  <dir>/usr/share/fonts</dir>
  <dir>/usr/local/share/fonts</dir>
  <cachedir>/tmp/govega-fontconfig-cache</cachedir>
  <include ignore_missing="yes">/etc/fonts/conf.d</include>
  <alias binding="strong"><family>sans-serif</family><prefer><family>Liberation Sans</family></prefer></alias>
  <alias binding="strong"><family>serif</family><prefer><family>Liberation Serif</family></prefer></alias>
  <alias binding="strong"><family>monospace</family><prefer><family>Liberation Mono</family></prefer></alias>
  <alias binding="strong"><family>Helvetica</family><prefer><family>Liberation Sans</family></prefer></alias>
  <alias binding="strong"><family>Helvetica Neue</family><prefer><family>Liberation Sans</family></prefer></alias>
  <alias binding="strong"><family>Arial</family><prefer><family>Liberation Sans</family></prefer></alias>
  <alias binding="strong"><family>Times</family><prefer><family>Liberation Serif</family></prefer></alias>
</fontconfig>
`

func writeReport(results []*result, out string) error {
	js, _ := json.MarshalIndent(results, "", "  ")
	if err := os.WriteFile(filepath.Join(out, "report.json"), js, 0o644); err != nil {
		return err
	}

	var ok, fail, refFail, scored int
	var sumScore, sumGo, sumRef float64
	var scoreCount int
	for _, r := range results {
		if r.OK {
			ok++
		} else {
			fail++
		}
		if r.RefErr != "" {
			refFail++
		}
		if r.Scored {
			scored++
			sumScore += r.Score
			scoreCount++
		}
		sumGo += r.GoMs
		sumRef += r.RefMs
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Vega example gallery report\n\n")
	fmt.Fprintf(&b, "Generated %s with govega (Vega %s) against the %d specs from https://vega.github.io/vega/examples/.\n\n",
		time.Now().Format("2006-01-02"), govega.Version(), len(results))
	fmt.Fprintf(&b, "| | |\n|---|---|\n")
	fmt.Fprintf(&b, "| Rendered by govega | %d of %d |\n", ok, len(results))
	fmt.Fprintf(&b, "| Failed in govega | %d |\n", fail)
	if scored > 0 {
		fmt.Fprintf(&b, "| Reference (Vega in Chrome) failures | %d |\n", refFail)
		fmt.Fprintf(&b, "| Mean similarity score | %.1f |\n", sumScore/float64(scoreCount))
		fmt.Fprintf(&b, "| Total render time, govega / Chrome | %.1f s / %.1f s |\n", sumGo/1000, sumRef/1000)
	} else {
		fmt.Fprintf(&b, "| Total govega render time | %.1f s |\n", sumGo/1000)
	}
	b.WriteString("\n")
	if scored > 0 {
		b.WriteString(`The similarity score compares govega's SVG with upstream Vega's SVG for the
same spec, both rasterized by the same Chrome build with the generic font
families mapped to the Liberation fonts. The images are converted to
grayscale, lightly blurred and downsampled by two (so sub-pixel and one-pixel
offsets are forgiven), and compared with SSIM over 8x8 windows; the mean SSIM
is scaled to 0-100. "Pixels" is the percentage of those downsampled pixels
whose intensity differs by at most 16 of 255. A chart whose text wraps or
whose axes shift because of different text measurements scores lower even
when every mark is present.

`)
	}
	b.WriteString("| Example | govega | Chrome | Similarity | Pixels | Status |\n|---|---:|---:|---:|---:|---|\n")
	for _, r := range results {
		status := "ok"
		switch {
		case !r.OK:
			status = "**FAIL**: " + oneLine(r.Error)
		case r.RefErr != "":
			status = "reference failed: " + oneLine(r.RefErr)
		case r.Note != "":
			status = r.Note
		}
		refMs, score, pix := "", "", ""
		if r.RefMs > 0 {
			refMs = fmt.Sprintf("%.0f ms", r.RefMs)
		}
		if r.Scored {
			score = fmt.Sprintf("%.0f", r.Score)
			pix = fmt.Sprintf("%.0f%%", r.PixelPc)
		}
		fmt.Fprintf(&b, "| [%s](https://vega.github.io/vega/examples/%s/) | %s | %s | %s | %s | %s |\n",
			r.Name, r.Name, fmtMs(r.GoMs), refMs, score, pix, status)
	}
	if err := os.WriteFile(filepath.Join(out, "report.md"), []byte(b.String()), 0o644); err != nil {
		return err
	}

	// Side-by-side gallery.
	var h strings.Builder
	h.WriteString(`<!doctype html><meta charset="utf-8"><title>govega example report</title>
<style>body{font-family:sans-serif;margin:16px;background:#fff;color:#111}
.ex{margin:24px 0;border-top:1px solid #ddd;padding-top:12px}
.row{display:flex;gap:16px;flex-wrap:wrap}.row div{flex:1;min-width:300px}
img{max-width:100%;border:1px solid #eee;background:#fff}
.bad{color:#b00}.meta{color:#555;font-size:13px}</style><h1>govega example report</h1>`)
	for _, r := range results {
		fmt.Fprintf(&h, `<div class="ex"><h2 id="%s">%s</h2><div class="meta">govega %s`, r.Name, r.Name, fmtMs(r.GoMs))
		if r.RefMs > 0 {
			fmt.Fprintf(&h, " · Chrome %.0f ms", r.RefMs)
		}
		if r.Scored {
			fmt.Fprintf(&h, " · similarity %.0f · pixels %.0f%%", r.Score, r.PixelPc)
		}
		if r.Note != "" {
			fmt.Fprintf(&h, " · %s", r.Note)
		}
		h.WriteString("</div>")
		if !r.OK {
			fmt.Fprintf(&h, `<p class="bad">govega failed: %s</p>`, r.Error)
		}
		if r.RefErr != "" {
			fmt.Fprintf(&h, `<p class="bad">reference failed: %s</p>`, r.RefErr)
		}
		fmt.Fprintf(&h, `<div class="row"><div><h4>Vega in Chrome</h4><img src="png/%s-ref.png"></div><div><h4>govega</h4><img src="png/%s-ours.png"></div></div></div>`, r.Name, r.Name)
	}
	return os.WriteFile(filepath.Join(out, "index.html"), []byte(h.String()), 0o644)
}

func fmtMs(ms float64) string {
	if ms >= 1000 {
		return fmt.Sprintf("%.1f s", ms/1000)
	}
	return fmt.Sprintf("%.0f ms", ms)
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", "\\|")
	if len(s) > 160 {
		s = s[:157] + "..."
	}
	return s
}
