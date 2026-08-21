// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package basic implements HTTP Basic Authentication strictly conforming to RFC 7617 (obsoletes RFC 2617).
package basic

import (
	"encoding/base64"
	"net/url"
	"strings"

	"github.com/lemon4ksan/foundation/silicon/bytesconv"
)

// Challenge represents an RFC 7617 HTTP Basic Authentication challenge from a WWW-Authenticate header.
type Challenge struct {
	Realm   string
	Charset string
}

// String formats the Challenge as a standard WWW-Authenticate header value (RFC 7617 §2).
func (c Challenge) String() string {
	var sb strings.Builder
	sb.WriteString("Basic realm=\"")
	sb.WriteString(c.Realm)
	sb.WriteByte('"')

	if c.Charset != "" {
		sb.WriteString(", charset=\"")
		sb.WriteString(c.Charset)
		sb.WriteByte('"')
	}

	return sb.String()
}

// Credentials represents user-id and password credentials.
type Credentials struct {
	Username string
	Password string
}

// Format constructs a standard "Authorization: Basic <credentials>" header value (RFC 7617 §2).
//
// Performance & Zero-Allocation Optimization:
// For credentials whose combined length ("username:password") is <= 128 bytes,
// serialization executes entirely on stack buffers without allocating heap memory.
func Format(username, password string) string {
	totalLen := len(username) + 1 + len(password)
	if totalLen <= 128 {
		var buf [128]byte

		n := copy(buf[:], username)
		buf[n] = ':'
		copy(buf[n+1:], password)

		var outBuf [256]byte

		nOut := copy(outBuf[:], "Basic ")
		base64.StdEncoding.Encode(outBuf[nOut:], buf[:totalLen])
		encodedLen := nOut + base64.StdEncoding.EncodedLen(totalLen)

		return string(outBuf[:encodedLen])
	}

	auth := username + ":" + password

	return "Basic " + base64.StdEncoding.EncodeToString(bytesconv.S2B(auth))
}

// Parse extracts the username and password from a standard "Authorization: Basic <credentials>" header (RFC 7617 §2).
func Parse(authHeader string) (username, password string, ok bool) {
	authHeader = strings.TrimSpace(authHeader)
	if !strings.HasPrefix(authHeader, "Basic ") && !strings.HasPrefix(authHeader, "basic ") {
		return "", "", false
	}

	payload := strings.TrimSpace(authHeader[6:])

	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", "", false
	}

	raw := bytesconv.B2S(decoded)

	colonIdx := strings.IndexByte(raw, ':')
	if colonIdx < 0 {
		return "", "", false
	}

	return raw[:colonIdx], raw[colonIdx+1:], true
}

// ParseChallenge extracts the realm and optional charset parameter from a "WWW-Authenticate: Basic ..." header (RFC 7617 §2).
func ParseChallenge(challengeHeader string) (Challenge, bool) {
	challengeHeader = strings.TrimSpace(challengeHeader)
	if !strings.HasPrefix(challengeHeader, "Basic ") && !strings.HasPrefix(challengeHeader, "basic ") {
		return Challenge{}, false
	}

	params := challengeHeader[6:]

	var ch Challenge

	foundRealm := false

	for len(params) > 0 {
		params = strings.TrimLeft(params, " ,")
		if len(params) == 0 {
			break
		}

		eqIdx := strings.IndexByte(params, '=')
		if eqIdx < 0 {
			break
		}

		key := strings.ToLower(strings.TrimSpace(params[:eqIdx]))
		params = params[eqIdx+1:]

		var val string
		if len(params) > 0 && params[0] == '"' {
			params = params[1:]

			endQuote := strings.IndexByte(params, '"')
			if endQuote < 0 {
				val = params
				params = ""
			} else {
				val = params[:endQuote]
				params = params[endQuote+1:]
			}
		} else {
			commaIdx := strings.IndexByte(params, ',')
			if commaIdx < 0 {
				val = strings.TrimSpace(params)
				params = ""
			} else {
				val = strings.TrimSpace(params[:commaIdx])
				params = params[commaIdx+1:]
			}
		}

		switch key {
		case "realm":
			ch.Realm = val
			foundRealm = true
		case "charset":
			ch.Charset = strings.ToUpper(val)
		}
	}

	return ch, foundRealm
}

// InScope verifies whether a target request URI falls within the canonical protection space (RFC 7617 §2.2).
func InScope(reqURL, scopeRootURL string) bool {
	parsedReq, errReq := url.Parse(reqURL)

	parsedScope, errScope := url.Parse(scopeRootURL)
	if errReq != nil || errScope != nil {
		return false
	}

	if !strings.EqualFold(parsedReq.Scheme, parsedScope.Scheme) {
		return false
	}

	if !strings.EqualFold(parsedReq.Host, parsedScope.Host) {
		return false
	}

	scopePath := parsedScope.Path
	if scopePath == "" {
		scopePath = "/"
	}

	if !strings.HasSuffix(scopePath, "/") {
		scopePath += "/"
	}

	reqPath := parsedReq.Path
	if reqPath == "" {
		reqPath = "/"
	}

	if reqPath == parsedScope.Path {
		return true
	}

	return strings.HasPrefix(reqPath, scopePath)
}
