// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package reqpool_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/internal/reqpool"
)

func TestHeaderPoolAcquireRelease(t *testing.T) {
	h := reqpool.AcquireHeader()
	assert.NotNil(t, h)
	h.Set("User-Agent", "aoni/1.0")

	reqpool.ReleaseHeader(h)
}
