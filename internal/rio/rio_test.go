// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rio_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/rio"
)

func TestRIOMemoryRegistration(t *testing.T) {
	buf := make([]byte, 4096)

	if rio.IsSupported() {
		reg, err := rio.RegisterBuffer(buf)
		require.NoError(t, err)
		require.NotNil(t, reg)
		reg.Deregister()
	} else {
		assert.False(t, rio.IsSupported())
	}
}
