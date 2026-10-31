// Copyright (c) 2026, Gravwell, Inc.
// All rights reserved.
//
// Use of this source code is governed by a BSD 2-Clause license that can be
// found in the LICENSE file.

package govega

import "testing"

func TestBundlePatchesApply(t *testing.T) {
	if _, err := New(); err != nil {
		t.Fatal(err)
	}
	if len(sharedMissingPatches) > 0 {
		t.Fatalf("bundle patches did not match the embedded Vega %s: %v", Version(), sharedMissingPatches)
	}
}
