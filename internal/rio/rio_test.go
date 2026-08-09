// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rio_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/internal/rio"
)

func TestRIOMemoryRegistration(t *testing.T) {
	buf := make([]byte, 4096)

	reg, err := rio.RegisterBuffer(buf)
	if rio.IsSupported() {
		assert.NoError(t, err)
		assert.NotNil(t, reg)
		reg.Deregister()
	} else {
		// Non-Windows or RIO fallback safe
		_ = err
	}
}
