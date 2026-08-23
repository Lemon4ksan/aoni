// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package netutil

import "github.com/lemon4ksan/aoni/netutil/basic"

// BasicChallenge represents a parsed HTTP Basic authentication challenge (RFC 7617 §2).
// For the dedicated subpackage, see [github.com/lemon4ksan/aoni/netutil/basic].
type BasicChallenge = basic.Challenge

// FormatBasicAuth formats username and password into an RFC 7617 HTTP Authorization header value.
// Credentials are formatted as "Basic <base64(user-id:password)>" (RFC 7617 §2).
func FormatBasicAuth(username, password string) string {
	return basic.Format(username, password)
}

// ParseBasicAuth parses an HTTP Authorization or Proxy-Authorization header value containing Basic credentials.
// Returns the extracted username, password, and whether parsing succeeded (RFC 7617 §2).
func ParseBasicAuth(authHeader string) (username, password string, ok bool) {
	return basic.Parse(authHeader)
}

// ParseBasicChallenge parses a "WWW-Authenticate: Basic ..." header value per RFC 7617 §2.
func ParseBasicChallenge(challengeHeader string) (BasicChallenge, bool) {
	return basic.ParseChallenge(challengeHeader)
}

// InBasicAuthScope verifies whether a target request URI falls within the canonical protection space (RFC 7617 §2.2).
func InBasicAuthScope(reqURL, scopeRootURL string) bool {
	return basic.InScope(reqURL, scopeRootURL)
}
