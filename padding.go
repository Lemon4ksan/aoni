// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

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
	// This forces TCP to fragment data into smaller packets, breaking static
	// packet length signatures used by DPI systems. Set to 0 to disable.
	// Typical values: 256-512 for strong fragmentation, 1024 for moderate.
	MaxSegmentSize int

	// MinPaddingBytes is the minimum number of random padding bytes added
	// to the start of the request body. A random value between MinPaddingBytes
	// and MaxPaddingBytes is chosen per request. Set both to 0 to disable padding.
	MinPaddingBytes int

	// MaxPaddingBytes is the maximum number of random padding bytes added.
	MaxPaddingBytes int

	// PaddingHeader is the name of a custom header used to carry padding data.
	// If empty and HeaderPool is empty, a default name "X-Padding" is used.
	// Ignored when HeaderPool is non-empty.
	PaddingHeader string

	// HeaderPool is a list of header names used to carry padding data.
	// On each request a random name is picked from this pool, making the
	// padding header indistinguishable from legitimate CDN or cloud tracing
	// headers. When non-empty this field takes precedence over PaddingHeader.
	HeaderPool []string
}

// GeneratePadding returns random padding bytes of the configured length range.
func GeneratePadding(cfg PaddingConfig) []byte {
	if cfg.MinPaddingBytes <= 0 && cfg.MaxPaddingBytes <= 0 {
		return nil
	}

	min := cfg.MinPaddingBytes
	max := cfg.MaxPaddingBytes

	if min <= 0 {
		min = 1
	}

	if max < min {
		max = min
	}

	n := min + randIntn(max-min+1) //nolint:gosec
	padding := make([]byte, n)

	_, _ = rand.Read(padding)

	return padding
}

// PaddingHeaderName returns a header name for padding.
// If HeaderPool is configured, a random entry is selected to avoid
// creating a static DPI fingerprint. Otherwise PaddingHeader is used,
// falling back to "X-Padding".
func PaddingHeaderName(cfg PaddingConfig) string {
	if len(cfg.HeaderPool) > 0 {
		return cfg.HeaderPool[randIntn(len(cfg.HeaderPool))]
	}

	if cfg.PaddingHeader != "" {
		return cfg.PaddingHeader
	}

	return "X-Padding"
}

// randIntn returns a cryptographically secure random int in [0, n).
func randIntn(n int) int {
	if n <= 0 {
		return 0
	}

	var buf [8]byte

	_, _ = rand.Read(buf[:])

	val := binary.BigEndian.Uint64(buf[:])

	return int(val % uint64(n)) //nolint:gosec
}
