// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bearer_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/netutil/bearer"
)

func TestBearer_FormatAndParse_RFC6750(t *testing.T) {
	t.Parallel()

	const sampleToken = "mF_9.B5f-4.1JqM"

	// Formatting
	formatted := bearer.Format(sampleToken)
	assert.Equal(t, "Bearer mF_9.B5f-4.1JqM", formatted)

	// Parsing valid
	token, ok := bearer.Parse(formatted)
	assert.True(t, ok)
	assert.Equal(t, sampleToken, token)

	// Case-insensitive scheme
	tokenCase, okCase := bearer.Parse("bearer " + sampleToken)
	assert.True(t, okCase)
	assert.Equal(t, sampleToken, tokenCase)

	// Invalid scheme
	_, okBadScheme := bearer.Parse("Basic dXNlcjpwYXNz")
	assert.False(t, okBadScheme)

	// Invalid characters (b64token ABNF violation)
	_, okBadToken := bearer.Parse("Bearer invalid token with spaces")
	assert.False(t, okBadToken)

	_, okBadChar := bearer.Parse("Bearer token#with@invalid$chars")
	assert.False(t, okBadChar)
}

func TestBearer_IsValidToken_RFC6750(t *testing.T) {
	t.Parallel()

	// Valid b64token characters
	assert.True(t, bearer.IsValidToken("v1.2.3_token-abc~xyz+/=="))
	assert.True(t, bearer.IsValidToken("mF_9.B5f-4.1JqM"))
	assert.True(
		t,
		bearer.IsValidToken("eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.e30.t-IDcSemACt8x4iTMCda8Yhe3iZaWbvV5XKSTbuAn0M"),
	)

	// Invalid tokens
	assert.False(t, bearer.IsValidToken(""))
	assert.False(t, bearer.IsValidToken("token with spaces"))
	assert.False(t, bearer.IsValidToken("invalid=token=with=extra=equals")) // padding in the middle
	assert.False(t, bearer.IsValidToken("token\x00with\x1fctrl"))
}

func TestBearer_ParseChallenge_RFC6750(t *testing.T) {
	t.Parallel()

	// RFC 6750 §3 example:
	// Bearer realm="example", error="invalid_token", error_description="The access token expired"
	header := `Bearer realm="example", error="invalid_token", error_description="The access token expired"`
	ch, ok := bearer.ParseChallenge(header)
	assert.True(t, ok)
	assert.Equal(t, "example", ch.Realm)
	assert.Equal(t, bearer.ErrInvalidToken, ch.Error)
	assert.Equal(t, "The access token expired", ch.ErrorDescription)
	assert.Empty(t, ch.Scope)
	assert.Empty(t, ch.ErrorURI)
	assert.Equal(t, header, ch.String())

	// Scope and URI challenge
	headerFull := `Bearer realm="api", scope="read write", error="insufficient_scope", error_description="higher privileges required", error_uri="https://example.com/docs/errors"`
	chFull, okFull := bearer.ParseChallenge(headerFull)
	assert.True(t, okFull)
	assert.Equal(t, "api", chFull.Realm)
	assert.Equal(t, "read write", chFull.Scope)
	assert.Equal(t, bearer.ErrInsufficientScope, chFull.Error)
	assert.Equal(t, "higher privileges required", chFull.ErrorDescription)
	assert.Equal(t, "https://example.com/docs/errors", chFull.ErrorURI)
	assert.Equal(t, headerFull, chFull.String())

	// Bare "Bearer" challenge
	chBare, okBare := bearer.ParseChallenge("Bearer")
	assert.True(t, okBare)
	assert.Empty(t, chBare.Realm)
	assert.Equal(t, "Bearer", chBare.String())

	// Invalid scheme
	_, okInvalid := bearer.ParseChallenge(`Basic realm="example"`)
	assert.False(t, okInvalid)
}
