// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package netutil

import (
	"net/http"

	"github.com/lemon4ksan/aoni/netutil/weblink"
)

// HeaderLink is the standard HTTP header field name for Web Linking (RFC 8288 §3).
const HeaderLink = weblink.Header

// WebLink represents a typed connection between two Web resources conforming to RFC 8288 §2.
// For the dedicated subpackage, see [github.com/lemon4ksan/aoni/netutil/weblink].
type WebLink = weblink.Link

// WebLinkGroup represents an ordered collection of Web Links parsed from HTTP headers (RFC 8288 §3).
type WebLinkGroup = weblink.Group

// ParseWebLinks parses a Link HTTP header string according to RFC 8288.
func ParseWebLinks(header string) (WebLinkGroup, error) {
	return weblink.Parse(header)
}

// ParseWebLinksHeader extracts Web Links from a standard [http.Header] map (RFC 8288 Appendix B.1).
func ParseWebLinksHeader(h http.Header) (WebLinkGroup, error) {
	return weblink.ParseHeader(h)
}
