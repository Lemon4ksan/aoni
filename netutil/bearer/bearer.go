// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package bearer implements OAuth 2.0 Bearer Token authentication strictly conforming to RFC 6750.
// Core implementation is located in [github.com/lemon4ksan/foundation/net/http/auth].
package bearer

import (
	"github.com/lemon4ksan/foundation/net/http/auth"
)

// Standard OAuth 2.0 Bearer error codes in WWW-Authenticate headers (RFC 6750 §3.1 & §6.2).
const (
	ErrInvalidRequest    = auth.ErrBearerInvalidRequest
	ErrInvalidToken      = auth.ErrBearerInvalidToken
	ErrInsufficientScope = auth.ErrBearerInsufficientScope
)

// Challenge represents a parsed HTTP Bearer authentication challenge from a WWW-Authenticate header (RFC 6750 §3).
type Challenge = auth.BearerChallenge

// Format formats an access token into an RFC 6750 Authorization header value ("Bearer <token>").
func Format(token string) string {
	return auth.FormatBearer(token)
}

// IsValidToken reports whether token conforms to the RFC 6750 §2.1 b64token ABNF production.
func IsValidToken(token string) bool {
	return auth.IsValidBearerToken(token)
}

// Parse extracts the bearer token from an HTTP Authorization or Proxy-Authorization header (RFC 6750 §2.1).
func Parse(authHeader string) (token string, ok bool) {
	return auth.ParseBearer(authHeader)
}

// ParseChallenge parses a WWW-Authenticate header containing a Bearer challenge (RFC 6750 §3).
func ParseChallenge(challengeHeader string) (Challenge, bool) {
	return auth.ParseBearerChallenge(challengeHeader)
}
