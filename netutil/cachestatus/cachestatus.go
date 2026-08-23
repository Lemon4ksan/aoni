// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package cachestatus implements the Cache-Status HTTP Response Header Field strictly conforming to RFC 9211.
// Core implementation is located in [github.com/lemon4ksan/foundation/net/cachestatus].
package cachestatus

import (
	"net/http"

	fcs "github.com/lemon4ksan/foundation/net/cachestatus"
)

// Header is the standard HTTP response header field name for cache status metadata (RFC 9211 §2).
const Header = fcs.Header

// RFC 9211 §2.2: Standard forward reasons (fwd parameter tokens).
const (
	FwdBypass   = fcs.FwdBypass
	FwdMethod   = fcs.FwdMethod
	FwdURIMiss  = fcs.FwdURIMiss
	FwdVaryMiss = fcs.FwdVaryMiss
	FwdMiss     = fcs.FwdMiss
	FwdRequest  = fcs.FwdRequest
	FwdStale    = fcs.FwdStale
	FwdPartial  = fcs.FwdPartial
)

var (
	ErrEmptyHeader   = fcs.ErrEmptyHeader
	ErrInvalidHeader = fcs.ErrInvalidHeader
)

// Entry represents a single cache node entry in the Cache-Status header field (RFC 9211 §2).
type Entry = fcs.Entry

// Chain represents an ordered chain of [Entry] objects from the Cache-Status header (RFC 9211 §2).
type Chain = fcs.Chain

// Parse extracts and parses all cache status entries from a Cache-Status HTTP header string (RFC 9211 §2).
func Parse(header string) (Chain, error) {
	return fcs.Parse(header)
}

// ParseHeader parses the Cache-Status header from standard [http.Header] map (RFC 9211 §2).
func ParseHeader(h http.Header) (Chain, error) {
	return fcs.ParseHeader(h)
}
