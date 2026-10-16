// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import (
	"encoding/binary"
)

// ClampMSSInPlace inspects TCP SYN packets and overwrites the MSS option (RFC 9293 / RFC 879)
// if it exceeds the calculated maximum MSS for the given MTU.
func ClampMSSInPlace(packet []byte, maxMTU int) {
	if len(packet) < 20 || maxMTU <= 40 {
		return
	}

	version := packet[0] >> 4

	maxMSS, ipHdrLen, ok := calculateMaxMSS(packet, version, maxMTU)
	if !ok {
		return
	}

	tcpHdr := packet[ipHdrLen:]
	if len(tcpHdr) < 20 || (tcpHdr[13]&0x02) == 0 {
		return
	}

	tcpDataOffset := int(tcpHdr[12]>>4) * 4
	if len(tcpHdr) < tcpDataOffset || tcpDataOffset < 20 {
		return
	}

	options := tcpHdr[20:tcpDataOffset]
	if updateMSSOption(options, maxMSS) {
		recalculateTCPChecksum(packet, version, ipHdrLen)
	}
}

func calculateMaxMSS(packet []byte, version byte, maxMTU int) (uint16, int, bool) {
	if version == 4 {
		ipHdrLen := int(packet[0]&0x0f) * 4
		if ipHdrLen < 20 || len(packet) < ipHdrLen+20 || packet[9] != 6 {
			return 0, 0, false
		}

		return uint16(maxMTU - 40), ipHdrLen, true
	}

	if version == 6 {
		ipHdrLen := 40
		if len(packet) < ipHdrLen+20 || packet[6] != 6 || maxMTU <= 60 {
			return 0, 0, false
		}

		return uint16(maxMTU - 60), ipHdrLen, true
	}

	return 0, 0, false
}

func updateMSSOption(options []byte, maxMSS uint16) bool {
	optIdx := 0
	for optIdx < len(options) {
		optKind := options[optIdx]
		if optKind == 0 {
			break
		}

		if optKind == 1 {
			optIdx++
			continue
		}

		if optIdx+1 >= len(options) {
			break
		}

		optLen := int(options[optIdx+1]) //nolint:gosec
		if optLen < 2 || optIdx+optLen > len(options) {
			break
		}

		if optKind == 2 && optLen == 4 {
			currentMSS := binary.BigEndian.Uint16(options[optIdx+2 : optIdx+4])
			if currentMSS > maxMSS {
				binary.BigEndian.PutUint16(options[optIdx+2:optIdx+4], maxMSS)
				return true
			}

			return false
		}

		optIdx += optLen
	}

	return false
}

func recalculateTCPChecksum(packet []byte, version byte, ipHdrLen int) {
	tcpHdr := packet[ipHdrLen:]
	tcpLen := len(packet) - ipHdrLen

	tcpHdr[16] = 0
	tcpHdr[17] = 0

	var sum uint32

	if version == 4 {
		sum += uint32(binary.BigEndian.Uint16(packet[12:14]))
		sum += uint32(binary.BigEndian.Uint16(packet[14:16]))
		sum += uint32(binary.BigEndian.Uint16(packet[16:18]))
		sum += uint32(binary.BigEndian.Uint16(packet[18:20]))
		sum += uint32(6)
		sum += uint32(tcpLen)
	} else {
		for i := 8; i < 40; i += 2 {
			sum += uint32(binary.BigEndian.Uint16(packet[i : i+2]))
		}

		sum += uint32(tcpLen)
		sum += uint32(6)
	}

	for i := 0; i < tcpLen-1; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(tcpHdr[i : i+2]))
	}

	if tcpLen%2 == 1 {
		sum += uint32(tcpHdr[tcpLen-1]) << 8
	}

	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}

	binary.BigEndian.PutUint16(tcpHdr[16:18], ^uint16(sum))
}
