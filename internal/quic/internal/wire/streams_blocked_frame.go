// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wire

import (
	"fmt"

	"github.com/lemon4ksan/aoni/internal/quic/internal/protocol"
	"github.com/lemon4ksan/aoni/internal/quic/quicvarint"
)

// A StreamsBlockedFrame is a STREAMS_BLOCKED frame
type StreamsBlockedFrame struct {
	Type        protocol.StreamType
	StreamLimit protocol.StreamNum
}

func parseStreamsBlockedFrame(b []byte, typ FrameType, _ protocol.Version) (*StreamsBlockedFrame, int, error) {
	f := &StreamsBlockedFrame{}
	//nolint:exhaustive // This will only be called with a BidiStreamBlockedFrameType or a UniStreamBlockedFrameType.
	switch typ {
	case FrameTypeBidiStreamBlocked:
		f.Type = protocol.StreamTypeBidi
	case FrameTypeUniStreamBlocked:
		f.Type = protocol.StreamTypeUni
	}

	streamLimit, l, err := quicvarint.Parse(b)
	if err != nil {
		return nil, 0, replaceUnexpectedEOF(err)
	}

	f.StreamLimit = protocol.StreamNum(streamLimit)
	if f.StreamLimit > protocol.MaxStreamCount {
		return nil, 0, fmt.Errorf("%d exceeds the maximum stream count", f.StreamLimit)
	}

	return f, l, nil
}

func (f *StreamsBlockedFrame) Append(b []byte, _ protocol.Version) ([]byte, error) {
	switch f.Type {
	case protocol.StreamTypeBidi:
		b = append(b, byte(FrameTypeBidiStreamBlocked))
	case protocol.StreamTypeUni:
		b = append(b, byte(FrameTypeUniStreamBlocked))
	}

	b = quicvarint.Append(b, uint64(f.StreamLimit))

	return b, nil
}

// Length of a written frame
func (f *StreamsBlockedFrame) Length(_ protocol.Version) protocol.ByteCount {
	return 1 + protocol.ByteCount(quicvarint.Len(uint64(f.StreamLimit)))
}
