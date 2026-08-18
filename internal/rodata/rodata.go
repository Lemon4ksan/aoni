// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package rodata provides static read-only byte slices stored in the binary's .rodata segment
// for zero-allocation HTTP header framing and parsing.
package rodata

import "github.com/lemon4ksan/aoni/foundation/bytesconv"

// Precompiled HTTP/1.1 and HTTP/2 static pseudo-header keys.
var (
	PseudoMethod    = []byte(":method")
	PseudoAuthority = []byte(":authority")
	PseudoScheme    = []byte(":scheme")
	PseudoPath      = []byte(":path")
	PseudoStatus    = []byte(":status")
)

// Precompiled HTTP header canonical & lower-case keys.
var (
	KeyContentType             = []byte("content-type")
	KeyAcceptEncoding          = []byte("accept-encoding")
	KeyAcceptLanguage          = []byte("accept-language")
	KeyAccept                  = []byte("accept")
	KeyUserAgent               = []byte("user-agent")
	KeyCookie                  = []byte("cookie")
	KeySetCookie               = []byte("set-cookie")
	KeyConnection              = []byte("connection")
	KeyPriority                = []byte("priority")
	KeyHost                    = []byte("host")
	KeyReferer                 = []byte("referer")
	KeyUpgradeInsecureRequests = []byte("upgrade-insecure-requests")
	KeySecChUa                 = []byte("sec-ch-ua")
	KeySecChUaMobile           = []byte("sec-ch-ua-mobile")
	KeySecChUaPlatform         = []byte("sec-ch-ua-platform")
	KeySecFetchDest            = []byte("sec-fetch-dest")
	KeySecFetchMode            = []byte("sec-fetch-mode")
	KeySecFetchSite            = []byte("sec-fetch-site")
	KeySecFetchUser            = []byte("sec-fetch-user")
	KeyContentLength           = []byte("content-length")
	KeyServer                  = []byte("server")
	KeyDate                    = []byte("date")
	KeyCacheControl            = []byte("cache-control")
)

// Precompiled common HTTP header values.
var (
	ValApplicationJSON      = []byte("application/json")
	ValApplicationForm      = []byte("application/x-www-form-urlencoded")
	ValAcceptEncodingGzip   = []byte("gzip, deflate, br, zstd")
	ValConnectionKeepAlive  = []byte("keep-alive")
	ValSecFetchDestDoc      = []byte("document")
	ValSecFetchModeNav      = []byte("navigate")
	ValSecFetchSiteSame     = []byte("same-origin")
	ValSecFetchSiteNone     = []byte("none")
	ValSecFetchSiteCross    = []byte("cross-site")
	ValSecFetchUserQuestion = []byte("?1")
	ValSecChUaMobileFalse   = []byte("?0")
)

// InternKey returns a static .rodata byte slice for key if recognized, or nil if not matched.
// It is case-insensitive for standard ASCII header keys.
func InternKey(key string) []byte {
	return InternKeyBytes(bytesconv.S2B(key))
}

// InternKeyBytes returns a static .rodata byte slice for key if recognized, or nil if not matched.
func InternKeyBytes(key []byte) []byte {
	if len(key) == 0 {
		return nil
	}

	switch key[0] {
	case ':':
		switch string(key) {
		case ":method":
			return PseudoMethod
		case ":authority":
			return PseudoAuthority
		case ":scheme":
			return PseudoScheme
		case ":path":
			return PseudoPath
		case ":status":
			return PseudoStatus
		}

	case 'a', 'A':
		if equalsIgnoreCase(key, KeyAcceptEncoding) {
			return KeyAcceptEncoding
		}

		if equalsIgnoreCase(key, KeyAcceptLanguage) {
			return KeyAcceptLanguage
		}

		if equalsIgnoreCase(key, KeyAccept) {
			return KeyAccept
		}

	case 'c', 'C':
		if equalsIgnoreCase(key, KeyContentType) {
			return KeyContentType
		}

		if equalsIgnoreCase(key, KeyCookie) {
			return KeyCookie
		}

		if equalsIgnoreCase(key, KeyConnection) {
			return KeyConnection
		}

		if equalsIgnoreCase(key, KeyContentLength) {
			return KeyContentLength
		}

		if equalsIgnoreCase(key, KeyCacheControl) {
			return KeyCacheControl
		}

	case 'h', 'H':
		if equalsIgnoreCase(key, KeyHost) {
			return KeyHost
		}
	case 'p', 'P':
		if equalsIgnoreCase(key, KeyPriority) {
			return KeyPriority
		}
	case 'r', 'R':
		if equalsIgnoreCase(key, KeyReferer) {
			return KeyReferer
		}
	case 's', 'S':
		if equalsIgnoreCase(key, KeySetCookie) {
			return KeySetCookie
		}

		if equalsIgnoreCase(key, KeyServer) {
			return KeyServer
		}

		if equalsIgnoreCase(key, KeySecChUa) {
			return KeySecChUa
		}

		if equalsIgnoreCase(key, KeySecChUaMobile) {
			return KeySecChUaMobile
		}

		if equalsIgnoreCase(key, KeySecChUaPlatform) {
			return KeySecChUaPlatform
		}

		if equalsIgnoreCase(key, KeySecFetchDest) {
			return KeySecFetchDest
		}

		if equalsIgnoreCase(key, KeySecFetchMode) {
			return KeySecFetchMode
		}

		if equalsIgnoreCase(key, KeySecFetchSite) {
			return KeySecFetchSite
		}

		if equalsIgnoreCase(key, KeySecFetchUser) {
			return KeySecFetchUser
		}

	case 'u', 'U':
		if equalsIgnoreCase(key, KeyUserAgent) {
			return KeyUserAgent
		}

		if equalsIgnoreCase(key, KeyUpgradeInsecureRequests) {
			return KeyUpgradeInsecureRequests
		}
	}

	return nil
}

// InternValue returns a static .rodata byte slice for val if recognized, or nil if not matched.
func InternValue(val string) []byte {
	return InternValueBytes(bytesconv.S2B(val))
}

// InternValueBytes returns a static .rodata byte slice for val if recognized, or nil if not matched.
func InternValueBytes(val []byte) []byte {
	switch string(val) {
	case "application/json":
		return ValApplicationJSON
	case "application/x-www-form-urlencoded":
		return ValApplicationForm
	case "gzip, deflate, br, zstd":
		return ValAcceptEncodingGzip
	case "keep-alive":
		return ValConnectionKeepAlive
	case "document":
		return ValSecFetchDestDoc
	case "navigate":
		return ValSecFetchModeNav
	case "same-origin":
		return ValSecFetchSiteSame
	case "none":
		return ValSecFetchSiteNone
	case "cross-site":
		return ValSecFetchSiteCross
	case "?1":
		return ValSecFetchUserQuestion
	case "?0":
		return ValSecChUaMobileFalse
	default:
		return nil
	}
}

// equalsIgnoreCase reports whether ASCII byte slices a and b are equal ignoring case.
func equalsIgnoreCase(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}

	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}

		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}

		if ca != cb {
			return false
		}
	}

	return true
}
