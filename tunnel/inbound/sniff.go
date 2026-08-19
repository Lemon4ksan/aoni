// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inbound

import "bufio"

// ProtocolType identifies the detected inbound proxy protocol from the initial byte stream.
type ProtocolType uint8

const (
	// ProtocolUnknown represents an unrecognized initial byte stream.
	ProtocolUnknown ProtocolType = iota
	// ProtocolSOCKS4 represents SOCKS4 protocol (0x04).
	ProtocolSOCKS4
	// ProtocolSOCKS5 represents SOCKS5 protocol (0x05).
	ProtocolSOCKS5
	// ProtocolHTTP represents HTTP/HTTPS proxy protocol (ASCII HTTP methods).
	ProtocolHTTP
)

// SniffProtocol peeks at the first byte of br to detect SOCKS4, SOCKS5, or HTTP protocols without advancing the stream.
func SniffProtocol(br *bufio.Reader) (ProtocolType, error) {
	peek, err := br.Peek(1)
	if err != nil || len(peek) == 0 {
		return ProtocolUnknown, err
	}

	b := peek[0]
	switch b {
	case 0x05:
		return ProtocolSOCKS5, nil
	case 0x04:
		return ProtocolSOCKS4, nil
	default:
		if isHTTPMethodByte(b) {
			return ProtocolHTTP, nil
		}

		return ProtocolUnknown, nil
	}
}

func isHTTPMethodByte(b byte) bool {
	return b == 'C' || b == 'G' || b == 'P' || b == 'D' || b == 'H' || b == 'O' || b == 'U'
}
