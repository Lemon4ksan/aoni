// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !amd64 || purego

package protocol

const hasVectorPktNum = false

func vectorDecodePacketNumber(length PacketNumberLen, largest, truncated PacketNumber) PacketNumber {
	return 0
}

func vectorPacketNumberLengthForHeader(pn, largestAcked PacketNumber) PacketNumberLen {
	return 0
}
