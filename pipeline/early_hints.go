// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

const maxEarlyHintsPreconnect = 4

// ProcessEarlyHints inspects HTTP 103 Early Hints response headers and prewarms connections
// for target origins declared in 'Link: <url>; rel=preconnect' headers.
func ProcessEarlyHints(
	ctx context.Context,
	headers http.Header,
	prewarmFn func(ctx context.Context, targetURL string),
) {
	if len(headers) == 0 || prewarmFn == nil {
		return
	}

	linkHeaders := headers.Values("Link")
	if len(linkHeaders) == 0 {
		if rawLink := headers.Get("Link"); rawLink != "" {
			linkHeaders = []string{rawLink}
		}
	}

	targets := extractPreconnectOrigins(linkHeaders)
	if len(targets) > maxEarlyHintsPreconnect {
		targets = targets[:maxEarlyHintsPreconnect]
	}

	for _, target := range targets {
		go prewarmFn(ctx, target)
	}
}

func extractPreconnectOrigins(linkHeaders []string) []string {
	var targets []string

	for _, headerVal := range linkHeaders {
		for link := range strings.SplitSeq(headerVal, ",") {
			link = strings.TrimSpace(link)
			if !strings.Contains(link, "rel=preconnect") && !strings.Contains(link, "rel=\"preconnect\"") {
				continue
			}

			targetURL := extractURLFromLink(link)
			if targetURL != "" {
				targets = append(targets, targetURL)
			}
		}
	}

	return targets
}

func extractURLFromLink(link string) string {
	start := strings.IndexByte(link, '<')

	end := strings.IndexByte(link, '>')
	if start == -1 || end == -1 || end <= start+1 {
		return ""
	}

	rawURL := strings.TrimSpace(link[start+1 : end])
	if u, err := url.Parse(rawURL); err == nil && u.Host != "" {
		totalLen := len(u.Scheme) + 3 + len(u.Host)
		if totalLen <= 128 {
			var buf [128]byte

			n := copy(buf[:], u.Scheme)
			copy(buf[n:], "://")
			n += 3
			copy(buf[n:], u.Host)

			return string(buf[:totalLen])
		}

		return u.Scheme + "://" + u.Host
	}

	return ""
}
