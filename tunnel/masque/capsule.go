// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import (
	"fmt"
	"net/netip"

	"github.com/lemon4ksan/foundation/silicon/offheap"

	impl "github.com/lemon4ksan/aoni/internal/masque"
)

const (
	// CapsuleDatagram specifies capsule type 0x00 (DATAGRAM) per RFC 9297 Section 3.5 & Section 5.4.
	CapsuleDatagram uint64 = 0x00

	// CapsuleAddressAssign specifies capsule type 0x01 (ADDRESS_ASSIGN) per RFC 9484 Section 4.7.1 & Section 12.4.
	CapsuleAddressAssign uint64 = 0x01

	// CapsuleAddressRequest specifies capsule type 0x02 (ADDRESS_REQUEST) per RFC 9484 Section 4.7.2 & Section 12.4.
	CapsuleAddressRequest uint64 = 0x02

	// CapsuleRouteAdvertisement specifies capsule type 0x03 (ROUTE_ADVERTISEMENT) per RFC 9484 Section 4.7.3 & Section 12.4.
	CapsuleRouteAdvertisement uint64 = 0x03
)

// AssignedAddressPOD is a 100% Plain Old Data representation of assigned IP addresses (RFC 9484 §4.7.1 Figure 8)
// for zero-alloc off-heap processing.
type AssignedAddressPOD struct {
	RequestID    uint64
	IPVersion    byte
	PrefixLength byte
	RawIP        [16]byte
}

