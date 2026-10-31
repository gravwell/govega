# Vega example gallery report

Every example from the Vega gallery, rendered by govega and compared with
upstream Vega running in Chrome. Produced by `cmd/vega-report`; see the end
of this file for how to reproduce it.

Generated 2026-10-29 with govega (Vega 6.4.0) against the 94 specs from https://vega.github.io/vega/examples/.

| | |
|---|---|
| Rendered by govega | 94 of 94 |
| Failed in govega | 0 |
| Reference (Vega in Chrome) failures | 0 |
| Mean similarity score | 98.9 |
| Total render time, govega / Chrome | 61.6 s / 2.9 s |

The similarity score compares govega's SVG with upstream Vega's SVG for the
same spec, both rasterized by the same Chrome build with the generic font
families mapped to the Liberation fonts. The images are converted to
grayscale, lightly blurred and downsampled by two (so sub-pixel and one-pixel
offsets are forgiven), and compared with SSIM over 8x8 windows; the mean SSIM
is scaled to 0-100. "Pixels" is the percentage of those downsampled pixels
whose intensity differs by at most 16 of 255. A chart whose text wraps or
whose axes shift because of different text measurements scores lower even
when every mark is present.


## How to read the table

- **govega** is the wall-clock time of `Renderer.Render` on one core of an
  8-core laptop with a warm interpreter from the pool. **Chrome** is the time
  Vega 6.4.0 needed for `view.toSVG()` inside headless Chrome 156 on the same
  machine. goja is an interpreter, so govega is slower by a median factor of
  about 11; 52 examples render in under 100 ms and 81 in under a second.
- **Similarity** and **Pixels** are explained above. Both renders use the
  same random seed (`Request.Seed` on the govega side), so examples that
  call `random()` are comparable.
- The report was produced in the America/Denver time zone; both renderers
  use the host zone.

## The remaining differences

Four examples score below 99. None of them is a rendering failure.

| Example | Score | Cause |
|---|---:|---|
| word-cloud | 34 | d3-cloud packs words by testing the pixels of each rasterized word. govega rasterizes glyphs with the Go fonts and Chrome with Liberation Sans, so the first collision test that differs sends the layouts down different paths. Every word is present and legible in both; only the placement differs. |
| earthquakes-globe | 79 | The legend title asks for the font `Tahoma` at weight `lighter`. Neither renderer has Tahoma. govega measures it with its sans-serif widths; Chrome's font fallback picked a light face of a different family, which is 5 px narrower, so the whole globe shifts left by 5 px. Font substitution for named fonts is outside the library's control. |
| watch, clock | 94, 95 | Both draw the current time with `now()`; the hands moved between the two renders. |

Two further details are worth knowing even though they did not affect the
scores. The heatmap example contains hourly data across a daylight-saving
transition; the nonexistent local hour on that day lands on a different row
in goja than in V8, so one cell of 8,760 differs. And interactive examples
(brushing, crossfilter, zooming, tooltips) render their initial state, since
there is no user input in either renderer.

## Reproducing

```bash
# fetch the gallery specs and data
mkdir -p gallery/specs && cd gallery
for name in $(curl -s https://vega.github.io/vega/examples/ | grep -o 'href="[a-z0-9-]*/"' | sed 's/href="//; s/\/"//' | sort -u); do
  curl -sfL -o specs/$name.vg.json https://vega.github.io/vega/examples/$name.vg.json
done
grep -ho '"url": *"data/[^"]*"' specs/*.json | sed 's/.*"\(data\/[^"]*\)"/\1/' | sort -u |
  while read f; do curl -sfL --create-dirs -o "$f" "https://vega.github.io/vega/$f"; done
cd ..
# render, compare and write report/report.md plus a side-by-side report/index.html
go run ./cmd/vega-report -specs gallery/specs -root gallery -out report
```

The comparison phase needs `google-chrome` on the path and the Liberation
fonts installed (package `fonts-liberation` on Debian and Ubuntu). Pass
`-chrome ""` to skip it and only time the govega renders.

## Results

