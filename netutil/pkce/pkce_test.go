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

	t.Run("default_new_and_validate", func(t *testing.T) {
		t.Parallel()

		pair, err := pkce.New()
		require.NoError(t, err)
		assert.NotEmpty(t, pair.Verifier)
		assert.NotEmpty(t, pair.Challenge)
		assert.True(t, pkce.Validate(pair.Verifier, pair.Challenge, pkce.MethodS256))
		assert.False(t, pkce.Validate(pair.Verifier, "wrong_challenge", pkce.MethodS256))
		assert.False(t, pkce.Validate(pair.Verifier, pair.Challenge, "invalid_method"))
	})

	t.Run("plain_method", func(t *testing.T) {
		t.Parallel()

		verifier, err := pkce.GenerateVerifier(64)
		require.NoError(t, err)
		assert.Len(t, verifier, 64)

		challenge, err := pkce.ComputeChallenge(verifier, pkce.MethodPlain)
		require.NoError(t, err)
		assert.Equal(t, verifier, challenge)
		assert.True(t, pkce.Validate(verifier, challenge, pkce.MethodPlain))
	})

	t.Run("invalid_method_and_length", func(t *testing.T) {
		t.Parallel()

		_, err := pkce.ComputeChallenge("valid_verifier_with_sufficient_length_here_12345", "UNKNOWN")
		assert.ErrorIs(t, err, pkce.ErrInvalidMethod)

		_, err = pkce.GenerateVerifier(10) // less than RFC minimum 43
		assert.ErrorIs(t, err, pkce.ErrVerifierLength)
	})
}
