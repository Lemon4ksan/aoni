// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wire_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/internal/quic/internal/protocol"
	"github.com/lemon4ksan/aoni/internal/quic/internal/wire"
)

func FuzzQUICFrameParser(f *testing.F) {
	// Ping frame
	f.Add([]byte{0x01}, uint8(0))
	// Max Data frame
	f.Add([]byte{0x10, 0x04, 0xff, 0xff}, uint8(2))
	// Stream frame
	f.Add([]byte{0x08, 0x01, 'h', 'e', 'l', 'l', 'o'}, uint8(2))
	// ACK frame
	f.Add([]byte{0x02, 0x01, 0x00, 0x00, 0x00}, uint8(2))
	f.Add([]byte{}, uint8(0))
	f.Add([]byte{0xff, 0xff, 0xff, 0xff}, uint8(1))

	f.Fuzz(func(t *testing.T, data []byte, encLevelByte uint8) {
		if len(data) > 64*1024 {
			return
		}

		var encLevel protocol.EncryptionLevel
		switch encLevelByte % 3 {
		case 0:
			encLevel = protocol.EncryptionInitial
		case 1:
			encLevel = protocol.EncryptionHandshake
		default:
			encLevel = protocol.Encryption1RTT
		}

		parser := wire.NewFrameParser(true, true, true)

		frameType, consumed, err := parser.ParseType(data, encLevel)
		if err == nil && frameType != 0 && consumed <= len(data) {
			remaining := data[consumed:]

			switch {
			case frameType.IsStreamFrameType():
				_, _, _ = parser.ParseStreamFrame(frameType, remaining, protocol.Version1)
			case frameType.IsAckFrameType():
				_, _, _ = parser.ParseAckFrame(frameType, remaining, encLevel, protocol.Version1)
			case frameType.IsDatagramFrameType():
				_, _, _ = parser.ParseDatagramFrame(frameType, remaining, protocol.Version1)
			default:
				_, _, _ = parser.ParseLessCommonFrame(frameType, remaining, protocol.Version1)
			}
		}

		_, _ = wire.ParseConnectionID(data, 8)
		_, _ = wire.ParseVersion(data)
		_, _, _, _ = wire.ParseArbitraryLenConnectionIDs(data)
	})
}
