// Copyright (c) 2026, Gravwell, Inc.
// All rights reserved.
//
// Use of this source code is governed by a BSD 2-Clause license that can be
// found in the LICENSE file.

// post.js runs after the Vega and Vega-Lite bundles have been evaluated and
// defines the __govega API that the Go side calls into.
(function (global) {
  'use strict';
  var vega = global.vega;
  var vegaLite = global.vegaLite;
  var host = global.__govega_host;

  if (vega.textMetrics.width !== vega.textMetrics.measureWidth) {
    throw new Error('govega: Vega did not pick up the host text measurer');
  }

  function errorText(e) {
    if (e == null) { return 'unknown error'; }
    if (e instanceof Error) { return e.message || String(e); }
    return String(e);
  }

  // findData locates a data definition by name in the spec, searching the
  // top-level `data` array and, recursively, the `data` arrays of group marks.
  function findData(node, name) {
    if (!node || typeof node !== 'object') { return null; }
    if (Array.isArray(node.data)) {
      for (var i = 0; i < node.data.length; i++) {
        if (node.data[i] && node.data[i].name === name) { return node.data[i]; }
      }
    }
    if (Array.isArray(node.marks)) {
      for (var j = 0; j < node.marks.length; j++) {
        var found = findData(node.marks[j], name);
        if (found) { return found; }
      }
    }
    return null;
  }

  // toRows transposes a column-oriented dataset {col: [v0, v1, ...]} into an
  // array of row objects.
  function toRows(columns) {
    var names = Object.keys(columns);
    if (names.length === 0) { return []; }
    var n = columns[names[0]].length;
    var rows = new Array(n);
    for (var i = 0; i < n; i++) {
      var row = {};
      for (var c = 0; c < names.length; c++) {
        row[names[c]] = columns[names[c]][i];
      }
      rows[i] = row;
    }
    return rows;
  }

  // inject replaces or adds data definitions so that the host-provided
  // datasets become the source of the corresponding named data sets. Any
  // existing transforms on a matched definition are preserved; url/source
  // references and the format *type* are dropped (values are already parsed),
  // while format.parse directives are kept.
  function inject(spec, datasets) {
    var names = Object.keys(datasets);
    if (names.length === 0) { return; }
    if (!Array.isArray(spec.data)) { spec.data = []; }
    var prepend = [];
    for (var i = 0; i < names.length; i++) {
      var name = names[i];
      var rows = toRows(datasets[name]);
      var def = findData(spec, name);
      if (!def) {
        prepend.push({ name: name, values: rows });
        continue;
      }
      delete def.url;
      delete def.source;
      delete def.async;
      if (def.format && typeof def.format === 'object') {
        delete def.format.type;
        delete def.format.property;
        delete def.format.delimiter;
        delete def.format.header;
        delete def.format.feature;
        delete def.format.mesh;
        if (Object.keys(def.format).length === 0) { delete def.format; }
      }
      def.values = rows;
    }
    if (prepend.length) { spec.data = prepend.concat(spec.data); }
  }

  // isVegaLite decides whether a spec is Vega-Lite. An explicit mode wins;
  // otherwise the $schema URL is consulted, and finally the presence of
  // Vega-Lite-only top-level properties.
  function isVegaLite(spec, mode) {
    if (mode === 'vega') { return false; }
    if (mode === 'vega-lite') { return true; }
    var schema = spec.$schema;
    if (typeof schema === 'string') {
      if (/vega-lite/.test(schema)) { return true; }
      if (/\/vega\//.test(schema)) { return false; }
    }
    return ['mark', 'layer', 'hconcat', 'vconcat', 'concat', 'repeat', 'facet', 'spec']
      .some(function (k) { return k in spec; }) && !('marks' in spec);
  }

  // injectLite makes host datasets available to a Vega-Lite spec. Every
  // dataset is registered under spec.datasets, which Vega-Lite resolves for
  // any `{"data": {"name": ...}}` reference; data objects anywhere in the
  // spec whose name matches lose their url/values so that they resolve to
  // the injected dataset.
  function injectLite(spec, datasets) {
    var names = Object.keys(datasets);
    if (names.length === 0) { return; }
    spec.datasets = spec.datasets || {};
    names.forEach(function (name) { spec.datasets[name] = toRows(datasets[name]); });
    function visit(node) {
      if (!node || typeof node !== 'object') { return; }
      if (node.data && typeof node.data === 'object' && !Array.isArray(node.data) &&
          Object.prototype.hasOwnProperty.call(datasets, node.data.name)) {
        delete node.data.url;
        delete node.data.values;
        delete node.data.sequence;
        delete node.data.sphere;
        delete node.data.graticule;
        if (node.data.format && typeof node.data.format === 'object') {
          delete node.data.format.type;
          delete node.data.format.property;
          delete node.data.format.feature;
          delete node.data.format.mesh;
          if (Object.keys(node.data.format).length === 0) { delete node.data.format; }
        }
      }
      ['layer', 'hconcat', 'vconcat', 'concat'].forEach(function (k) {
        if (Array.isArray(node[k])) { node[k].forEach(visit); }
      });
      if (node.spec) { visit(node.spec); }
    }
    visit(spec);
  }

  // makeLoader builds a Vega loader that delegates to the Go host, which
  // fetches on a goroutine so that several data sources load concurrently.
  // Vega itself only warns when a load fails and continues with empty data;
  // we additionally record failures on the result so the render can be failed.
  function makeLoader(result) {
    var loader = vega.loader();
    loader.load = function (uri) {
      return new Promise(function (resolve, reject) {
        host.loadStart(String(uri), resolve, function (msg) {
          result.loadErrors.push(String(msg));
          reject(new Error(String(msg)));
        });
      });
    };
    loader.http = loader.file = function (uri) { return loader.load(uri); };
    return loader;
  }

  // overrideSignals applies host signal values to the top-level signal
  // definitions of a Vega spec before parsing. A signal's `update` or `init`
  // expression would otherwise recompute it on the first dataflow run and
  // discard the override, so those are removed; event handlers are kept.
  // It returns the names with no top-level definition, which are set on the
  // View instead (built-in signals such as width, or an error for unknown
  // names).
  function overrideSignals(spec, values) {
    var rest = {};
    var defs = Array.isArray(spec.signals) ? spec.signals : [];
    Object.keys(values).forEach(function (name) {
      var def = null;
      for (var i = 0; i < defs.length; i++) {
        if (defs[i] && defs[i].name === name) { def = defs[i]; break; }
      }
      if (!def) { rest[name] = values[name]; return; }
      delete def.update;
      delete def.init;
      def.value = values[name];
    });
    return rest;
  }

  var levels = { none: vega.None, error: vega.Error, warn: vega.Warn, info: vega.Info, debug: vega.Debug };

  // render parses and renders a spec. It returns a result object that is
  // filled in asynchronously; the Go side drains promise jobs and timers
  // until result.done is true.
  function render(specText, datasetsText, optsText) {
    var result = { done: false, svg: null, error: null, loadErrors: [], mode: null };
    var view = null;
    function finish(svg, err) {
      result.svg = svg;
      result.error = err;
      result.done = true;
      vega.setRandom(Math.random);
      if (view) { try { view.finalize(); } catch (e) { /* ignore */ } }
    }
    try {
      var spec = JSON.parse(specText);
      var opts = optsText ? JSON.parse(optsText) : {};
      var datasets = datasetsText ? JSON.parse(datasetsText) : {};
      if (!spec || typeof spec !== 'object' || Array.isArray(spec)) {
        throw new Error('spec must be a JSON object');
      }
      var lite = isVegaLite(spec, opts.mode);
      result.mode = lite ? 'vega-lite' : 'vega';
      var config = opts.config || undefined;
      if (lite) {
        injectLite(spec, datasets);
        spec = vegaLite.compile(spec, { config: config }).spec;
        config = undefined; // already folded into the compiled spec
      } else {
        inject(spec, datasets);
      }

      var signals = opts.signals ? overrideSignals(spec, opts.signals) : {};
      // A seeded generator makes random() and the sampling transforms
      // reproducible; Math.random is restored once the render finishes.
      vega.setRandom(opts.seed ? vega.randomLCG(opts.seed) : Math.random);
      var runtime = vega.parse(spec, config);
      var viewOpts = {
        renderer: 'none',
        loader: makeLoader(result),
        logLevel: levels[opts.logLevel || 'warn']
      };
      if (opts.locale) { viewOpts.locale = opts.locale; }
      view = new vega.View(runtime, viewOpts);
      Object.keys(signals).forEach(function (name) {
        view.signal(name, signals[name]);
      });
      view.toSVG(opts.scaleFactor || 1).then(
        function (svg) { finish(svg, null); },
        function (e) { finish(null, errorText(e)); }
      );
    } catch (e) {
      finish(null, errorText(e));
    }
    return result;
  }

  global.__govega = { render: render, version: vega.version, liteVersion: vegaLite.version };
})(globalThis);
