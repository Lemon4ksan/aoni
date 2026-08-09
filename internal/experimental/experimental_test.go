// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package experimental_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/internal/experimental"
)

func TestInspectFeatures(t *testing.T) {
	feats := experimental.InspectFeatures()
	assert.NotNil(t, feats)

	// CPU Affinity safely executes without panics
	experimental.ApplyCPUAffinity([]int{0, 1})
}
