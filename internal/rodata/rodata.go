// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package rodata provides static read-only byte slices stored in the binary's .rodata segment
// for zero-allocation HTTP header framing and parsing.
// Core implementation is located in [github.com/lemon4ksan/foundation/net/http/rodata].
package rodata

import (
	frodata "github.com/lemon4ksan/foundation/net/http/rodata"
)

// Precompiled HTTP/1.1 and HTTP/2 static pseudo-header keys.
var (
	PseudoMethod    = frodata.PseudoMethod
	PseudoAuthority = frodata.PseudoAuthority
	PseudoScheme    = frodata.PseudoScheme
	PseudoPath      = frodata.PseudoPath
	PseudoStatus    = frodata.PseudoStatus
)

// Precompiled HTTP header canonical & lower-case keys.
var (
	KeyContentType             = frodata.KeyContentType
	KeyAcceptEncoding          = frodata.KeyAcceptEncoding
	KeyAcceptLanguage          = frodata.KeyAcceptLanguage
	KeyAccept                  = frodata.KeyAccept
	KeyUserAgent               = frodata.KeyUserAgent
	KeyCookie                  = frodata.KeyCookie
	KeySetCookie               = frodata.KeySetCookie
	KeyConnection              = frodata.KeyConnection
	KeyPriority                = frodata.KeyPriority
	KeyHost                    = frodata.KeyHost
	KeyReferer                 = frodata.KeyReferer
	KeyUpgradeInsecureRequests = frodata.KeyUpgradeInsecureRequests
	KeySecChUa                 = frodata.KeySecChUa
	KeySecChUaMobile           = frodata.KeySecChUaMobile
	KeySecChUaPlatform         = frodata.KeySecChUaPlatform
	KeySecFetchDest            = frodata.KeySecFetchDest
	KeySecFetchMode            = frodata.KeySecFetchMode
	KeySecFetchSite            = frodata.KeySecFetchSite
	KeySecFetchUser            = frodata.KeySecFetchUser
	KeyContentLength           = frodata.KeyContentLength
	KeyServer                  = frodata.KeyServer
	KeyDate                    = frodata.KeyDate
	KeyCacheControl            = frodata.KeyCacheControl
)

// Precompiled common HTTP header values.
var (
	ValApplicationJSON      = frodata.ValApplicationJSON
	ValApplicationForm      = frodata.ValApplicationForm
	ValAcceptEncodingGzip   = frodata.ValAcceptEncodingGzip
	ValConnectionKeepAlive  = frodata.ValConnectionKeepAlive
	ValSecFetchDestDoc      = frodata.ValSecFetchDestDoc
	ValSecFetchModeNav      = frodata.ValSecFetchModeNav
	ValSecFetchSiteSame     = frodata.ValSecFetchSiteSame
	ValSecFetchSiteNone     = frodata.ValSecFetchSiteNone
	ValSecFetchSiteCross    = frodata.ValSecFetchSiteCross
	ValSecFetchUserQuestion = frodata.ValSecFetchUserQuestion
	ValSecChUaMobileFalse   = frodata.ValSecChUaMobileFalse
)

// InternKey returns a static .rodata byte slice for key if recognized, or nil if not matched.
func InternKey(key string) []byte {
	return frodata.InternKey(key)
}

// InternKeyBytes returns a static .rodata byte slice for key if recognized, or nil if not matched.
func InternKeyBytes(key []byte) []byte {
	return frodata.InternKeyBytes(key)
}

// InternValue returns a static .rodata byte slice for val if recognized, or nil if not matched.
func InternValue(val string) []byte {
	return frodata.InternValue(val)
}

// InternValueBytes returns a static .rodata byte slice for val if recognized, or nil if not matched.
func InternValueBytes(val []byte) []byte {
	return frodata.InternValueBytes(val)
}
