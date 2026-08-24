// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkce_test

import (
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/netutil/pkce"
)

func TestPKCEFacade(t *testing.T) {
	t.Parallel()

	pair, err := pkce.New()
	require.NoError(t, err)
	assert.NotEmpty(t, pair.Verifier)
	assert.NotEmpty(t, pair.Challenge)
	assert.True(t, pkce.Validate(pair.Verifier, pair.Challenge, pkce.MethodS256))
}
