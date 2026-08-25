// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build (amd64 || arm64) && !purego

package protocol

const hasVectorPktNum = true

func vectorDecodePacketNumber(length PacketNumberLen, largest, truncated PacketNumber) PacketNumber {
	return PacketNumber(quic_decode_packet_number(
		uint64(length),
		uint64(largest),
		uint64(truncated),
		0,
		0,
		0,
	))
}

func vectorPacketNumberLengthForHeader(pn, largestAcked PacketNumber) PacketNumberLen {
	return PacketNumberLen(quic_packet_number_len_for_header(
		uint64(pn),
		uint64(largestAcked),
		0,
		0,
		0,
		0,
	))
}
