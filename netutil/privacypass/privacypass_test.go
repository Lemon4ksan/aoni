// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package privacypass_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/netutil/privacypass"
)

func TestTokenChallenge_EncodingRoundtrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		challenge *privacypass.TokenChallenge
	}{
		{
			name: "single_origin_with_context",
			challenge: &privacypass.TokenChallenge{
				TokenType:         privacypass.TypePubliclyVerifiable,
				IssuerName:        "issuer.example.com",
				RedemptionContext: bytes.Repeat([]byte{0x42}, 32),
				OriginInfo:        "origin.example.com",
			},
		},
		{
			name: "single_origin_empty_context",
			challenge: &privacypass.TokenChallenge{
				TokenType:         privacypass.TypePubliclyVerifiable,
				IssuerName:        "issuer.example.com",
				RedemptionContext: nil,
				OriginInfo:        "origin.example.com",
			},
		},
		{
			name: "cross_origin_empty_context",
			challenge: &privacypass.TokenChallenge{
				TokenType:         privacypass.TypeBlindRSA,
				IssuerName:        "issuer.example.com:8443",
				RedemptionContext: nil,
				OriginInfo:        "",
			},
		},
		{
			name: "multiple_origins_with_context",
			challenge: &privacypass.TokenChallenge{
				TokenType:         privacypass.TypePubliclyVerifiable,
				IssuerName:        "turnstile.cloudflare.com",
				RedemptionContext: bytes.Repeat([]byte{0xaa}, 32),
				OriginInfo:        "site1.com,site2.com,site3.com",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			wire := privacypass.MarshalTokenChallenge(tc.challenge)
			require.NotEmpty(t, wire)

			parsed, err := privacypass.UnmarshalTokenChallenge(wire)
			require.NoError(t, err)
			assert.Equal(t, tc.challenge.TokenType, parsed.TokenType)
			assert.Equal(t, tc.challenge.IssuerName, parsed.IssuerName)
			assert.Equal(t, tc.challenge.RedemptionContext, parsed.RedemptionContext)
			assert.Equal(t, tc.challenge.OriginInfo, parsed.OriginInfo)

			digest := privacypass.ComputeChallengeDigest(tc.challenge)
			assert.NotEmpty(t, digest)
		})
	}
}

func TestToken_EncodingRoundtrip(t *testing.T) {
	t.Parallel()

	token := &privacypass.Token{
		TokenType:     privacypass.TypePubliclyVerifiable,
		TokenKeyID:    bytes.Repeat([]byte{0x01}, 32),
		Authenticator: bytes.Repeat([]byte{0x99}, 64),
	}
	_, err := rand.Read(token.Nonce[:])
	require.NoError(t, err)
	_, err = rand.Read(token.ChallengeDigest[:])
	require.NoError(t, err)

	wire := privacypass.MarshalToken(token)
	require.NotEmpty(t, wire)

	parsed, err := privacypass.UnmarshalToken(wire, 32, 64)
	require.NoError(t, err)
	assert.Equal(t, token.TokenType, parsed.TokenType)
	assert.Equal(t, token.Nonce, parsed.Nonce)
	assert.Equal(t, token.ChallengeDigest, parsed.ChallengeDigest)
	assert.Equal(t, token.TokenKeyID, parsed.TokenKeyID)
	assert.Equal(t, token.Authenticator, parsed.Authenticator)
}

func TestWWWAuthenticate_Parsing(t *testing.T) {
	t.Parallel()

	challenge := &privacypass.TokenChallenge{
		TokenType:         privacypass.TypePubliclyVerifiable,
		IssuerName:        "issuer.example.com",
		RedemptionContext: bytes.Repeat([]byte{0x11}, 32),
		OriginInfo:        "origin.example.com",
	}
	challengeB64 := base64.URLEncoding.EncodeToString(privacypass.MarshalTokenChallenge(challenge))
	tokenKeyB64 := base64.URLEncoding.EncodeToString([]byte("public-key-32-bytes-identifier--"))

	header := `PrivateToken challenge="` + challengeB64 + `", token-key="` + tokenKeyB64 + `", max-age=3600`

	params, err := privacypass.ParseWWWAuthenticate(header)
	require.NoError(t, err)
	require.Len(t, params, 1)

	assert.Equal(t, privacypass.TypePubliclyVerifiable, params[0].Challenge.TokenType)
	assert.Equal(t, "issuer.example.com", params[0].Challenge.IssuerName)
	assert.Equal(t, time.Hour, params[0].MaxAge)
	assert.Equal(t, []byte("public-key-32-bytes-identifier--"), params[0].TokenKey)

	// Validate Challenge Origin
	assert.NoError(t, privacypass.ValidateChallenge("origin.example.com", params[0].Challenge))
	assert.NoError(t, privacypass.ValidateChallenge("https://origin.example.com:443", params[0].Challenge))
	assert.ErrorIs(t, privacypass.ValidateChallenge("attacker.com", params[0].Challenge), privacypass.ErrOriginMismatch)
}

func TestTokenCache_SingleSpend(t *testing.T) {
	t.Parallel()

	cache := privacypass.NewTokenCache()
	challenge := &privacypass.TokenChallenge{
		TokenType:         privacypass.TypePubliclyVerifiable,
		IssuerName:        "turnstile.cloudflare.com",
		RedemptionContext: nil,
		OriginInfo:        "example.com",
	}

	token := &privacypass.Token{
		TokenType: privacypass.TypePubliclyVerifiable,
	}

	cache.Put(challenge, token, []byte("dummy-token-payload"), time.Minute)
	assert.Equal(t, 1, cache.Len())

	// First spend succeeds
	ct, ok := cache.Get(challenge)
	require.True(t, ok)
	assert.Equal(t, []byte("dummy-token-payload"), ct.RawBytes)
	assert.Equal(t, 0, cache.Len())

	// Second spend fails (single-use spent)
	_, ok = cache.Get(challenge)
	assert.False(t, ok)
}

func TestTokenProvider_CachedFallback(t *testing.T) {
	t.Parallel()

	challenge := &privacypass.TokenChallenge{
		TokenType:         privacypass.TypePubliclyVerifiable,
		IssuerName:        "apple-attest.apple.com",
		RedemptionContext: nil,
		OriginInfo:        "target.com",
	}
	cp := &privacypass.ChallengeParams{
		Challenge: challenge,
	}

	staticProv := privacypass.NewStaticProvider()
	staticProv.AddToken(challenge, []byte("apple-pat-token-bytes"))

	cachedProv := privacypass.NewCachedProvider(nil, staticProv, time.Minute)

	// Fallback retrieves token and caches it
	raw, err := cachedProv.ProvideToken(context.Background(), "target.com", cp)
	require.NoError(t, err)
	assert.Equal(t, []byte("apple-pat-token-bytes"), raw)

	// Format Authorization and PST
	authHeader := privacypass.FormatAuthorizationToken(raw)
	assert.Contains(t, authHeader, "PrivateToken token=")

	pstHeader := privacypass.FormatSecPrivateStateToken(raw)
	assert.NotEmpty(t, pstHeader)
}

func TestGreaseTokenTypes(t *testing.T) {
	t.Parallel()

	assert.True(t, privacypass.TokenType(0x02AA).IsGrease())
	assert.True(t, privacypass.TokenType(0xF057).IsGrease())
	assert.False(t, privacypass.TypePubliclyVerifiable.IsGrease())
}
