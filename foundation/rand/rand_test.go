// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rand_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	fastrand "github.com/lemon4ksan/aoni/foundation/rand"
)

func TestFastRand(t *testing.T) {
	val := fastrand.Intn(100)
	assert.True(t, val >= 0 && val < 100)

	jitter := fastrand.FastJitter(500 * time.Millisecond)
	assert.True(t, jitter >= 0 && jitter < 500*time.Millisecond)
}
