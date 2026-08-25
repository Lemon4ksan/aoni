// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import (
	"errors"
	"net/netip"
)

var (
	// ErrSpoofedSourceAddress is returned when a packet's source address violates BCP 38 / BCP 84 ingress filtering.
	ErrSpoofedSourceAddress = errors.New("aoni/masque: ingress filter blocked spoofed source address")

	// ErrMartianAddress is returned when a packet contains a reserved, unroutable Martian address (RFC 2827).
	ErrMartianAddress = errors.New("aoni/masque: packet contains reserved martian address")
)

// ValidateIngressSourceAddress verifies that srcAddr belongs to one of allowedPrefixes
// per RFC 9484 Section 11 (Security Considerations) and BCP 38 / BCP 84 (RFC 2827 / RFC 3704 / RFC 8704).
//
// Preconditions:
//   - If allowedPrefixes is empty, validation passes by default (no static prefix constraints).
//   - Executes in 0 B/op zero-allocation time using netip.Prefix.Contains.
func ValidateIngressSourceAddress(srcAddr netip.Addr, allowedPrefixes []netip.Prefix) error {
	if !srcAddr.IsValid() || len(allowedPrefixes) == 0 {
		return nil
	}

	if IsMartianAddr(srcAddr) {
		return ErrMartianAddress
	}

	for _, prefix := range allowedPrefixes {
		if prefix.Contains(srcAddr) {
			return nil
		}
	}

	return ErrSpoofedSourceAddress
}

// IsMartianAddr reports whether addr belongs to reserved, unroutable Martian address ranges per BCP 38 (RFC 2827) and RFC 6890.
func IsMartianAddr(addr netip.Addr) bool {
	if !addr.IsValid() || addr.IsUnspecified() || addr.IsLoopback() || addr.IsMulticast() {
		return true
	}

	if addr.Is4() {
		b := addr.As4()
		return b[0] == 0 || b[0] == 127 || (b[0] >= 224)
	}

	return false
}
