// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fingerprint

import (
	"crypto/rand"
	"encoding/binary"
)

// Predefined realistic header pools to mimic popular Cloud/CDN networks.
var (
	// AmazonCDNHeaderPool mimics AWS CloudFront and API Gateway tracing headers.
	AmazonCDNHeaderPool = []string{
		"X-Amz-Trace-Id",
		"X-Amzn-RequestId",
		"X-Amz-Cf-Id",
	}

	// CloudflareHeaderPool mimics Cloudflare proxy and CDN headers.
	CloudflareHeaderPool = []string{
		"CF-RAY",
		"CF-Connecting-IP",
		"CF-Visitor",
		"CF-IPCountry",
	}

	// GenericCDNHeaderPool mixes multiple standard cloud and CDN diagnostics headers.
	GenericCDNHeaderPool = []string{
		"X-Request-ID",
		"X-Trace-ID",
		"X-Edge-Cache-Id",
		"X-Cloud-Trace-Context",
		"X-Correlation-ID",
	}
)

// PaddingConfig controls MTU fragmentation and packet padding
// to disrupt DPI signature analysis of packet length patterns.
type PaddingConfig struct {
	// MaxSegmentSize sets the TCP Maximum Segment Size (MSS) at the socket level.
	// Typical values: 256-512 for strong fragmentation, 1024 for moderate.
	MaxSegmentSize int

	// MinPaddingBytes is the minimum number of random padding bytes added to the request body.
	MinPaddingBytes int

	// MaxPaddingBytes is the maximum number of random padding bytes added.
	MaxPaddingBytes int

	// PaddingHeader is the name of a custom header used to carry padding data.
	// Defaults to "X-Padding" if empty and HeaderPool is empty.
	PaddingHeader string

	// HeaderPool is a list of header names used to carry padding data.
	// A random entry is selected per request to prevent static DPI fingerprints.
	HeaderPool []string
}

// GeneratePadding returns random padding bytes of the configured length range.
func GeneratePadding(cfg PaddingConfig) []byte {
	if cfg.MinPaddingBytes <= 0 && cfg.MaxPaddingBytes <= 0 {
		return nil
	}

	minLen := cfg.MinPaddingBytes
	maxLen := cfg.MaxPaddingBytes

	if minLen <= 0 {
		minLen = 1
	}

	if maxLen < minLen {
		maxLen = minLen
	}

	n := minLen + secureRandomInt(maxLen-minLen+1)
	padding := make([]byte, n)

	_, _ = rand.Read(padding)

	return padding
}

// PaddingHeaderName returns a header name for carrying padding.
// Selects a random entry from HeaderPool if configured, falling back to PaddingHeader or "X-Padding".
func PaddingHeaderName(cfg PaddingConfig) string {
	if len(cfg.HeaderPool) > 0 {
		return cfg.HeaderPool[secureRandomInt(len(cfg.HeaderPool))]
	}

	if cfg.PaddingHeader != "" {
		return cfg.PaddingHeader
	}

	return "X-Padding"
}

// secureRandomInt generates a cryptographically secure random integer in the range [0, maxVal) using crypto/rand.
func secureRandomInt(maxVal int) int {
	if maxVal <= 0 {
		return 0
	}

	var buf [8]byte

	_, _ = rand.Read(buf[:])
	val := binary.BigEndian.Uint64(buf[:])

	return int(val % uint64(maxVal)) //nolint:gosec
}
