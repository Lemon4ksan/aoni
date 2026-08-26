// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package hints implements RFC 8297 (103 Early Hints) and RFC 8288 (Web Linking) processing
// for speculative connection pre-warming and DNS pre-resolution.
package hints

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	fheader "github.com/lemon4ksan/foundation/net/http/header"
)

// Preconnecter defines an interface for clients capable of proactive TCP/TLS preconnecting
// and DNS pre-resolution.
type Preconnecter interface {
	Preconnect(ctx context.Context, targetURL string) error
	Preresolve(ctx context.Context, host string) error
}

// Link represents a parsed RFC 8288 / RFC 8297 link relation.
type Link struct {
	URI         string
	Rel         string
	As          string
	Crossorigin string
}

// ParseLinkHeader parses an RFC 8288 Link header string into a slice of [Link] objects.
func ParseLinkHeader(raw string) []Link {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	entries := strings.Split(raw, ",")
	links := make([]Link, 0, len(entries))

	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		parts := strings.Split(entry, ";")
		if len(parts) == 0 {
			continue
		}

		targetURI := strings.TrimSpace(parts[0])
		if !strings.HasPrefix(targetURI, "<") || !strings.HasSuffix(targetURI, ">") {
			continue
		}

		link := Link{
			URI: strings.Trim(targetURI, "<>"),
		}

		for _, param := range parts[1:] {
			param = strings.TrimSpace(param)
			k, v, found := strings.Cut(param, "=")
			if !found {
				continue
			}

			k = strings.ToLower(strings.TrimSpace(k))
			v = strings.Trim(strings.TrimSpace(v), "\"")

			switch k {
			case "rel":
				link.Rel = strings.ToLower(v)
			case "as":
				link.As = strings.ToLower(v)
			case "crossorigin":
				link.Crossorigin = strings.ToLower(v)
			}
		}

		if link.URI != "" {
			links = append(links, link)
		}
	}

	return links
}

// ParseLinksFromHeaders extracts and parses all "Link" headers from [http.Header].
func ParseLinksFromHeaders(headers http.Header) []Link {
	if headers == nil {
		return nil
	}

	var allLinks []Link
	for _, val := range headers[fheader.Link] {
		links := ParseLinkHeader(val)
		allLinks = append(allLinks, links...)
	}

	// Also check lowercase canonical "link"
	if len(allLinks) == 0 {
		for k, val := range headers {
			if strings.EqualFold(k, "link") {
				for _, v := range val {
					allLinks = append(allLinks, ParseLinkHeader(v)...)
				}
			}
		}
	}

	return allLinks
}

// ProcessEarlyHints asynchronously processes 103 Early Hints link relations,
// dispatching speculative preconnects and DNS preresolves in the background.
func ProcessEarlyHints(ctx context.Context, p Preconnecter, hints http.Header) {
	if p == nil || hints == nil {
		return
	}

	links := ParseLinksFromHeaders(hints)
	if len(links) == 0 {
		return
	}

	bgCtx := context.WithoutCancel(ctx)

	for _, link := range links {
		switch link.Rel {
		case "preconnect":
			go func(target string) {
				preconnectCtx, cancel := context.WithTimeout(bgCtx, 3*time.Second)
				defer cancel()

				_ = p.Preconnect(preconnectCtx, target)
			}(link.URI)

		case "dns-prefetch":
			go func(target string) {
				preresolveCtx, cancel := context.WithTimeout(bgCtx, 3*time.Second)
				defer cancel()

				u, err := url.Parse(target)
				host := target
				if err == nil && u.Hostname() != "" {
					host = u.Hostname()
				}

				_ = p.Preresolve(preresolveCtx, host)
			}(link.URI)
		}
	}
}
