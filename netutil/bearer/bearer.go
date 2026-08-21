// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package bearer implements OAuth 2.0 Bearer Token authentication strictly conforming to RFC 6750.
package bearer

import (
	"strings"
)

// Standard OAuth 2.0 Bearer error codes in WWW-Authenticate headers (RFC 6750 §3.1 & §6.2).
const (
	// ErrInvalidRequest indicates the request is missing a required parameter, includes an
	// unsupported parameter, or is otherwise malformed (HTTP 400 Bad Request, RFC 6750 §3.1).
	ErrInvalidRequest = "invalid_request"

	// ErrInvalidToken indicates the access token provided is expired, revoked, or malformed
	// (HTTP 401 Unauthorized, RFC 6750 §3.1).
	ErrInvalidToken = "invalid_token"

	// ErrInsufficientScope indicates the request requires higher privileges than provided
	// by the access token (HTTP 403 Forbidden, RFC 6750 §3.1).
	ErrInsufficientScope = "insufficient_scope"
)

// Challenge represents a parsed HTTP Bearer authentication challenge from a WWW-Authenticate header (RFC 6750 §3).
type Challenge struct {
	// Realm indicates the scope of protection (RFC 6750 §3).
	Realm string

	// Scope is a space-delimited list of case-sensitive scope values required to access the resource (RFC 6750 §3).
	Scope string

	// Error is the machine-readable error code if authentication failed (RFC 6750 §3.1).
	Error string

	// ErrorDescription is a human-readable explanation intended for developers (RFC 6750 §3).
	ErrorDescription string

	// ErrorURI is an absolute URI identifying a human-readable web page explaining the error (RFC 6750 §3).
	ErrorURI string
}

// String formats the Challenge as a standard WWW-Authenticate header value (RFC 6750 §3).
func (c Challenge) String() string {
	var sb strings.Builder
	sb.WriteString("Bearer")

	first := true
	appendParam := func(k, v string) {
		if v == "" {
			return
		}

		if first {
			sb.WriteByte(' ')

			first = false
		} else {
			sb.WriteString(", ")
		}

		sb.WriteString(k)
		sb.WriteString("=\"")
		sb.WriteString(v)
		sb.WriteByte('"')
	}

	appendParam("realm", c.Realm)
	appendParam("scope", c.Scope)
	appendParam("error", c.Error)
	appendParam("error_description", c.ErrorDescription)
	appendParam("error_uri", c.ErrorURI)

	return sb.String()
}

// Format formats an access token into an RFC 6750 Authorization header value ("Bearer <token>").
func Format(token string) string {
	return "Bearer " + strings.TrimSpace(token)
}

// IsValidToken reports whether token conforms to the RFC 6750 §2.1 b64token ABNF production:
// 1*( ALPHA / DIGIT / "-" / "." / "_" / "~" / "+" / "/" ) *"="
func IsValidToken(token string) bool {
	if len(token) == 0 {
		return false
	}

	padding := false
	for i := range len(token) {
		b := token[i]
		if b == '=' {
			padding = true
			continue
		}

		if padding {
			// No non-padding characters allowed after padding started
			return false
		}

		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') {
			continue
		}

		switch b {
		case '-', '.', '_', '~', '+', '/':
			continue
		default:
			return false
		}
	}

	return true
}

// Parse extracts the bearer token from an HTTP Authorization or Proxy-Authorization header (RFC 6750 §2.1).
// Returns the extracted token string and whether the header was a valid Bearer token.
func Parse(authHeader string) (token string, ok bool) {
	authHeader = strings.TrimSpace(authHeader)
	if len(authHeader) < 7 || !strings.EqualFold(authHeader[:7], "Bearer ") {
		return "", false
	}

	token = strings.TrimSpace(authHeader[7:])
	if token == "" || !IsValidToken(token) {
		return "", false
	}

	return token, true
}

// ParseChallenge parses a WWW-Authenticate header containing a Bearer challenge (RFC 6750 §3).
func ParseChallenge(challengeHeader string) (Challenge, bool) {
	challengeHeader = strings.TrimSpace(challengeHeader)
	if len(challengeHeader) < 6 || !strings.EqualFold(challengeHeader[:6], "Bearer") {
		return Challenge{}, false
	}

	rest := strings.TrimSpace(challengeHeader[6:])

	var challenge Challenge
	if rest == "" {
		return challenge, true
	}

	for rest != "" {
		eqIdx := strings.IndexByte(rest, '=')
		if eqIdx == -1 {
			break
		}

		key := strings.ToLower(strings.TrimSpace(rest[:eqIdx]))
		rest = strings.TrimSpace(rest[eqIdx+1:])

		var val string
		if strings.HasPrefix(rest, "\"") {
			rest = rest[1:]

			endQuote := strings.IndexByte(rest, '"')
			if endQuote == -1 {
				val = rest
				rest = ""
			} else {
				val = rest[:endQuote]

				rest = strings.TrimSpace(rest[endQuote+1:])
				if strings.HasPrefix(rest, ",") {
					rest = strings.TrimSpace(rest[1:])
				}
			}
		} else {
			commaIdx := strings.IndexByte(rest, ',')
			if commaIdx == -1 {
				val = strings.TrimSpace(rest)
				rest = ""
			} else {
				val = strings.TrimSpace(rest[:commaIdx])
				rest = strings.TrimSpace(rest[commaIdx+1:])
			}
		}

		switch key {
		case "realm":
			challenge.Realm = val
		case "scope":
			challenge.Scope = val
		case "error":
			challenge.Error = val
		case "error_description":
			challenge.ErrorDescription = val
		case "error_uri":
			challenge.ErrorURI = val
		}
	}

	return challenge, true
}
