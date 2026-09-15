// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"net/http"
	"net/url"
	"strings"
)

// PrecomputedHeader stores precomputed header key-value pairs as strings and byte slices
// for zero-allocation header application.
type PrecomputedHeader struct {
	Slice    []string
	KeyBytes []byte
	ValBytes []byte
	Key      string
	Val      string
}

// PreparedConfig holds immutable precomputed configuration values, wire-format header blocks,
// ALPN token slices, and pre-decomposed URI components (RFC 3986 §3).
type PreparedConfig struct {
	BaseURLTrimmedBytes       []byte
	BaseURLCleanPathBytes     []byte
	BaseURLHostBytes          []byte
	BaseURLSchemeBytes        []byte
	RawHeaderBlock            []byte
	PrecomputedClientHints    []byte
	DefaultALPN               []string
	PrecomputedDefaultHeaders []PrecomputedHeader
	BaseURLString             string
	BaseURLTrimmedString      string
	DefaultHostHeader         string
	BaseURL                   *url.URL
	StaticHeaders             http.Header
	FastPathCapable           bool
}

// NewPreparedConfig constructs an immutable [PreparedConfig], pre-parsing the Base URI into its
// scheme (§3.1), host/authority (§3.2), and clean path (§3.3) components for zero-allocation recomposition.
func NewPreparedConfig(baseURL *url.URL, headers ...http.Header) PreparedConfig {
	prep := PreparedConfig{
		BaseURL:         baseURL,
		DefaultALPN:     []string{"h3", "h2", "http/1.1"},
		StaticHeaders:   make(http.Header),
		FastPathCapable: true,
	}

	if baseURL != nil {
		prep.BaseURLString = baseURL.String()
		prep.BaseURLTrimmedString = strings.TrimSuffix(prep.BaseURLString, "/")
		prep.BaseURLTrimmedBytes = []byte(prep.BaseURLTrimmedString)
		prep.BaseURLHostBytes = []byte(baseURL.Host)
		prep.BaseURLSchemeBytes = []byte(baseURL.Scheme)

		prep.DefaultHostHeader = baseURL.Host
		if baseURL.Path != "" && baseURL.Path != "/" {
			prep.BaseURLCleanPathBytes = []byte(strings.TrimSuffix(baseURL.Path, "/"))
		}
	}

	if len(headers) > 0 && headers[0] != nil {
		for k, v := range headers[0] {
			copiedVal := append([]string(nil), v...)

			prep.StaticHeaders[k] = copiedVal
			if len(v) > 0 {
				prep.PrecomputedDefaultHeaders = append(prep.PrecomputedDefaultHeaders, PrecomputedHeader{
					Key:      k,
					Val:      v[0],
					KeyBytes: []byte(k),
					ValBytes: []byte(v[0]),
					Slice:    copiedVal,
				})
			}
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
		if len(k) >= 9 && strings.EqualFold(k[:9], "sec-ch-ua") {
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
