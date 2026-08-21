// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package netutil

import "github.com/lemon4ksan/aoni/netutil/bearer"

// RFC 6750 §3.1 & §6.2: Standard OAuth 2.0 Bearer error codes in WWW-Authenticate headers.
const (
	// BearerErrInvalidRequest indicates the request is missing a required parameter or malformed (RFC 6750 §3.1).
	BearerErrInvalidRequest = bearer.ErrInvalidRequest

	// BearerErrInvalidToken indicates the access token provided is expired, revoked, or malformed (RFC 6750 §3.1).
	BearerErrInvalidToken = bearer.ErrInvalidToken

	// BearerErrInsufficientScope indicates the request requires higher privileges than provided (RFC 6750 §3.1).
	BearerErrInsufficientScope = bearer.ErrInsufficientScope
)

// BearerChallenge represents a parsed HTTP Bearer authentication challenge (RFC 6750 §3).
// For the dedicated subpackage, see [github.com/lemon4ksan/aoni/netutil/bearer].
type BearerChallenge = bearer.Challenge

// FormatBearerAuth formats an access token into an RFC 6750 Authorization header value ("Bearer <token>").
func FormatBearerAuth(token string) string {
	return bearer.Format(token)
}

// IsValidBearerToken reports whether token conforms to the RFC 6750 §2.1 b64token ABNF production.
func IsValidBearerToken(token string) bool {
	return bearer.IsValidToken(token)
}

// ParseBearerAuth extracts the bearer token from an HTTP Authorization or Proxy-Authorization header (RFC 6750 §2.1).
func ParseBearerAuth(authHeader string) (token string, ok bool) {
	return bearer.Parse(authHeader)
}

// ParseBearerChallenge parses a WWW-Authenticate header containing a Bearer challenge (RFC 6750 §3).
func ParseBearerChallenge(challengeHeader string) (BearerChallenge, bool) {
	return bearer.ParseChallenge(challengeHeader)
}
