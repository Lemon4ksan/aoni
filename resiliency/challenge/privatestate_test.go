// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package challenge_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/netutil/privacypass"
	"github.com/lemon4ksan/aoni/resiliency/challenge"
)

func TestDetectPrivateTokenChallenge(t *testing.T) {
	t.Parallel()

	resp401 := &http.Response{
		StatusCode: http.StatusUnauthorized,
		Header: http.Header{
			"Www-Authenticate": []string{`PrivateToken challenge="abc", token-key="key"`},
		},
	}
	detected, err := challenge.DetectPrivateTokenChallenge(resp401)
	require.True(t, detected)
	assert.ErrorIs(t, err, challenge.ErrPrivateTokenChallengeDetected)

	respPST := &http.Response{
		StatusCode: http.StatusForbidden,
		Header: http.Header{
			"Sec-Private-State-Token": []string{"challenge"},
		},
	}
	detected, err = challenge.DetectPrivateTokenChallenge(respPST)
	require.True(t, detected)
	assert.ErrorIs(t, err, challenge.ErrPrivateTokenChallengeDetected)

	resp200 := &http.Response{
		StatusCode: http.StatusOK,
	}
	detected, err = challenge.DetectPrivateTokenChallenge(resp200)
	assert.False(t, detected)
	assert.NoError(t, err)
}

func TestPrivateTokenSolver_E2E(t *testing.T) {
	t.Parallel()

	tokenChallenge := &privacypass.TokenChallenge{
		TokenType:         privacypass.TypePubliclyVerifiable,
		IssuerName:        "cloudflare-turnstile.com",
		RedemptionContext: nil,
		OriginInfo:        "127.0.0.1",
	}
	challengeB64 := base64.URLEncoding.EncodeToString(privacypass.MarshalTokenChallenge(tokenChallenge))

	validToken := []byte("valid-cryptographic-token-signature")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		pst := r.Header.Get("Sec-Private-State-Token")

		if auth == "" && pst == "" {
			w.Header().Set("WWW-Authenticate", `PrivateToken challenge="`+challengeB64+`", max-age=300`)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("WAF PrivateToken challenge required"))

			return
		}

		if auth == privacypass.FormatAuthorizationToken(validToken) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("WAF challenge solved!"))
			return
		}

		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(server.Close)

	tokenCache := privacypass.NewTokenCache()
	staticProv := privacypass.NewStaticProvider()
	staticProv.AddToken(tokenChallenge, validToken)
	cachedProv := privacypass.NewCachedProvider(tokenCache, staticProv, time.Minute)

	solver := challenge.NewPrivateTokenSolver(cachedProv, server.Client().Transport)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	resp, err := solver.Solve(context.Background(), nil, req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "WAF challenge solved!", string(bytes.TrimSpace(body)))
}
