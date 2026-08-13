// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import (
	"fmt"
	"net/netip"

	internalMasque "github.com/lemon4ksan/aoni/internal/masque"
	"github.com/lemon4ksan/aoni/internal/offheap"
)

const (
	// CapsuleAddressAssign specifies capsule type 0x01 per RFC 9484 Section 4.7.1.
	CapsuleAddressAssign uint64 = 0x01

	// CapsuleAddressRequest specifies capsule type 0x02 per RFC 9484 Section 4.7.2.
	CapsuleAddressRequest uint64 = 0x02

	// CapsuleRouteAdvertisement specifies capsule type 0x03 per RFC 9484 Section 4.7.3.
	CapsuleRouteAdvertisement uint64 = 0x03
)

// AssignedAddressPOD is a 100% Plain Old Data representation of assigned IP addresses for zero-alloc off-heap processing.
type AssignedAddressPOD struct {
	RequestID    uint64
	IPVersion    byte
	PrefixLength byte
	RawIP        [16]byte
}

// DecodeAddressAssignPayloadPOD parses AssignedAddressPOD entries using offheap.AllocStruct when arena is provided.
func DecodeAddressAssignPayloadPOD(arena *offheap.Arena, payload []byte) ([]*AssignedAddressPOD, error) {
	var entries []*AssignedAddressPOD
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

		var rawIP [16]byte
		switch ipVer {
		case 4:
			if offset+4+1 > len(payload) {
				return nil, ErrInvalidCapsule
			}
			copy(rawIP[:4], payload[offset:offset+4])
			offset += 4

		case 6:
			if offset+16+1 > len(payload) {
				return nil, ErrInvalidCapsule
			}
			copy(rawIP[:16], payload[offset:offset+16])
			offset += 16

		default:
			return nil, fmt.Errorf("%w: invalid IP version %d", ErrInvalidCapsule, ipVer)
		}

		prefixLen := payload[offset]
		offset++

		var pod *AssignedAddressPOD
		if arena != nil {
			pod = offheap.AllocStruct[AssignedAddressPOD](arena)
		} else {
			pod = &AssignedAddressPOD{}
		}

		pod.RequestID = reqID
		pod.IPVersion = ipVer
		pod.PrefixLength = prefixLen
		pod.RawIP = rawIP

		entries = append(entries, pod)
	}

	return entries, nil
}

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
	return internalMasque.EncodeVarintSlice(v, b)
}

// DecodeVarint decodes a QUIC variable-length integer from b, returning value and byte length.
func DecodeVarint(b []byte) (uint64, int, error) {
	v, n, err := internalMasque.DecodeVarint(b)
	if err != nil {
		return 0, 0, ErrInvalidCapsule
	}

	return v, n, nil
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
