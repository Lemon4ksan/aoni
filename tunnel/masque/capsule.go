// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import (
	"encoding/binary"
	"fmt"
	"net/netip"
)

const (
	// CapsuleAddressAssign specifies capsule type 0x01 per RFC 9484 Section 4.7.1.
	CapsuleAddressAssign uint64 = 0x01

	// CapsuleAddressRequest specifies capsule type 0x02 per RFC 9484 Section 4.7.2.
	CapsuleAddressRequest uint64 = 0x02

	// CapsuleRouteAdvertisement specifies capsule type 0x03 per RFC 9484 Section 4.7.3.
	CapsuleRouteAdvertisement uint64 = 0x03
)

// AssignedAddress represents an assigned IP address/prefix entry in ADDRESS_ASSIGN capsules.
type AssignedAddress struct {
	Addr         netip.Addr
	RequestID    uint64
	IPVersion    byte
	PrefixLength byte
}

// RequestedAddress represents a requested IP address/prefix entry in ADDRESS_REQUEST capsules.
type RequestedAddress struct {
	Addr         netip.Addr
	RequestID    uint64
	IPVersion    byte
	PrefixLength byte
}

// IPAddressRange represents an advertised route range entry in ROUTE_ADVERTISEMENT capsules.
type IPAddressRange struct {
	StartIP    netip.Addr
	EndIP      netip.Addr
	IPVersion  byte
	IPProtocol byte
}

// EncodeVarint encodes v into b using QUIC variable-length integer encoding (RFC 9000).
func EncodeVarint(v uint64, b []byte) int {
	switch {
	case v < 1<<6:
		b[0] = byte(v)
		return 1
	case v < 1<<14:
		binary.BigEndian.PutUint16(b[:2], uint16(v)|0x4000)
		return 2
	case v < 1<<30:
		binary.BigEndian.PutUint32(b[:4], uint32(v)|0x80000000)
		return 4
	default:
		binary.BigEndian.PutUint64(b[:8], v|0xc000000000000000)
		return 8
	}
}

// DecodeVarint decodes a QUIC variable-length integer from b, returning value and byte length.
func DecodeVarint(b []byte) (uint64, int, error) {
	if len(b) == 0 {
		return 0, 0, ErrInvalidCapsule
	}

	first := b[0]
	tag := first >> 6

	switch tag {
	case 0:
		return uint64(first), 1, nil
	case 1:
		if len(b) < 2 {
			return 0, 0, ErrInvalidCapsule
		}

		return uint64(binary.BigEndian.Uint16(b[:2]) & 0x3fff), 2, nil

	case 2:
		if len(b) < 4 {
			return 0, 0, ErrInvalidCapsule
		}

		return uint64(binary.BigEndian.Uint32(b[:4]) & 0x3fffffff), 4, nil

	default:
		if len(b) < 8 {
			return 0, 0, ErrInvalidCapsule
		}

		return binary.BigEndian.Uint64(b[:8]) & 0x3fffffffffffffff, 8, nil
	}
}

// EncodeAddressAssignHeader writes type (0x01) and payload length varints for ADDRESS_ASSIGN capsule.
func EncodeAddressAssignHeader(payloadLen uint64, b []byte) int {
	n1 := EncodeVarint(CapsuleAddressAssign, b)
	n2 := EncodeVarint(payloadLen, b[n1:])
	return n1 + n2
}

// DecodeAddressAssignPayload parses AssignedAddress entries from payload bytes into a new slice.
func DecodeAddressAssignPayload(payload []byte) ([]AssignedAddress, error) {
	entries := make([]AssignedAddress, 0, len(payload)/8)
	return DecodeAddressAssignPayloadTo(payload, entries)
}

// DecodeAddressAssignPayloadTo parses AssignedAddress entries into pre-allocated dst slice achieving 0 B/op.
func DecodeAddressAssignPayloadTo(payload []byte, dst []AssignedAddress) ([]AssignedAddress, error) {
	offset := 0

	for offset < len(payload) {
		reqID, n, err := DecodeVarint(payload[offset:])
		if err != nil {
			return nil, err
		}

		offset += n

		if offset+2 > len(payload) {
			return nil, ErrInvalidCapsule
		}

		ipVer := payload[offset]
		offset++

		var addr netip.Addr
		switch ipVer {
		case 4:
			if offset+4+1 > len(payload) {
				return nil, ErrInvalidCapsule
			}

			var ip4 [4]byte
			copy(ip4[:], payload[offset:offset+4])
			addr = netip.AddrFrom4(ip4)
			offset += 4

		case 6:
			if offset+16+1 > len(payload) {
				return nil, ErrInvalidCapsule
			}

			var ip6 [16]byte
			copy(ip6[:], payload[offset:offset+16])
			addr = netip.AddrFrom16(ip6)
			offset += 16

		default:
			return nil, fmt.Errorf("%w: invalid IP version %d", ErrInvalidCapsule, ipVer)
		}

		prefixLen := payload[offset]
		offset++

		dst = append(dst, AssignedAddress{
			Addr:         addr,
			RequestID:    reqID,
			IPVersion:    ipVer,
			PrefixLength: prefixLen,
		})
	}

	return dst, nil
}
