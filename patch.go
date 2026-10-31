// Copyright (c) 2026, Gravwell, Inc.
// All rights reserved.
//
// Use of this source code is governed by a BSD 2-Clause license that can be
// found in the LICENSE file.

package govega

import "strings"

// bundlePatch is a textual substitution applied to an embedded JavaScript
// bundle before compilation. The embedded files stay byte-identical to the
// upstream releases; patches exist only to work around interpreter
// characteristics that differ from V8.
type bundlePatch struct {
	name string
	from string
	to   string
}

// vegaPatches are applied to vega.min.js.
var vegaPatches = []bundlePatch{
	{
		// vega-scenegraph's SVG string renderer builds the document by
		// appending to a single string. V8 represents such strings as ropes,
		// but goja copies on every concatenation, which makes rendering a
		// large scene quadratic (a 12 MB SVG took 98 s). Collect the chunks in
		// an array and join once instead.
		name: "svg-markup-builder",
		from: `function c_(){let t="",e="",n="";const r=[],i=()=>e=n="",o=(t,n)=>{var r;return null!=n&&(e+=` + "`" + ` ${t}="${r=n,l_(r).replace(/"/g,"&quot;").replace(/\t/g,"&#x9;").replace(/\n/g,"&#xA;").replace(/\r/g,"&#xD;")}"` + "`" + `),a},a={open(s,...u){(o=>{e&&(t+=` + "`" + `${e}>${n}` + "`" + `,i()),r.push(o)})(s),e="<"+s;for(const t of u)for(const e in t)o(e,t[e]);return a},close(){const o=r.pop();return t+=e?e+(n?` + "`" + `>${n}</${o}>` + "`" + `:"/>"):` + "`" + `</${o}>` + "`" + `,i(),a},attr:o,text:t=>(n+=l_(t),a),toString:()=>t};return a}`,
		to:   `function c_(){const T=[];let e="",n="";const r=[],i=()=>e=n="",o=(t,n)=>{var r;return null!=n&&(e+=` + "`" + ` ${t}="${r=n,l_(r).replace(/"/g,"&quot;").replace(/\t/g,"&#x9;").replace(/\n/g,"&#xA;").replace(/\r/g,"&#xD;")}"` + "`" + `),a},a={open(s,...u){(o=>{e&&(T.push(` + "`" + `${e}>${n}` + "`" + `),i()),r.push(o)})(s),e="<"+s;for(const t of u)for(const e in t)o(e,t[e]);return a},close(){const o=r.pop();return T.push(e?e+(n?` + "`" + `>${n}</${o}>` + "`" + `:"/>"):` + "`" + `</${o}>` + "`" + `),i(),a},attr:o,text:t=>(n+=l_(t),a),toString:()=>T.join("")};return a}`,
	},
	{
		// d3-path's Path builds the SVG path data string the same way, so a
		// single line mark with tens of thousands of points is quadratic too.
		// Collect segments in an array and join in toString().
		name: "d3-path-append",
		from: `function Yl(t){this._+=t[0];for(let e=1,n=t.length;e<n;++e)this._+=arguments[e]+t[e]}let Gl=class{constructor(t){this._x0=this._y0=this._x1=this._y1=null,this._="",this._append=null==t?Yl:function(t){let e=Math.floor(t);if(!(e>=0))throw new Error(` + "`" + `invalid digits: ${t}` + "`" + `);if(e>15)return Yl;const n=10**e;return function(t){this._+=t[0];for(let e=1,r=t.length;e<r;++e)this._+=Math.round(arguments[e]*n)/n+t[e]}}(t)}`,
		to:   `function Yl(t){this._.push(t[0]);for(let e=1,n=t.length;e<n;++e)this._.push(arguments[e]+t[e])}let Gl=class{constructor(t){this._x0=this._y0=this._x1=this._y1=null,this._=[],this._append=null==t?Yl:function(t){let e=Math.floor(t);if(!(e>=0))throw new Error(` + "`" + `invalid digits: ${t}` + "`" + `);if(e>15)return Yl;const n=10**e;return function(t){this._.push(t[0]);for(let e=1,r=t.length;e<r;++e)this._.push(Math.round(arguments[e]*n)/n+t[e])}}(t)}`,
	},
	{
		name: "d3-path-tostring",
		from: `toString(){return this._}`,
		to:   `toString(){return this._.join("")}`,
	},
}

// applyPatches applies each patch that matches exactly once and reports the
// names of patches that did not match. A non-matching patch is not fatal:
// the bundle still works, only slower, and TestBundlePatchesApply guards
// against silently losing a patch on upgrade.
func applyPatches(src string, patches []bundlePatch) (string, []string) {
	var missing []string
	for _, p := range patches {
		if strings.Count(src, p.from) != 1 {
			missing = append(missing, p.name)
			continue
		}
		src = strings.Replace(src, p.from, p.to, 1)
	}
	return src, missing
}
