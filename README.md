# govega

Render [Vega](https://vega.github.io/vega/) and
[Vega-Lite](https://vega.github.io/vega-lite/) visualizations to SVG from
pure Go. No cgo, no browser, no Node.js, and `CGO_ENABLED=0` builds a fully
static binary.

Hand the library a Vega or Vega-Lite spec plus your data, in columns or
rows, and get back an SVG document. It is meant for servers that produce
charts for reports, alerts, emails and dashboards without a browser in the
loop.

## How it works

The package embeds the upstream Vega 6 and Vega-Lite 6 JavaScript bundles
and runs them with [goja](https://github.com/dop251/goja), a JavaScript
interpreter written in Go. Go supplies everything the headless runtime
would otherwise take from a browser:

- text measurement, from width tables derived from the Liberation fonts,
  so axis, legend and label layout matches what a viewer will draw;
- a pixel canvas backed by a Go bitmap, for the few transforms that need
  one (`heatmap`, `wordcloud`, `label`), with glyphs rasterized by the Go
  fonts through `golang.org/x/image`;
- timers, logging, and loading of external data through a pluggable,
  off-by-default loader.

Because the real Vega runtime does the work, the whole spec language is
supported: every transform, scale, projection (including the extended
d3-geo-projection types), axis, legend, mark type, expression and signal.
All 94 examples in the [Vega gallery](https://vega.github.io/vega/examples/)
and 54 vendored [Vega-Lite gallery](https://vega.github.io/vega-lite/examples/)
examples render; [docs/examples-report.md](docs/examples-report.md) lists
render times and a pixel-level similarity score against Vega running in
Chrome for every gallery example.

## Install

```bash
go get github.com/gravwell/govega
```

## A simple example

```go
package main

import (
	"context"
	"log"
	"os"

	"github.com/gravwell/govega"
)

const spec = `{
  "width": 400, "height": 200,
  "data": [{"name": "table"}],
  "scales": [
    {"name": "x", "type": "band", "domain": {"data": "table", "field": "category"}, "range": "width", "padding": 0.1},
    {"name": "y", "type": "linear", "domain": {"data": "table", "field": "amount"}, "range": "height", "nice": true}
  ],
  "axes": [{"orient": "bottom", "scale": "x"}, {"orient": "left", "scale": "y"}],
  "marks": [{
    "type": "rect", "from": {"data": "table"},
    "encode": {"enter": {
      "x": {"scale": "x", "field": "category"}, "width": {"scale": "x", "band": 1},
      "y": {"scale": "y", "field": "amount"}, "y2": {"scale": "y", "value": 0},
      "fill": {"value": "steelblue"}
    }}
  }]
}`

func main() {
	r, err := govega.New()
	if err != nil {
		log.Fatal(err)
	}
	svg, err := r.RenderSVG(context.Background(), []byte(spec), map[string]govega.Dataset{
		"table": govega.NewDataset(
			govega.Col("category", []string{"A", "B", "C", "D"}),
			govega.Col("amount", []float64{28, 55, 43, 91}),
		),
	})
	if err != nil {
		log.Fatal(err)
	}
	os.WriteFile("bar.svg", svg, 0o644)
}
```

Create one `Renderer` per process and share it. It is safe for concurrent
use: it compiles the JavaScript bundles once and keeps a pool of
interpreter instances, each serving one render at a time.

## More examples

The [examples](examples/) directory holds small, runnable programs:

| Example | Shows |
|---|---|
| [examples/basic](examples/basic/main.go) | A bar chart from Go slices. |
| [examples/timeseries](examples/timeseries/main.go) | `time.Time` columns, several series, gaps for missing values, a signal override for the title. |
| [examples/vegalite](examples/vegalite/main.go) | A Vega-Lite spec with named data from row-oriented maps and a dark theme through `Config`. |
| [examples/loader](examples/loader/main.go) | A spec that loads CSV data by URL from an embedded file system. |

Run one with `go run ./examples/basic`; each writes an SVG next to where it
runs.

## Providing data

Datasets are column-oriented. A `Column` holds a slice of any element type:
integers, floats, bools, strings, `time.Time`, pointers and `any` all work.
`time.Time` is sent as epoch milliseconds, which Vega's time scales and date
functions accept directly; `NaN`, `±Inf` and the zero time become `null`.
`govega.FromRows` converts row-oriented `[]map[string]any` data.

For Vega specs, datasets are matched to `data` definitions by name,
including definitions nested inside group marks:

- A matching definition has its `values` replaced. Its `transform` pipeline
  is kept, and `url`/`source` references and the `format.type` are dropped
  (the data is already parsed). `format.parse` directives are kept.
- A name with no matching definition is added as a new top-level data set.

For Vega-Lite specs, datasets go in through the spec's top-level `datasets`
property, so refer to them by name:

```json
{"data": {"name": "table"}, "mark": "line", "encoding": {...}}
```

Any `data` object with a matching `name`, at any level of a `layer`,
`concat`, `facet` or `repeat` composition, resolves to the injected rows;
a `url` on it is dropped.

## Vega-Lite

Vega-Lite specs are detected from their `$schema` URL (or, without one,
from top-level properties such as `mark` or `layer`), compiled with the
embedded Vega-Lite compiler and rendered. Set `Request.Mode` to `ModeVega`
or `ModeVegaLite` to force an interpretation.

## Render options

`Renderer.Render` takes a `Request`:

| Field | Purpose |
|---|---|
| `Spec` | The Vega or Vega-Lite spec as JSON. |
| `Data` | Datasets by name. |
| `Mode` | Force Vega or Vega-Lite interpretation (default: detect). |
| `Signals` | Signal values that override the spec's definitions, for parameterized charts. |
| `Config` | A Vega (or Vega-Lite) config object for theming. |
| `Locale` | d3 number and time format locale definitions. |
| `ScaleFactor` | Scales the output dimensions. |
| `Seed` | Seeds Vega's random number generator so `random()`, `sample`, `wordcloud` and `force` are reproducible. |

Renderer options:

| Option | Default | Purpose |
|---|---|---|
| `WithLoader` | loading disabled | Resolve `url` data sources (`FSLoader`, `HTTPLoader`, `MultiLoader`). |
| `WithTimeout` | 30s | Per-render wall clock limit; the interpreter is interrupted. |
| `WithPoolSize` | `NumCPU` | Idle interpreter instances kept for reuse. |
| `WithLogger` | discard | Receive Vega warnings such as infinite extents. |
| `WithLogLevel` | warn | Vega log verbosity. |
| `WithFailOnError` | true | Fail the render when Vega logs a dataflow error. |
| `WithTextMeasurer` | built-in metrics | Replace the text width function used for layout. |

### Loading external data

URLs referenced by a spec are **not** fetched unless a `Loader` is
configured, because fetching arbitrary URLs named in an untrusted spec is a
request-forgery risk. A spec that references a URL without a loader fails
the render rather than silently drawing an empty chart. A spec's data
sources are fetched concurrently, so a custom `Loader` must be safe for
concurrent use.

### Errors

Vega never throws for problems inside its dataflow (an unknown projection
type, a failing expression): it logs an error and renders whatever is left,
often an empty chart. By default this package turns such logged errors into
a render failure. `WithFailOnError(false)` restores the lenient behavior,
and the messages still arrive through `WithLogger`.

## Command-line tools

`vega2svg` renders one spec:

```bash
go run ./cmd/vega2svg -data table=values.csv -o chart.svg spec.vg.json
```

The spec may be Vega or Vega-Lite. `-data name=file.csv` injects CSV files
(header row required; numbers and RFC 3339 timestamps are inferred),
`-root dir` allows `url` references relative to a directory, `-allow-http`
permits http(s) URLs, `-signal name=json` sets signals, `-config file.json`
applies a theme and `-mode vega|vega-lite` overrides detection.

`vega-report` renders a directory of specs, times each one and, when Chrome
is installed, renders the same specs with upstream Vega in headless Chrome,
rasterizes both outputs and scores how similar they look. It produced
[docs/examples-report.md](docs/examples-report.md) from the Vega gallery:

```bash
go run ./cmd/vega-report -specs gallery/specs -root gallery -out report
```

## Gallery demo

The `demo` tool is registered in `go.mod` as a Go tool, so it runs from
anywhere inside the module:

```bash
go tool demo
```

It downloads every example from the [Vega gallery](https://vega.github.io/vega/examples/)
and the [Vega-Lite gallery](https://vega.github.io/vega-lite/examples/),
with the data files each spec references and the reference image published
alongside each example, then serves a page at <http://localhost:8080/>. The
first run downloads about 70 MB into the user cache directory
(`~/.cache/govega-demo` on Linux); later runs start from the cache.

The page opens with a table of every example (gallery, name, description,
render time, bytes of spec and data fed to the chart, and a link to the SVG
govega produced), and below it shows each govega rendering next to the
official image so the two can be compared at a glance. Rendering starts as
soon as the server is up and runs in the background; rows show "waiting"
until their example has rendered, and the page updates itself without a
reload.

Flags: `-addr host:port` chooses the listen address, `-gallery vega` or
`-gallery vega-lite` loads one gallery, `-filter regexp` selects examples
by name, `-jobs n` renders n examples at once (faster overall, but the
times in the table then include contention), `-timeout` bounds a single
render and `-refresh` downloads everything again:

```bash
go tool demo -addr localhost:9000 -gallery vega-lite -filter '^bar'
```

## Text measurement and the canvas

Vega lays out axes, legends and truncated labels from measured text widths.
Without a canvas it falls back to a crude `0.8 × length × fontSize`
estimate. This package measures text with character widths and kerning pairs taken
from the Liberation Sans (`sans-serif` and unknown families) and Liberation
Serif (`serif`) fonts, regular and bold, and a fixed 0.6 em per character
for `monospace`. The tables cover ASCII, Latin-1, Latin Extended-A and
common punctuation; other characters are approximated. The Liberation fonts
are metrically compatible with Arial and Times New Roman, which match
Helvetica and Times, so layout closely tracks what a viewer will draw: in
the gallery comparison, measurements agree with Chrome's to within 0.2 px.
`WithTextMeasurer` plugs in exact metrics for a specific font. The tables
live in `fontwidths.go`, generated by `genwidths.go` from the Liberation
2.1.5 TTF files (`go generate` regenerates them when the fonts are
installed).

The `heatmap`, `wordcloud` and `label` transforms need a pixel canvas even
for SVG output. The package provides a `<canvas>` shim whose pixels live in
a Go bitmap. Text is rasterized with the embedded Go fonts, scaled
horizontally to the measured width; path fills and strokes, rectangles and
raw image data are painted with a pure-Go scanline rasterizer. Clipping and
`drawImage` are ignored.

## Time zones

Vega's `time` scales and date functions use the JavaScript runtime's local
time zone, which goja takes from Go's `time.Local`. Set `TZ` or assign
`time.Local` to control it, or use `utc` scales and the `utc*` expression
functions in specs that must not depend on it.

## Upgrading the bundles

Replace the files under `internal/jsdist/` with newer `build/*.min.js`
releases from npm (strip a trailing `sourceMappingURL` comment if present),
bump the version constants in `internal/jsdist/embed.go`, and run the tests.
`TestBundlePatchesApply` reports if a patch no longer matches, and the
gallery tests render the example specs under `testdata/`.

## Projects this builds on

- [Vega](https://github.com/vega/vega) and
  [Vega-Lite](https://github.com/vega/vega-lite), the visualization grammars
  and runtime from the University of Washington Interactive Data Lab.
  Everything a chart does happens in their code.
- [goja](https://github.com/dop251/goja), the pure-Go JavaScript engine
  that runs them.
- [D3](https://github.com/d3) modules bundled inside Vega: scales, shapes,
  geographic projections, layouts, time and number formatting, and
  [d3-cloud](https://github.com/jasondavies/d3-cloud) for word clouds.
- [golang.org/x/image](https://pkg.go.dev/golang.org/x/image) for font
  rasterization and image scaling, including the
  [Go fonts](https://go.dev/blog/go-fonts).
- The [Liberation fonts](https://github.com/liberationfonts/liberation-fonts),
  whose metrics drive text layout.
- [vega-datasets](https://github.com/vega/vega-datasets), the data behind
  the vendored gallery examples used in tests.

## Licenses

This project's own code is under the BSD 2-Clause license; see
[LICENSE](LICENSE). It embeds and links third-party code, fonts and data
under permissive licenses (BSD, MIT, ISC, Apache-2.0, Unlicense). The one
exception is `fontwidths.go`, which is derived from the Liberation fonts and
stays under the SIL Open Font License 1.1. The vendored test data carries
its own per-file terms. [LICENSES](LICENSES) lists every component, where
it came from and its full license text.

## Performance

Measured on this machine:

| | |
|---|---|
| CPU | Intel Core Ultra 5 335, 8 cores / 8 threads, 400 MHz to 4.7 GHz, 16 MiB L2, 12 MiB L3 |
| Memory | 30 GiB |
| OS / Go | Linux 7.0 (x86-64), Go 1.27.3 |

Each example program times its single `Render` call and prints the result.
The figures below are the median of seven runs of a fresh process, so they
include creating the interpreter instance (about 50 ms) but not compiling
the bundles, which `New` does once per process. Later renders on a pooled
instance skip that setup; the bar chart, for example, renders in about 25 ms
warm.

| Example | Description | Render time |
|---|---|---:|
| `examples/basic` | Five-bar chart with two axes, data from Go slices | 44 ms |
| `examples/timeseries` | 270-point, three-series line chart with a time axis, gaps, legend and title | 81 ms |
| `examples/vegalite` | Vega-Lite grouped bar chart compiled from a spec, with aggregation, legend and a dark theme | 70 ms |
| `examples/loader` | Stacked area chart over 52 CSV rows loaded by URL from an embedded file system | 79 ms |

Larger scenes, same machine, pooled instance:

| Chart | Render time |
|---|---:|
| 1,000-point multi-series line chart | ~130 ms |
| 50,000-point line mark | ~0.8 s |
| US county choropleth (3,000 shapes, 770 KB SVG) | ~5.5 s |

goja is an interpreter, roughly ten times slower than V8 on Vega's dataflow
code. Across the 94 Vega gallery examples the median slowdown against Vega in
Chrome is 11x; 52 of them render in under 100 ms and 81 in under a second,
and the slow ones are geographic and force layouts.
[docs/examples-report.md](docs/examples-report.md) lists every one.

Two load-time patches in `patch.go` rewrite string accumulation in Vega's
SVG renderer and in d3-path to use arrays. V8 makes repeated string
concatenation cheap with ropes and goja does not, so large scenes were
quadratic without them (98 s instead of 7 s for a 12 MB SVG). The patches
are exact-match substitutions guarded by a test, so a bundle upgrade that
no longer matches fails loudly.