| Example | govega | Chrome | Similarity | Pixels | Status |
|---|---:|---:|---:|---:|---|
| [airport-connections](https://vega.github.io/vega/examples/airport-connections/) | 1.1 s | 95 ms | 100 | 100% | ok |
| [annual-precipitation](https://vega.github.io/vega/examples/annual-precipitation/) | 6.3 s | 85 ms | 100 | 100% | ok |
| [annual-temperature](https://vega.github.io/vega/examples/annual-temperature/) | 500 ms | 28 ms | 100 | 100% | ok |
| [arc-diagram](https://vega.github.io/vega/examples/arc-diagram/) | 77 ms | 9 ms | 100 | 100% | ok |
| [area-chart](https://vega.github.io/vega/examples/area-chart/) | 29 ms | 4 ms | 100 | 100% | ok |
| [bar-chart](https://vega.github.io/vega/examples/bar-chart/) | 21 ms | 3 ms | 100 | 100% | ok |
| [bar-line-toggle](https://vega.github.io/vega/examples/bar-line-toggle/) | 32 ms | 8 ms | 100 | 100% | ok |
| [barley-trellis-plot](https://vega.github.io/vega/examples/barley-trellis-plot/) | 110 ms | 12 ms | 100 | 100% | ok |
| [beeswarm-plot](https://vega.github.io/vega/examples/beeswarm-plot/) | 1.4 s | 19 ms | 100 | 100% | ok |
| [binned-scatter-plot](https://vega.github.io/vega/examples/binned-scatter-plot/) | 79 ms | 7 ms | 100 | 100% | ok |
| [box-plot](https://vega.github.io/vega/examples/box-plot/) | 27 ms | 5 ms | 100 | 100% | ok |
| [brushing-scatter-plots](https://vega.github.io/vega/examples/brushing-scatter-plots/) | 650 ms | 25 ms | 100 | 100% | ok |
| [budget-forecasts](https://vega.github.io/vega/examples/budget-forecasts/) | 57 ms | 8 ms | 100 | 100% | ok |
| [calendar-view](https://vega.github.io/vega/examples/calendar-view/) | 1.1 s | 85 ms | 100 | 100% | ok |
| [circle-packing](https://vega.github.io/vega/examples/circle-packing/) | 53 ms | 6 ms | 100 | 100% | ok |
| [clock](https://vega.github.io/vega/examples/clock/) | 41 ms | 3 ms | 95 | 98% | ok |
| [connected-scatter-plot](https://vega.github.io/vega/examples/connected-scatter-plot/) | 60 ms | 11 ms | 100 | 100% | ok |
| [contour-plot](https://vega.github.io/vega/examples/contour-plot/) | 506 ms | 17 ms | 100 | 100% | ok |
| [county-unemployment](https://vega.github.io/vega/examples/county-unemployment/) | 5.3 s | 152 ms | 100 | 100% | ok |
| [crossfilter-flights](https://vega.github.io/vega/examples/crossfilter-flights/) | 3.5 s | 280 ms | 100 | 100% | ok |
| [density-heatmaps](https://vega.github.io/vega/examples/density-heatmaps/) | 193 ms | 14 ms | 100 | 100% | ok |
| [distortion-comparison](https://vega.github.io/vega/examples/distortion-comparison/) | 797 ms | 27 ms | 100 | 100% | ok |
| [donut-chart-labelled](https://vega.github.io/vega/examples/donut-chart-labelled/) | 34 ms | 6 ms | 100 | 100% | ok |
| [donut-chart](https://vega.github.io/vega/examples/donut-chart/) | 4 ms | 1 ms | 100 | 100% | ok |
| [dorling-cartogram](https://vega.github.io/vega/examples/dorling-cartogram/) | 1.4 s | 31 ms | 100 | 100% | ok |
| [dot-plot](https://vega.github.io/vega/examples/dot-plot/) | 45 ms | 4 ms | 100 | 100% | ok |
| [earthquakes-globe](https://vega.github.io/vega/examples/earthquakes-globe/) | 646 ms | 34 ms | 79 | 91% | size 627x658 vs 622x658 |
| [earthquakes](https://vega.github.io/vega/examples/earthquakes/) | 576 ms | 27 ms | 100 | 100% | ok |
| [edge-bundling](https://vega.github.io/vega/examples/edge-bundling/) | 863 ms | 39 ms | 100 | 100% | ok |
| [error-bars](https://vega.github.io/vega/examples/error-bars/) | 165 ms | 10 ms | 100 | 100% | ok |
| [falkensee-population](https://vega.github.io/vega/examples/falkensee-population/) | 43 ms | 5 ms | 100 | 100% | ok |
| [flight-passengers](https://vega.github.io/vega/examples/flight-passengers/) | 20 ms | 6 ms | 100 | 100% | ok |
| [force-directed-layout](https://vega.github.io/vega/examples/force-directed-layout/) | 3.2 s | 50 ms | 100 | 100% | ok |
| [global-development](https://vega.github.io/vega/examples/global-development/) | 72 ms | 10 ms | 100 | 100% | ok |
| [grouped-bar-chart](https://vega.github.io/vega/examples/grouped-bar-chart/) | 21 ms | 3 ms | 100 | 100% | ok |
| [heatmap](https://vega.github.io/vega/examples/heatmap/) | 776 ms | 42 ms | 100 | 100% | ok |
| [histogram-null-values](https://vega.github.io/vega/examples/histogram-null-values/) | 70 ms | 13 ms | 100 | 100% | ok |
| [histogram](https://vega.github.io/vega/examples/histogram/) | 77 ms | 4 ms | 100 | 100% | ok |
| [horizon-graph](https://vega.github.io/vega/examples/horizon-graph/) | 19 ms | 2 ms | 100 | 100% | ok |
| [hypothetical-outcome-plots](https://vega.github.io/vega/examples/hypothetical-outcome-plots/) | 34 ms | 3 ms | 100 | 100% | ok |
| [interactive-legend](https://vega.github.io/vega/examples/interactive-legend/) | 97 ms | 8 ms | 100 | 100% | ok |
| [job-voyager](https://vega.github.io/vega/examples/job-voyager/) | 854 ms | 46 ms | 100 | 100% | ok |
| [labeled-scatter-plot](https://vega.github.io/vega/examples/labeled-scatter-plot/) | 607 ms | 47 ms | 100 | 100% | ok |
| [line-chart](https://vega.github.io/vega/examples/line-chart/) | 19 ms | 6 ms | 100 | 100% | ok |
| [loess-regression](https://vega.github.io/vega/examples/loess-regression/) | 4.6 s | 41 ms | 100 | 100% | ok |
| [map-with-tooltip](https://vega.github.io/vega/examples/map-with-tooltip/) | 2.8 s | 99 ms | 100 | 100% | ok |
| [nested-bar-chart](https://vega.github.io/vega/examples/nested-bar-chart/) | 26 ms | 3 ms | 100 | 100% | ok |
| [overview-plus-detail](https://vega.github.io/vega/examples/overview-plus-detail/) | 36 ms | 6 ms | 100 | 100% | ok |
| [packed-bubble-chart](https://vega.github.io/vega/examples/packed-bubble-chart/) | 8 ms | 3 ms | 100 | 100% | ok |
| [pacman](https://vega.github.io/vega/examples/pacman/) | 111 ms | 7 ms | 100 | 100% | ok |
| [parallel-coordinates-interactive](https://vega.github.io/vega/examples/parallel-coordinates-interactive/) | 649 ms | 44 ms | 100 | 100% | ok |
| [parallel-coordinates](https://vega.github.io/vega/examples/parallel-coordinates/) | 420 ms | 20 ms | 100 | 100% | ok |
| [pi-monte-carlo](https://vega.github.io/vega/examples/pi-monte-carlo/) | 280 ms | 14 ms | 100 | 100% | ok |
| [pie-chart](https://vega.github.io/vega/examples/pie-chart/) | 8 ms | 1 ms | 100 | 100% | ok |
| [platformer](https://vega.github.io/vega/examples/platformer/) | 1.0 s | 468 ms | 99 | 100% | ok |
| [population-pyramid](https://vega.github.io/vega/examples/population-pyramid/) | 28 ms | 4 ms | 100 | 100% | ok |
| [probability-density](https://vega.github.io/vega/examples/probability-density/) | 117 ms | 14 ms | 100 | 100% | ok |
| [projections](https://vega.github.io/vega/examples/projections/) | 12.4 s | 387 ms | 100 | 100% | ok |
| [quantile-dot-plot](https://vega.github.io/vega/examples/quantile-dot-plot/) | 30 ms | 3 ms | 100 | 100% | ok |
| [quantile-quantile-plot](https://vega.github.io/vega/examples/quantile-quantile-plot/) | 54 ms | 6 ms | 100 | 100% | ok |
| [radar-chart](https://vega.github.io/vega/examples/radar-chart/) | 13 ms | 2 ms | 100 | 100% | ok |
| [radial-plot](https://vega.github.io/vega/examples/radial-plot/) | 5 ms | 1 ms | 100 | 100% | ok |
| [radial-tree-layout](https://vega.github.io/vega/examples/radial-tree-layout/) | 67 ms | 6 ms | 100 | 100% | ok |
| [regression](https://vega.github.io/vega/examples/regression/) | 180 ms | 17 ms | 100 | 100% | ok |
| [reorderable-matrix](https://vega.github.io/vega/examples/reorderable-matrix/) | 325 ms | 14 ms | 100 | 100% | ok |
| [scatter-plot-null-values](https://vega.github.io/vega/examples/scatter-plot-null-values/) | 317 ms | 28 ms | 100 | 100% | ok |
| [scatter-plot](https://vega.github.io/vega/examples/scatter-plot/) | 61 ms | 6 ms | 100 | 100% | ok |
| [serpentine-timeline](https://vega.github.io/vega/examples/serpentine-timeline/) | 108 ms | 15 ms | 100 | 100% | ok |
| [stacked-area-chart](https://vega.github.io/vega/examples/stacked-area-chart/) | 19 ms | 2 ms | 100 | 100% | ok |
| [stacked-bar-chart](https://vega.github.io/vega/examples/stacked-bar-chart/) | 30 ms | 2 ms | 100 | 100% | ok |
| [stock-index-chart](https://vega.github.io/vega/examples/stock-index-chart/) | 65 ms | 5 ms | 100 | 100% | ok |
| [sunburst](https://vega.github.io/vega/examples/sunburst/) | 29 ms | 4 ms | 100 | 100% | ok |
| [table-scrollbar](https://vega.github.io/vega/examples/table-scrollbar/) | 14 ms | 4 ms | 100 | 100% | ok |
| [time-units](https://vega.github.io/vega/examples/time-units/) | 296 ms | 48 ms | 100 | 100% | ok |
| [timelines](https://vega.github.io/vega/examples/timelines/) | 31 ms | 2 ms | 100 | 100% | ok |
| [top-k-plot-with-others](https://vega.github.io/vega/examples/top-k-plot-with-others/) | 86 ms | 22 ms | 100 | 100% | ok |
| [top-k-plot](https://vega.github.io/vega/examples/top-k-plot/) | 72 ms | 14 ms | 100 | 100% | ok |
| [tree-layout](https://vega.github.io/vega/examples/tree-layout/) | 74 ms | 13 ms | 100 | 100% | ok |
| [treemap](https://vega.github.io/vega/examples/treemap/) | 21 ms | 7 ms | 100 | 100% | ok |
| [u-district-cuisine](https://vega.github.io/vega/examples/u-district-cuisine/) | 186 ms | 18 ms | 100 | 100% | ok |
| [violin-plot](https://vega.github.io/vega/examples/violin-plot/) | 76 ms | 6 ms | 100 | 100% | ok |
| [volcano-contours](https://vega.github.io/vega/examples/volcano-contours/) | 174 ms | 8 ms | 100 | 100% | ok |
| [warming-stripes](https://vega.github.io/vega/examples/warming-stripes/) | 113 ms | 24 ms | 100 | 100% | ok |
| [watch](https://vega.github.io/vega/examples/watch/) | 34 ms | 3 ms | 94 | 98% | ok |
| [weekly-temperature](https://vega.github.io/vega/examples/weekly-temperature/) | 19 ms | 3 ms | 100 | 100% | ok |
| [wheat-and-wages](https://vega.github.io/vega/examples/wheat-and-wages/) | 37 ms | 6 ms | 100 | 100% | ok |
| [wheat-plot](https://vega.github.io/vega/examples/wheat-plot/) | 63 ms | 5 ms | 100 | 100% | ok |
| [wind-vectors](https://vega.github.io/vega/examples/wind-vectors/) | 523 ms | 22 ms | 100 | 100% | ok |
| [word-cloud](https://vega.github.io/vega/examples/word-cloud/) | 3.2 s | 112 ms | 34 | 67% | ok |
| [world-map](https://vega.github.io/vega/examples/world-map/) | 518 ms | 28 ms | 100 | 100% | ok |
| [zoomable-binned-plot](https://vega.github.io/vega/examples/zoomable-binned-plot/) | 72 ms | 14 ms | 100 | 100% | ok |
| [zoomable-circle-packing](https://vega.github.io/vega/examples/zoomable-circle-packing/) | 54 ms | 7 ms | 100 | 100% | ok |
| [zoomable-scatter-plot](https://vega.github.io/vega/examples/zoomable-scatter-plot/) | 69 ms | 4 ms | 100 | 100% | ok |
| [zoomable-world-map](https://vega.github.io/vega/examples/zoomable-world-map/) | 515 ms | 16 ms | 100 | 100% | ok |
