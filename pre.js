// Copyright (c) 2026, Gravwell, Inc.
// All rights reserved.
//
// Use of this source code is governed by a BSD 2-Clause license that can be
// found in the LICENSE file.

// pre.js runs before the Vega bundle is evaluated.
//
// Vega captures a canvas 2D context at module load time (via
// document.createElement('canvas')) and, when one exists, measures text with
// context.measureText instead of a crude `0.8 * length * fontSize` estimate.
// Several transforms (heatmap, wordcloud, label) also need canvas pixels. We
// provide a document whose only real element is a <canvas> backed by a Go
// bitmap; measureText uses the Go host's font-metrics tables.
(function (global) {
  'use strict';
  var host = global.__govega_host;
  function noop() {}

  // --- <canvas> shim ---------------------------------------------------------
  //
  // Vega creates canvases through document.createElement('canvas') for text
  // measurement and for the heatmap, wordcloud and label transforms. The
  // drawing state (transform, font, styles, current path) lives here; pixels
  // live in a Go bitmap addressed by id, which rasterizes text, path fills
  // and strokes, rectangles and raw image data.

  function Context2D(canvas) {
    this.canvas = canvas;
    this._m = [1, 0, 0, 1, 0, 0];
    this._stack = [];
    this._path = [];   // subpaths: arrays of canvas-space coordinates
    this._sub = null;  // current subpath
    this._sx = 0;      // current point in user space
    this._sy = 0;
    this.font = '10px sans-serif';
    this.fillStyle = '#000000';
    this.strokeStyle = '#000000';
    this.lineWidth = 1;
    this.lineCap = 'butt';
    this.lineJoin = 'miter';
    this.miterLimit = 10;
    this.globalAlpha = 1;
    this.globalCompositeOperation = 'source-over';
    this.textAlign = 'start';
    this.textBaseline = 'alphabetic';
    this.imageSmoothingEnabled = true;
    this._dash = [];
  }

  var STATE_KEYS = ['font', 'fillStyle', 'strokeStyle', 'lineWidth', 'lineCap', 'lineJoin',
    'miterLimit', 'globalAlpha', 'globalCompositeOperation', 'textAlign', 'textBaseline', '_dash'];

  function styleString(style) {
    if (style == null) { return '#000000'; }
    if (typeof style === 'string') { return style; }
    return '#000000'; // gradients and patterns rasterize as black
  }

  Context2D.prototype.save = function () {
    var st = { _m: this._m.slice() };
    for (var i = 0; i < STATE_KEYS.length; i++) { st[STATE_KEYS[i]] = this[STATE_KEYS[i]]; }
    this._stack.push(st);
  };
  Context2D.prototype.restore = function () {
    var st = this._stack.pop();
    if (!st) { return; }
    this._m = st._m;
    for (var i = 0; i < STATE_KEYS.length; i++) { this[STATE_KEYS[i]] = st[STATE_KEYS[i]]; }
  };
  Context2D.prototype.transform = function (a, b, c, d, e, f) {
    var m = this._m;
    this._m = [
      m[0] * a + m[2] * b, m[1] * a + m[3] * b,
      m[0] * c + m[2] * d, m[1] * c + m[3] * d,
      m[0] * e + m[2] * f + m[4], m[1] * e + m[3] * f + m[5]
    ];
  };
  Context2D.prototype.setTransform = function (a, b, c, d, e, f) {
    if (a && typeof a === 'object') { this._m = [a.a, a.b, a.c, a.d, a.e, a.f]; return; }
    this._m = [a, b, c, d, e, f];
  };
  Context2D.prototype.resetTransform = function () { this._m = [1, 0, 0, 1, 0, 0]; };
  Context2D.prototype.getTransform = function () {
    var m = this._m;
    return { a: m[0], b: m[1], c: m[2], d: m[3], e: m[4], f: m[5] };
  };
  Context2D.prototype.translate = function (x, y) { this.transform(1, 0, 0, 1, x, y); };
  Context2D.prototype.scale = function (x, y) { this.transform(x, 0, 0, y, 0, 0); };
  Context2D.prototype.rotate = function (r) {
    var c = Math.cos(r), s = Math.sin(r);
    this.transform(c, s, -s, c, 0, 0);
  };
  Context2D.prototype.measureText = function (text) {
    var w = host.measureText(String(text), String(this.font));
    return { width: w, actualBoundingBoxLeft: 0, actualBoundingBoxRight: w };
  };
  Context2D.prototype.fillText = function (text, x, y) {
    var m = this._m;
    host.canvasFillText(this.canvas._id, m[0], m[1], m[2], m[3], m[4] + m[0] * x + m[2] * y, m[5] + m[1] * x + m[3] * y,
      String(text), String(this.font), styleString(this.fillStyle), this.textAlign, this.textBaseline, 0);
  };
  Context2D.prototype.strokeText = function (text, x, y) {
    var m = this._m;
    host.canvasFillText(this.canvas._id, m[0], m[1], m[2], m[3], m[4] + m[0] * x + m[2] * y, m[5] + m[1] * x + m[3] * y,
      String(text), String(this.font), styleString(this.strokeStyle), this.textAlign, this.textBaseline, +this.lineWidth || 1);
  };
  Context2D.prototype.clearRect = function (x, y, w, h) {
    var m = this._m;
    host.canvasClearRect(this.canvas._id, m[0], m[1], m[2], m[3], m[4], m[5], x, y, w, h);
  };
  Context2D.prototype.fillRect = function (x, y, w, h) {
    var m = this._m;
    host.canvasFillRect(this.canvas._id, m[0], m[1], m[2], m[3], m[4], m[5], x, y, w, h, styleString(this.fillStyle));
  };
  Context2D.prototype.createImageData = function (w, h) {
    if (w && typeof w === 'object') { h = w.height; w = w.width; }
    return { data: new Uint8ClampedArray((w | 0) * (h | 0) * 4), width: w | 0, height: h | 0, colorSpace: 'srgb' };
  };
  Context2D.prototype.getImageData = function (x, y, w, h) {
    var buf = host.canvasGetImageData(this.canvas._id, x | 0, y | 0, w | 0, h | 0);
    return { data: new Uint8ClampedArray(buf), width: Math.abs(w | 0), height: Math.abs(h | 0), colorSpace: 'srgb' };
  };
  Context2D.prototype.putImageData = function (img, dx, dy) {
    var d = img.data;
    host.canvasPutImageData(this.canvas._id, d.buffer, d.byteOffset, d.byteLength, img.width | 0, img.height | 0, dx | 0, dy | 0);
  };
  Context2D.prototype.setLineDash = function (d) { this._dash = d || []; };
  Context2D.prototype.getLineDash = function () { return this._dash; };
  Context2D.prototype.isPointInPath = function () { return false; };
  Context2D.prototype.isPointInStroke = function () { return false; };
  Context2D.prototype.createLinearGradient = function () { return { addColorStop: noop }; };
  Context2D.prototype.createRadialGradient = function () { return { addColorStop: noop }; };
  Context2D.prototype.createPattern = function () { return {}; };
  // --- paths ---------------------------------------------------------------
  //
  // Paths are flattened to polylines in canvas space as they are built, as
  // the canvas specification requires (the transform applies at construction
  // time). fill() and stroke() hand the polylines to Go, which rasterizes
  // them with a non-zero winding scanline rasterizer. clip() is ignored.

  Context2D.prototype._pt = function (x, y) {
    var m = this._m;
    return [m[0] * x + m[2] * y + m[4], m[1] * x + m[3] * y + m[5]];
  };
  Context2D.prototype.beginPath = function () { this._path = []; this._sub = null; };
  Context2D.prototype.moveTo = function (x, y) {
    var p = this._pt(x, y);
    this._sub = { pts: [p[0], p[1]], closed: false, x0: x, y0: y };
    this._path.push(this._sub);
    this._sx = x; this._sy = y;
  };
  Context2D.prototype.lineTo = function (x, y) {
    if (!this._sub) { this.moveTo(x, y); return; }
    var p = this._pt(x, y);
    this._sub.pts.push(p[0], p[1]);
    this._sx = x; this._sy = y;
  };
  Context2D.prototype.closePath = function () {
    if (!this._sub) { return; }
    this._sub.closed = true;
    var x0 = this._sub.x0, y0 = this._sub.y0;
    this._sub = null;
    this.moveTo(x0, y0);
  };
  // _segments picks a flattening step count from the user-space extent of a
  // curve scaled by the transform, aiming at segments of roughly 2px.
  Context2D.prototype._segments = function (extent) {
    var m = this._m;
    var scale = Math.sqrt(Math.abs(m[0] * m[3] - m[1] * m[2])) || 1;
    return Math.max(4, Math.min(96, Math.ceil(extent * scale / 2)));
  };
  Context2D.prototype.bezierCurveTo = function (c1x, c1y, c2x, c2y, x, y) {
    var x0 = this._sx, y0 = this._sy;
    if (!this._sub) { this.moveTo(c1x, c1y); x0 = c1x; y0 = c1y; }
    var n = this._segments(Math.hypot(c1x - x0, c1y - y0) + Math.hypot(c2x - c1x, c2y - c1y) + Math.hypot(x - c2x, y - c2y));
    for (var i = 1; i <= n; i++) {
      var t = i / n, u = 1 - t;
      this.lineTo(u * u * u * x0 + 3 * u * u * t * c1x + 3 * u * t * t * c2x + t * t * t * x,
        u * u * u * y0 + 3 * u * u * t * c1y + 3 * u * t * t * c2y + t * t * t * y);
    }
  };
  Context2D.prototype.quadraticCurveTo = function (cx, cy, x, y) {
    var x0 = this._sx, y0 = this._sy;
    if (!this._sub) { this.moveTo(cx, cy); x0 = cx; y0 = cy; }
    var n = this._segments(Math.hypot(cx - x0, cy - y0) + Math.hypot(x - cx, y - cy));
    for (var i = 1; i <= n; i++) {
      var t = i / n, u = 1 - t;
      this.lineTo(u * u * x0 + 2 * u * t * cx + t * t * x, u * u * y0 + 2 * u * t * cy + t * t * y);
    }
  };
  Context2D.prototype.arc = function (x, y, r, a0, a1, ccw) {
    if (!(r >= 0)) { return; }
    var span = a1 - a0;
    var tau = 2 * Math.PI;
    if (ccw) {
      span = -(((a0 - a1) % tau) + tau) % tau;
      if (span === 0 && a0 !== a1) { span = -tau; }
    } else {
      span = (((a1 - a0) % tau) + tau) % tau;
      if (span === 0 && a0 !== a1) { span = tau; }
    }
    if (Math.abs(a1 - a0) >= tau) { span = ccw ? -tau : tau; }
    var n = this._segments(Math.abs(span) * r);
    var x0 = x + r * Math.cos(a0), y0 = y + r * Math.sin(a0);
    if (this._sub) { this.lineTo(x0, y0); } else { this.moveTo(x0, y0); }
    for (var i = 1; i <= n; i++) {
      var a = a0 + span * i / n;
      this.lineTo(x + r * Math.cos(a), y + r * Math.sin(a));
    }
  };
  Context2D.prototype.ellipse = function (x, y, rx, ry, rot, a0, a1, ccw) {
    this.save();
    this.translate(x, y);
    this.rotate(rot || 0);
    this.scale(rx, ry);
    this.arc(0, 0, 1, a0, a1, ccw);
    this.restore();
  };
  Context2D.prototype.arcTo = function (x1, y1, x2, y2) {
    // Approximation: a straight corner. Vega does not use arcTo.
    this.lineTo(x1, y1);
    this.lineTo(x2, y2);
  };
  Context2D.prototype.rect = function (x, y, w, h) {
    this.moveTo(x, y);
    this.lineTo(x + w, y);
    this.lineTo(x + w, y + h);
    this.lineTo(x, y + h);
    this.closePath();
  };
  Context2D.prototype.roundRect = function (x, y, w, h) { this.rect(x, y, w, h); };
  // _flat packs the current path as [count, closed, x0, y0, ...]* into a
  // Float64Array for the host.
  Context2D.prototype._flat = function () {
    var n = 0;
    for (var i = 0; i < this._path.length; i++) { n += 2 + this._path[i].pts.length; }
    var out = new Float64Array(n), k = 0;
    for (var j = 0; j < this._path.length; j++) {
      var sp = this._path[j];
      out[k++] = sp.pts.length / 2;
      out[k++] = sp.closed ? 1 : 0;
      for (var q = 0; q < sp.pts.length; q++) { out[k++] = sp.pts[q]; }
    }
    return out.buffer;
  };
  Context2D.prototype.fill = function () {
    if (!this._path.length) { return; }
    host.canvasFillPath(this.canvas._id, this._flat(), styleString(this.fillStyle), +this.globalAlpha);
  };
  Context2D.prototype.stroke = function () {
    if (!this._path.length) { return; }
    var m = this._m;
    var scale = Math.sqrt(Math.abs(m[0] * m[3] - m[1] * m[2])) || 1;
    host.canvasStrokePath(this.canvas._id, this._flat(), styleString(this.strokeStyle), +this.globalAlpha,
      (+this.lineWidth || 1) * scale);
  };
  Context2D.prototype.strokeRect = function (x, y, w, h) {
    var saved = this._path, savedSub = this._sub;
    this.beginPath();
    this.rect(x, y, w, h);
    this.stroke();
    this._path = saved; this._sub = savedSub;
  };
  // Accepted but not rasterized.
  ['clip', 'drawImage', 'drawFocusIfNeeded'].forEach(function (name) { Context2D.prototype[name] = noop; });

  function Canvas(w, h) {
    this._w = w | 0;
    this._h = h | 0;
    this._id = host.canvasCreate(this._w, this._h);
    this._ctx = null;
    this.style = {};
  }
  Object.defineProperty(Canvas.prototype, 'width', {
    get: function () { return this._w; },
    set: function (v) { this._w = v | 0; host.canvasResize(this._id, this._w, this._h); }
  });
  Object.defineProperty(Canvas.prototype, 'height', {
    get: function () { return this._h; },
    set: function (v) { this._h = v | 0; host.canvasResize(this._id, this._w, this._h); }
  });
  Canvas.prototype.getContext = function (kind) {
    if (kind !== '2d') { return null; }
    return this._ctx || (this._ctx = new Context2D(this));
  };
  Canvas.prototype.toDataURL = function () { return host.canvasToDataURL(this._id); };
  Canvas.prototype.getBoundingClientRect = function () {
    return { left: 0, top: 0, right: this._w, bottom: this._h, width: this._w, height: this._h };
  };

  // Vega only uses document.createElement('canvas') in headless rendering;
  // any other element is a minimal inert object.
  global.document = {
    createElement: function (tag) {
      if (String(tag).toLowerCase() === 'canvas') { return new Canvas(300, 150); }
      return { style: {}, setAttribute: noop, appendChild: noop, removeChild: noop, childNodes: [] };
    }
  };

  // Minimal Image so that image marks carry their (sanitized) URL through to
  // the SVG output. No pixels are fetched; width/height come from the mark.
  if (typeof global.Image === 'undefined') {
    global.Image = function Image() {
      this.complete = false;
      this.width = 0;
      this.height = 0;
      this.onload = null;
      this.onerror = null;
      this._src = '';
    };
    Object.defineProperty(global.Image.prototype, 'src', {
      get: function () { return this._src; },
      set: function (v) {
        this._src = String(v);
        this.complete = true;
        var self = this;
        Promise.resolve().then(function () { if (self.onload) { self.onload(); } });
      }
    });
  }

  // Interactive specs bind event streams to the window ("source": "window"
  // for drags and scroll zooms). Nothing can fire them here, but Vega warns
  // when the source does not exist, so provide one that accepts listeners
  // and never calls them. Only the properties Vega reads are defined.
  if (typeof global.window === 'undefined') {
    global.window = {
      addEventListener: function () {},
      removeEventListener: function () {},
      location: { href: '', hash: '' },
      screen: {}
    };
  }

  // goja's Number.prototype.toLocaleString takes its first argument as a
  // radix, so the locale tag d3-format passes for very large values
  // (x.toLocaleString("en")) throws a RangeError. Format the way the "en"
  // locale does: grouped thousands, at most three fraction digits, and the
  // infinity sign for non-finite values.
  Number.prototype.toLocaleString = function () {
    var x = Number(this);
    if (x !== x) { return 'NaN'; }
    if (x === Infinity) { return '∞'; }
    if (x === -Infinity) { return '-∞'; }
    var neg = x < 0 || (x === 0 && 1 / x < 0);
    var a = Math.abs(x), s;
    if (a >= 1e21) {
      // toFixed switches to exponential notation here; expand it by hand.
      var m = /^(\d)(?:\.(\d+))?e\+(\d+)$/.exec(a.toExponential(20));
      var digits = m[1] + (m[2] || ''), exp = +m[3];
      while (digits.length < exp + 1) { digits += '0'; }
      s = digits.slice(0, exp + 1);
    } else {
      s = a.toFixed(3).replace(/\.?0+$/, '');
    }
    var dot = s.indexOf('.');
    var int = dot < 0 ? s : s.slice(0, dot), frac = dot < 0 ? '' : s.slice(dot);
    int = int.replace(/\B(?=(\d{3})+(?!\d))/g, ',');
    return (neg ? '-' : '') + int + frac;
  };

  // goja has no structuredClone; Vega-Lite uses it to deep-copy specs and
  // configs. This covers the value types that occur there.
  if (typeof global.structuredClone !== 'function') {
    global.structuredClone = function clone(v) {
      if (v === null || typeof v !== 'object') { return v; }
      if (v instanceof Date) { return new Date(v.getTime()); }
      if (v instanceof RegExp) { return new RegExp(v.source, v.flags); }
      if (Array.isArray(v)) {
        var arr = new Array(v.length);
        for (var i = 0; i < v.length; i++) { arr[i] = clone(v[i]); }
        return arr;
      }
      if (v instanceof Map) {
        var m = new Map();
        v.forEach(function (val, key) { m.set(clone(key), clone(val)); });
        return m;
      }
      if (v instanceof Set) {
        var st = new Set();
        v.forEach(function (val) { st.add(clone(val)); });
        return st;
      }
      if (ArrayBuffer.isView(v)) { return v.slice(); }
      var out = {};
      var keys = Object.keys(v);
      for (var k = 0; k < keys.length; k++) { out[keys[k]] = clone(v[keys[k]]); }
      return out;
    };
  }
})(globalThis);
