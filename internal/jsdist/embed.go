// Copyright (c) 2026, Gravwell, Inc.
// All rights reserved.
//
// Use of this source code is governed by a BSD 2-Clause license that can be
// found in the LICENSE file.

// Package jsdist embeds the upstream Vega and Vega-Lite JavaScript bundles.
//
// The bundles are the unmodified UMD builds published to npm as
// vega@<Version>/build/vega.min.js and
// vega-lite@<LiteVersion>/build/vega-lite.min.js, except that the trailing
// sourceMappingURL comment has been removed from vega-lite.min.js, plus the
// vega-projection-extended bundle that the Vega examples site serves as
// vega-projections.min.js. All are licensed under the BSD-3-Clause license;
// see LICENSE.vega and LICENSE.vega-lite in this directory and the LICENSES
// file at the repository root.
package jsdist

import _ "embed"

// Version is the upstream Vega release embedded in this package.
const Version = "6.4.0"

// LiteVersion is the upstream Vega-Lite release embedded in this package.
const LiteVersion = "6.4.3"

// VegaJS is the source text of the Vega UMD bundle.
//
//go:embed vega.min.js
var VegaJS string

// VegaLiteJS is the source text of the Vega-Lite UMD bundle. It expects the
// `vega` global to be defined.
//
//go:embed vega-lite.min.js
var VegaLiteJS string

// ProjectionsVersion is the vega-projection-extended release embedded as
// ProjectionsJS.
const ProjectionsVersion = "3.1.3"

// ProjectionsJS registers the d3-geo-projection projection types (airy,
// mollweide, robinson, ...) with Vega. It is the bundle the Vega examples
// site loads as https://vega.github.io/vega/vega-projections.min.js and
// expects the `vega` global to be defined.
//
//go:embed vega-projections.min.js
var ProjectionsJS string
