// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package masque implements the MASQUE protocol for the aoni project.
package masque

import (
	"encoding/binary"
	"errors"
	"net/netip"
)

var (
	ErrInvalidIPHeader = errors.New("aoni masque: invalid ip packet header")
	ErrMTUTooSmall     = errors.New("aoni masque: mtu below protocol minimum")
)

// BuildICMPPacketTooBig4 constructs an ICMPv4 Fragmentation Needed packet.
func BuildICMPPacketTooBig4(packet []byte, nextHopMTU uint16) ([]byte, error) {
	if len(packet) < 20 || (packet[0]>>4) != 4 {
		return nil, ErrInvalidIPHeader
	}

	if nextHopMTU < 68 {
		return nil, ErrMTUTooSmall
	}

	ipHdrLen := int(packet[0]&0x0f) * 4
	if len(packet) < ipHdrLen {
		return nil, ErrInvalidIPHeader
	}

	originalLen := min(max(len(packet), 128), 500)
	paddedLen := (originalLen + 3) &^ 3

	totalLen := 20 + 8 + paddedLen
	out := make([]byte, totalLen)

	out[0] = 0x45
	out[1] = 0x00
	binary.BigEndian.PutUint16(out[2:4], uint16(totalLen))
	out[8] = 64
	out[9] = 1

	copy(out[12:16], packet[16:20])
	copy(out[16:20], packet[12:16])

	out[20] = 3
	out[21] = 4
	out[22] = 0
	out[23] = 0
	out[24] = 0
	out[25] = byte(paddedLen / 4)
	binary.BigEndian.PutUint16(out[26:28], nextHopMTU)

	copy(out[28:], packet[:min(len(packet), originalLen)])

	binary.BigEndian.PutUint16(out[10:12], CalculateInternetChecksum(out[:20]))
	binary.BigEndian.PutUint16(out[22:24], CalculateInternetChecksum(out[20:]))

	return out, nil
}

// BuildICMPPacketTooBig6 constructs an ICMPv6 Packet Too Big packet.
func BuildICMPPacketTooBig6(packet []byte, nextHopMTU uint32) ([]byte, error) {
	if len(packet) < 40 || (packet[0]>>4) != 6 {
		return nil, ErrInvalidIPHeader
	}

	if nextHopMTU < 1280 {
		return nil, ErrMTUTooSmall
	}

	originalLen := min(len(packet), 1200)
	totalLen := 40 + 8 + originalLen
	out := make([]byte, totalLen)

	out[0] = 0x60
	binary.BigEndian.PutUint16(out[4:6], uint16(8+originalLen))
	out[6] = 58
	out[7] = 64

	copy(out[8:24], packet[24:40])
	copy(out[24:40], packet[8:24])

	out[40] = 2
	out[41] = 0
	out[42] = 0
	out[43] = 0
	binary.BigEndian.PutUint32(out[44:48], nextHopMTU)

	copy(out[48:], packet[:originalLen])

	srcAddr, _ := netip.AddrFromSlice(out[8:24])
	dstAddr, _ := netip.AddrFromSlice(out[24:40])

	csum := CalculateICMPv6Checksum(srcAddr, dstAddr, out[40:])
	binary.BigEndian.PutUint16(out[42:44], csum)

	return out, nil
}

// CalculateInternetChecksum calculates standard 16-bit 1's complement Internet Checksum.
func CalculateInternetChecksum(b []byte) uint16 {
	var sum uint32

	for i := 0; i < len(b)-1; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(b[i : i+2]))
	}

	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}

	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}

	return ^uint16(sum)
}

// CalculateICMPv6Checksum calculates ICMPv6 checksum with IPv6 pseudo-header.
func CalculateICMPv6Checksum(srcIP, dstIP netip.Addr, icmpMessage []byte) uint16 {
	var ph [40]byte

	srcBytes := srcIP.As16()
	dstBytes := dstIP.As16()

	copy(ph[0:16], srcBytes[:])
	copy(ph[16:32], dstBytes[:])
	binary.BigEndian.PutUint32(ph[32:36], uint32(len(icmpMessage)))
	ph[39] = 58

	var sum uint32

	for i := 0; i < 40; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(ph[i : i+2]))
	}

	for i := 0; i < len(icmpMessage)-1; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(icmpMessage[i : i+2]))
	}

	if len(icmpMessage)%2 == 1 {
		sum += uint32(icmpMessage[len(icmpMessage)-1]) << 8
	}

	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}

	return ^uint16(sum)
}

// EncodeVarintSlice encodes v into b using QUIC variable-length integer encoding (RFC 9000).
func EncodeVarintSlice(v uint64, b []byte) int {
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

// EncodeVarint writes a QUIC-style variable length integer to buf.
func EncodeVarint(val uint64) []byte {
	switch {
	case val <= 63:
		return []byte{byte(val)}
	case val <= 16383:
		b := make([]byte, 2)
		binary.BigEndian.PutUint16(b, uint16(val)|0x4000)
		return b
	case val <= 1073741823:
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, uint32(val)|0x80000000)
		return b
	default:
		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, val|0xc000000000000000)
		return b
	}
}

// DecodeVarint decodes a QUIC-style variable length integer from payload.
func DecodeVarint(payload []byte) (val uint64, readLen int, err error) {
	if len(payload) == 0 {
		return 0, 0, errors.New("aoni masque: truncated varint payload")
	}

	first := payload[0]
	tag := first >> 6

	switch tag {
	case 0:
		return uint64(first & 0x3f), 1, nil

	case 1:
		if len(payload) < 2 {
			return 0, 0, errors.New("aoni masque: truncated 2-byte varint")
		}

		v := binary.BigEndian.Uint16(payload[:2]) & 0x3fff

		return uint64(v), 2, nil

	case 2:
		if len(payload) < 4 {
			return 0, 0, errors.New("aoni masque: truncated 4-byte varint")
		}

		v := binary.BigEndian.Uint32(payload[:4]) & 0x3fffffff

		return uint64(v), 4, nil

	case 3:
		if len(payload) < 8 {
			return 0, 0, errors.New("aoni masque: truncated 8-byte varint")
		}

		v := binary.BigEndian.Uint64(payload[:8]) & 0x3fffffffffffffff

		return v, 8, nil
	}

	return 0, 0, errors.New("aoni masque: invalid varint tag")
}
