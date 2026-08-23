// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package weblink implements Web Linking and Link HTTP header serialization strictly conforming to RFC 8288 (obsoletes RFC 5988).
// Core implementation is located in [github.com/lemon4ksan/foundation/net/weblink].
package weblink

import (
	"net/http"

	fweblink "github.com/lemon4ksan/foundation/net/weblink"
)

// Header is the standard HTTP header field name for Web Linking (RFC 8288 §3).
const Header = fweblink.Header

// Standard IANA registered link relation types per RFC 8288 §4.2.
const (
	RelNext        = fweblink.RelNext
	RelPrev        = fweblink.RelPrev
	RelPrevious    = fweblink.RelPrevious
	RelFirst       = fweblink.RelFirst
	RelLast        = fweblink.RelLast
	RelCanonical   = fweblink.RelCanonical
	RelAlternate   = fweblink.RelAlternate
	RelAuthor      = fweblink.RelAuthor
	RelHelp        = fweblink.RelHelp
	RelIcon        = fweblink.RelIcon
	RelLicense     = fweblink.RelLicense
	RelPreload     = fweblink.RelPreload
	RelPrefetch    = fweblink.RelPrefetch
	RelPreconnect  = fweblink.RelPreconnect
	RelDNSPrefetch = fweblink.RelDNSPrefetch
	RelStylesheet  = fweblink.RelStylesheet
	RelService     = fweblink.RelService
	RelPayment     = fweblink.RelPayment
	RelSearch      = fweblink.RelSearch
	RelSelf        = fweblink.RelSelf
	RelUp          = fweblink.RelUp
)

var (
	ErrEmptyHeader   = fweblink.ErrEmptyHeader
	ErrInvalidHeader = fweblink.ErrInvalidHeader
)

// Link represents a typed connection between two Web resources conforming to RFC 8288 §2.
type Link = fweblink.Link

// New creates a new [Link] with target URI and primary relation type.
func New(target, rel string) Link {
	return fweblink.New(target, rel)
}

// Group represents an ordered collection of [Link] objects parsed from HTTP headers (RFC 8288 §3).
type Group = fweblink.Group

// Parse extracts Web Links from a Link HTTP header string according to RFC 8288 Appendix B algorithms.
func Parse(header string) (Group, error) {
	return fweblink.Parse(header)
}

// ParseHeader extracts Web Links from a standard [http.Header] map across all "Link" headers.
func ParseHeader(h http.Header) (Group, error) {
	return fweblink.ParseHeader(h)
}
