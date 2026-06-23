// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"math/rand"
	"net"
)

// PacketPaddingConfig controls MTU fragmentation and packet padding
// to disrupt DPI signature analysis of packet length patterns.
type PacketPaddingConfig struct {
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
	// If empty, a default name "X-Padding" is used. The header value contains
	// random bytes encoded as hex.
	PaddingHeader string
}

// GeneratePadding returns random padding bytes of the configured length range.
func GeneratePadding(cfg PacketPaddingConfig) []byte {
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

	n := min + rand.Intn(max-min+1) //nolint:gosec
	padding := make([]byte, n)
	rand.Read(padding) //nolint:errcheck
	return padding
}

// PaddingHeaderName returns the header name for padding, defaulting to "X-Padding".
func PaddingHeaderName(cfg PacketPaddingConfig) string {
	if cfg.PaddingHeader != "" {
		return cfg.PaddingHeader
	}

	return "X-Padding"
}

// wrapWithMSSLimit wraps a connection with TCP MSS limiting.
// This forces TCP to fragment data into smaller segments, disrupting
// DPI analysis of packet length signatures during TLS handshake and
// initial data transfer.
func wrapWithMSSLimit(conn net.Conn, mss int) net.Conn {
	if mss <= 0 {
		return conn
	}

	if tc, ok := conn.(*net.TCPConn); ok {
		raw, err := tc.SyscallConn()
		if err != nil {
			return conn
		}

		_ = raw.Control(func(fd uintptr) {
			setTCPMaxSeg(fd, mss)
		})
	}

	return conn
}
