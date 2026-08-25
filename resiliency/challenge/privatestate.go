// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package challenge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/lemon4ksan/aoni/netutil/privacypass"
)

// ErrPrivateTokenChallengeDetected indicates an RFC 9577 PrivateToken challenge was received from the Origin.
var ErrPrivateTokenChallengeDetected = errors.New("aoni/challenge: PrivateToken challenge detected in response")

// DetectPrivateTokenChallenge inspects response status codes and authentication headers
// to determine whether an RFC 9577 PrivateToken or W3C Private State Token challenge was returned.
func DetectPrivateTokenChallenge(resp *http.Response) (bool, error) {
	if resp == nil {
		return false, nil
	}

	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		return false, nil
	}

	authHeader := resp.Header.Get(privacypass.HeaderWWWAuthenticate)
	if authHeader != "" &&
		strings.Contains(strings.ToLower(authHeader), strings.ToLower(privacypass.SchemePrivateToken)) {
		return true, ErrPrivateTokenChallengeDetected
	}

	pstHeader := resp.Header.Get(privacypass.HeaderSecPrivateStateToken)
	if pstHeader != "" {
		return true, ErrPrivateTokenChallengeDetected
	}

	return false, nil
}

// PrivateTokenSolver resolves RFC 9577 PrivateToken challenges by acquiring a cryptographic token
// and presenting it via Authorization or Sec-Private-State-Token headers on request retry.
type PrivateTokenSolver struct {
	provider  privacypass.TokenProvider
	transport http.RoundTripper
}

// NewPrivateTokenSolver instantiates a [PrivateTokenSolver] configured with the specified [privacypass.TokenProvider].
func NewPrivateTokenSolver(provider privacypass.TokenProvider, transport http.RoundTripper) *PrivateTokenSolver {
	if transport == nil {
		transport = http.DefaultTransport
	}

	return &PrivateTokenSolver{
		provider:  provider,
		transport: transport,
	}
}

// Solve acquires an RFC 9577 Privacy Pass token matching the detected challenge and retries the request.
func (s *PrivateTokenSolver) Solve(ctx context.Context, _ error, req *http.Request) (*http.Response, error) {
	if s.provider == nil {
		return nil, privacypass.ErrNoTokenAvailable
	}

	if req == nil {
		return nil, errors.New("aoni/challenge: request cannot be nil")
	}

	// Make an initial probe if needed or extract challenge from previous response if stored in context
	origin := req.URL.Host
	if origin == "" {
		origin = req.Host
	}

	// First make an unauthenticated probe to receive the WWW-Authenticate header if not already available
	probeReq := req.Clone(ctx)

	probeResp, err := s.transport.RoundTrip(probeReq)
	if err != nil {
		return nil, fmt.Errorf("aoni/challenge: failed to probe challenge: %w", err)
	}

	detected, _ := DetectPrivateTokenChallenge(probeResp)
	if !detected {
		return probeResp, nil
	}

	authHeader := probeResp.Header.Get(privacypass.HeaderWWWAuthenticate)
	_ = probeResp.Body.Close()

	challenges, err := privacypass.ParseWWWAuthenticate(authHeader)
	if err != nil || len(challenges) == 0 {
		return nil, fmt.Errorf("aoni/challenge: no valid PrivateToken challenges parsed: %w", err)
	}

	var (
		tokenBytes        []byte
		selectedChallenge *privacypass.ChallengeParams
	)

	for _, ch := range challenges {
		if err := privacypass.ValidateChallenge(origin, ch.Challenge); err != nil {
			continue
		}

		tb, err := s.provider.ProvideToken(ctx, origin, ch)
		if err == nil && len(tb) > 0 {
			tokenBytes = tb
			selectedChallenge = ch
			break
		}
	}

	if len(tokenBytes) == 0 || selectedChallenge == nil {
		return nil, privacypass.ErrNoTokenAvailable
	}

	// Clone original request and inject redemption credentials
	redeemReq := req.Clone(ctx)
	authVal := privacypass.FormatAuthorizationToken(tokenBytes)
	redeemReq.Header.Set(privacypass.HeaderAuthorization, authVal)

	// Also set Sec-Private-State-Token for W3C compatibility
	pstVal := privacypass.FormatSecPrivateStateToken(tokenBytes)
	redeemReq.Header.Set(privacypass.HeaderSecPrivateStateToken, pstVal)

	return s.transport.RoundTrip(redeemReq)
}
