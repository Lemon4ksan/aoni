// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sys_test

import (
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"

	"github.com/lemon4ksan/aoni/internal/sys"
)

func TestInspectFeatures(t *testing.T) {
	t.Parallel()

	feats := sys.InspectFeatures()
	assert.Greater(t, feats.NumCPU, 0)
	assert.Greater(t, feats.PageSize, 0)
}

func TestApplyCPUAffinity(t *testing.T) {
	t.Parallel()

	// Safe no-op on empty slice
	sys.ApplyCPUAffinity(nil)
	sys.ApplyCPUAffinity([]int{})

	// Safe valid core pinning
	sys.ApplyCPUAffinity([]int{0})
}
