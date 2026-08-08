// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine

import (
	"net/http"
	"net/url"
	"strings"
)

// PreparedConfig holds immutable precomputed configuration values, wire-format header blocks,
// ALPN token slices, and client hint headers. Since it is constructed once during client initialization,
// it is completely read-only and requires ZERO mutex lock synchronization during requests.
type PreparedConfig struct {
	BaseURL                *url.URL
	BaseURLString          string
	BaseURLTrimmedString   string
	DefaultHostHeader      string
	DefaultALPN            []string
	StaticHeaders          http.Header
	RawHeaderBlock         []byte
	PrecomputedClientHints []byte
}

// NewPreparedConfig constructs an immutable [PreparedConfig].
func NewPreparedConfig(baseURL *url.URL, headers ...http.Header) PreparedConfig {
	prep := PreparedConfig{
		BaseURL:       baseURL,
		DefaultALPN:   []string{"h3", "h2", "http/1.1"},
		StaticHeaders: make(http.Header),
	}

	if baseURL != nil {
		prep.BaseURLString = baseURL.String()
		prep.BaseURLTrimmedString = strings.TrimSuffix(prep.BaseURLString, "/")
		prep.DefaultHostHeader = baseURL.Host
	}

	if len(headers) > 0 && headers[0] != nil {
		for k, v := range headers[0] {
			prep.StaticHeaders[k] = append([]string(nil), v...)
		}

		prep.RawHeaderBlock = buildRawHeaderBlock(prep.StaticHeaders)
		prep.PrecomputedClientHints = extractClientHints(prep.StaticHeaders)
	}

	return prep
}

// MergeHeaders merges static precomputed headers into target [http.Header] without re-allocating keys or locking.
func (p *PreparedConfig) MergeHeaders(target http.Header) {
	if p == nil || len(p.StaticHeaders) == 0 || target == nil {
		return
	}

	for k, vals := range p.StaticHeaders {
		if _, exists := target[k]; !exists {
			target[k] = vals
		}
	}
}

func buildRawHeaderBlock(h http.Header) []byte {
	var sb strings.Builder
	for k, vals := range h {
		for _, v := range vals {
			sb.WriteString(k)
			sb.WriteString(": ")
			sb.WriteString(v)
			sb.WriteString("\r\n")
		}
	}

	return []byte(sb.String())
}

func extractClientHints(h http.Header) []byte {
	var sb strings.Builder
	for k, vals := range h {
		if strings.HasPrefix(strings.ToLower(k), "sec-ch-ua") {
			for _, v := range vals {
				sb.WriteString(k)
				sb.WriteString(": ")
				sb.WriteString(v)
				sb.WriteString("\r\n")
			}
		}
	}

	return []byte(sb.String())
}
