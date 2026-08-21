// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package basic implements HTTP Basic Authentication strictly conforming to RFC 7617 (obsoletes RFC 2617).
// Core implementation is located in [github.com/lemon4ksan/foundation/net/http/auth].
package basic

import (
	"github.com/lemon4ksan/foundation/net/http/auth"
)

// Challenge represents an RFC 7617 HTTP Basic Authentication challenge from a WWW-Authenticate header.
type Challenge = auth.BasicChallenge

// Credentials represents user-id and password credentials.
type Credentials struct {
	Username string
	Password string
}

// Format constructs a standard "Authorization: Basic <credentials>" header value (RFC 7617 §2).
func Format(username, password string) string {
	return auth.FormatBasic(username, password)
}

// Parse extracts the username and password from a standard "Authorization: Basic <credentials>" header (RFC 7617 §2).
func Parse(authHeader string) (username, password string, ok bool) {
	return auth.ParseBasic(authHeader)
}

// ParseChallenge extracts the realm and optional charset parameter from a "WWW-Authenticate: Basic ..." header (RFC 7617 §2).
func ParseChallenge(challengeHeader string) (Challenge, bool) {
	return auth.ParseBasicChallenge(challengeHeader)
}

// InScope verifies whether a target request URI falls within the canonical protection space (RFC 7617 §2.2).
func InScope(reqURL, scopeRootURL string) bool {
	return auth.InScope(reqURL, scopeRootURL)
}