// NewAssignedAddressSlab creates an off-heap [offheap.SlabAllocator] pre-configured for
// [AssignedAddressPOD] entries. Use the returned slab with [DecodeAddressAssignPayloadPODSlab]
// to allocate entries with individual Free semantics.
func NewAssignedAddressSlab(capacity int) (*offheap.SlabAllocator[AssignedAddressPOD], error) {
	return offheap.NewSlabAllocator[AssignedAddressPOD](capacity)
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

// DecodeAddressAssignPayloadPODSlab parses AssignedAddressPOD entries using a [offheap.SlabAllocator].
// Unlike [DecodeAddressAssignPayloadPOD] with an arena, individual entries can be returned to the
// slab via slab.Free(entry) as soon as they are processed, instead of resetting the entire arena.
//
// When slab is nil or exhausted, entries are heap-allocated as a safe fallback.
func DecodeAddressAssignPayloadPODSlab(
	slab *offheap.SlabAllocator[AssignedAddressPOD],
	payload []byte,
) ([]*AssignedAddressPOD, error) {
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

		// Use slab allocation for individual Free semantics; fall back to heap when exhausted.
		var pod *AssignedAddressPOD

		if slab != nil {
			pod = slab.Alloc()
		}

		if pod == nil {
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

// AssignedAddress represents an allocated IP address or prefix entry in ADDRESS_ASSIGN capsules (RFC 9484 §4.7.1 Figure 8).
type AssignedAddress struct {
	Addr         netip.Addr
	RequestID    uint64
	IPVersion    byte
	PrefixLength byte
}

// RequestedAddress represents an address preference entry in ADDRESS_REQUEST capsules (RFC 9484 §4.7.2 Figure 10).
type RequestedAddress struct {
	Addr         netip.Addr
	RequestID    uint64
	IPVersion    byte
	PrefixLength byte
}

// IPAddressRange represents an advertised route range entry in ROUTE_ADVERTISEMENT capsules (RFC 9484 §4.7.3 Figure 12).
type IPAddressRange struct {
	StartIP    netip.Addr
	EndIP      netip.Addr
	IPVersion  byte
	IPProtocol byte
}

// EncodeVarint encodes v into b using QUIC variable-length integer encoding (RFC 9000 §16 / RFC 9297 §1.1).
func EncodeVarint(v uint64, b []byte) int {
	return impl.EncodeVarintSlice(v, b)
}

// DecodeVarint decodes a QUIC variable-length integer from b, returning value and byte length (RFC 9000 §16).
func DecodeVarint(b []byte) (uint64, int, error) {
	v, n, err := impl.DecodeVarint(b)
	if err != nil {
		return 0, 0, ErrInvalidCapsule
	}

	return v, n, nil
}

// EncodeCapsuleHeader writes capsule type and payload length varints into b per RFC 9297 Section 3.2 (Figure 3).
func EncodeCapsuleHeader(capsuleType, payloadLen uint64, b []byte) int {
	n1 := EncodeVarint(capsuleType, b)
	n2 := EncodeVarint(payloadLen, b[n1:])

	return n1 + n2
}

// EncodeCapsule writes complete capsule frame (type, length, payload) into dst per RFC 9297 Section 3.2.
func EncodeCapsule(capsuleType uint64, payload, dst []byte) int {
	hdrLen := EncodeCapsuleHeader(capsuleType, uint64(len(payload)), dst)
	copy(dst[hdrLen:], payload)

	return hdrLen + len(payload)
}

// EncodeAddressAssignHeader writes type (0x01) and payload length varints for ADDRESS_ASSIGN capsule (RFC 9484 §4.7.1).
func EncodeAddressAssignHeader(payloadLen uint64, b []byte) int {
	return EncodeCapsuleHeader(CapsuleAddressAssign, payloadLen, b)
}

// EncodeAddressAssignPayload writes AssignedAddress entries payload bytes into b per RFC 9484 Section 4.7.1.
func EncodeAddressAssignPayload(entries []AssignedAddress, b []byte) int {
	offset := 0

	for _, entry := range entries {
		offset += EncodeVarint(entry.RequestID, b[offset:])

		ipVer := entry.IPVersion
		if ipVer == 0 {
			if entry.Addr.Is4() {
				ipVer = 4
			} else {
				ipVer = 6
			}
		}

		b[offset] = ipVer
		offset++

		if ipVer == 4 {
			ip4 := entry.Addr.As4()
			copy(b[offset:offset+4], ip4[:])
			offset += 4
		} else {
			ip6 := entry.Addr.As16()
			copy(b[offset:offset+16], ip6[:])
			offset += 16
		}

		b[offset] = entry.PrefixLength
		offset++
	}

	return offset
}

// EncodeAddressAssignCapsule writes full ADDRESS_ASSIGN capsule into b per RFC 9484 Section 4.7.1 & RFC 9297 Section 3.2.
func EncodeAddressAssignCapsule(entries []AssignedAddress, b []byte) int {
	var payloadBuf [2048]byte

	payloadLen := EncodeAddressAssignPayload(entries, payloadBuf[:])

	return EncodeCapsule(CapsuleAddressAssign, payloadBuf[:payloadLen], b)
}

// DecodeAddressAssignPayload parses AssignedAddress entries from payload bytes into a new slice per RFC 9484 Section 4.7.1.
func DecodeAddressAssignPayload(payload []byte) ([]AssignedAddress, error) {
	entries := make([]AssignedAddress, 0, len(payload)/8)
	return DecodeAddressAssignPayloadTo(payload, entries)
}

// DecodeAddressAssignPayloadTo parses AssignedAddress entries into pre-allocated dst slice per RFC 9484 Section 4.7.1 with 0 B/op.
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

// EncodeAddressRequestPayload writes RequestedAddress entries payload bytes into b per RFC 9484 Section 4.7.2.
func EncodeAddressRequestPayload(entries []RequestedAddress, b []byte) (int, error) {
	if len(entries) == 0 {
		return 0, ErrEmptyAddressRequest
	}

	offset := 0

	for _, entry := range entries {
		offset += EncodeVarint(entry.RequestID, b[offset:])

		ipVer := entry.IPVersion
		if ipVer == 0 {
			if entry.Addr.Is4() {
				ipVer = 4
			} else {
				ipVer = 6
			}
		}

		b[offset] = ipVer
		offset++

		switch ipVer {
		case 4:
			ip4 := entry.Addr.As4()
			copy(b[offset:offset+4], ip4[:])
			offset += 4
		case 6:
			ip6 := entry.Addr.As16()
			copy(b[offset:offset+16], ip6[:])
			offset += 16
		default:
			return 0, fmt.Errorf("%w: invalid IP version %d", ErrInvalidCapsule, ipVer)
		}

		b[offset] = entry.PrefixLength
		offset++
	}

	return offset, nil
}

// EncodeAddressRequestCapsule writes full ADDRESS_REQUEST capsule into b per RFC 9484 Section 4.7.2 & RFC 9297 Section 3.2.
func EncodeAddressRequestCapsule(entries []RequestedAddress, b []byte) (int, error) {
	var payloadBuf [2048]byte

	payloadLen, err := EncodeAddressRequestPayload(entries, payloadBuf[:])
	if err != nil {
		return 0, err
	}

	return EncodeCapsule(CapsuleAddressRequest, payloadBuf[:payloadLen], b), nil
}

// DecodeAddressRequestPayload parses RequestedAddress entries from payload bytes into a new slice per RFC 9484 Section 4.7.2.
func DecodeAddressRequestPayload(payload []byte) ([]RequestedAddress, error) {
	entries := make([]RequestedAddress, 0, len(payload)/8)
	return DecodeAddressRequestPayloadTo(payload, entries)
}

// DecodeAddressRequestPayloadTo parses RequestedAddress entries into pre-allocated dst slice per RFC 9484 Section 4.7.2 with 0 B/op.
func DecodeAddressRequestPayloadTo(payload []byte, dst []RequestedAddress) ([]RequestedAddress, error) {
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

		dst = append(dst, RequestedAddress{
			Addr:         addr,
			RequestID:    reqID,
			IPVersion:    ipVer,
			PrefixLength: prefixLen,
		})
	}

	return dst, nil
}

// EncodeRouteAdvertisementPayload writes IPAddressRange entries payload bytes into b per RFC 9484 Section 4.7.3.
func EncodeRouteAdvertisementPayload(ranges []IPAddressRange, b []byte) (int, error) {
	offset := 0

	for _, r := range ranges {
		ipVer := r.IPVersion
		if ipVer == 0 {
			if r.StartIP.Is4() {
				ipVer = 4
			} else {
				ipVer = 6
			}
		}

		b[offset] = ipVer
		offset++

		switch ipVer {
		case 4:
			start4 := r.StartIP.As4()
			copy(b[offset:offset+4], start4[:])
			offset += 4

			end4 := r.EndIP.As4()
			copy(b[offset:offset+4], end4[:])
			offset += 4

		case 6:
			start6 := r.StartIP.As16()
			copy(b[offset:offset+16], start6[:])
			offset += 16

			end6 := r.EndIP.As16()
			copy(b[offset:offset+16], end6[:])
			offset += 16

		default:
			return 0, fmt.Errorf("%w: invalid IP version %d", ErrInvalidCapsule, ipVer)
		}

		b[offset] = r.IPProtocol
		offset++
	}

	return offset, nil
}

// EncodeRouteAdvertisementCapsule writes full ROUTE_ADVERTISEMENT capsule into b per RFC 9484 Section 4.7.3 & RFC 9297 Section 3.2.
func EncodeRouteAdvertisementCapsule(ranges []IPAddressRange, b []byte) (int, error) {
	var payloadBuf [2048]byte

	payloadLen, err := EncodeRouteAdvertisementPayload(ranges, payloadBuf[:])
	if err != nil {
		return 0, err
	}

	return EncodeCapsule(CapsuleRouteAdvertisement, payloadBuf[:payloadLen], b), nil
}

// DecodeRouteAdvertisementPayload parses IPAddressRange entries from payload bytes per RFC 9484 Section 4.7.3.
func DecodeRouteAdvertisementPayload(payload []byte) ([]IPAddressRange, error) {
	entries := make([]IPAddressRange, 0, len(payload)/10)
	return DecodeRouteAdvertisementPayloadTo(payload, entries)
}

// DecodeRouteAdvertisementPayloadTo parses IPAddressRange entries into pre-allocated dst slice per RFC 9484 Section 4.7.3 with 0 B/op.
func DecodeRouteAdvertisementPayloadTo(payload []byte, dst []IPAddressRange) ([]IPAddressRange, error) {
	offset := 0

	for offset < len(payload) {
		if offset >= len(payload) {
			break
		}

		ipVer := payload[offset]
		offset++

		var startAddr, endAddr netip.Addr

		switch ipVer {
		case 4:
			if offset+4+4+1 > len(payload) {
				return nil, ErrInvalidCapsule
			}

			var start4, end4 [4]byte
			copy(start4[:], payload[offset:offset+4])
			offset += 4
			copy(end4[:], payload[offset:offset+4])
			offset += 4

			startAddr = netip.AddrFrom4(start4)
			endAddr = netip.AddrFrom4(end4)

		case 6:
			if offset+16+16+1 > len(payload) {
				return nil, ErrInvalidCapsule
			}

			var start6, end6 [16]byte
			copy(start6[:], payload[offset:offset+16])
			offset += 16
			copy(end6[:], payload[offset:offset+16])
			offset += 16

			startAddr = netip.AddrFrom16(start6)
			endAddr = netip.AddrFrom16(end6)

		default:
			return nil, fmt.Errorf("%w: invalid IP version %d", ErrInvalidCapsule, ipVer)
		}

		proto := payload[offset]
		offset++

		dst = append(dst, IPAddressRange{
			StartIP:    startAddr,
			EndIP:      endAddr,
			IPVersion:  ipVer,
			IPProtocol: proto,
		})
	}

	return dst, nil
}
