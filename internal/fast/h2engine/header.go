// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h2engine

import (
	"bufio"
	"io"
	"sync"

	"github.com/lemon4ksan/foundation/silicon/offheap"
)

const (
	// defaultFrameSize specifies the mandatory 9-octet HTTP/2 frame header length (RFC 9113 §4.1).
	defaultFrameSize = 9

	// defaultMaxLen specifies the default initial maximum frame payload length of 16,384 octets (2^14, RFC 9113 §4.2).
	defaultMaxLen = 1 << 14
)

const (
	// FlagAck indicates that the frame acknowledges receipt of a SETTINGS or PING frame (RFC 9113 §6.5 & §6.7).
	FlagAck FrameFlags = 0x1

	// FlagEndStream indicates that this frame is the last that the endpoint will send for the stream (RFC 9113 §6.1 & §6.2).
	FlagEndStream FrameFlags = 0x1

	// FlagEndHeaders indicates that this frame contains an entire field block (RFC 9113 §6.2, §6.6 & §6.10).
	FlagEndHeaders FrameFlags = 0x4

	// FlagPadded indicates that the Pad Length field and frame padding are present (RFC 9113 §6.1, §6.2 & §6.6).
	FlagPadded FrameFlags = 0x8

	// FlagPriority indicates that the priority fields are present in a HEADERS frame (RFC 9113 §6.2, deprecated per §5.3.2).
	FlagPriority FrameFlags = 0x20
)

var frameHeaderPool = sync.Pool{
	New: func() any { return &FrameHeader{} },
}

// FrameHeader encapsulates the fixed 9-octet wire header and payload of an HTTP/2 frame (RFC 9113 §4.1 & §4.2).
type FrameHeader struct {
	length    int
	kind      FrameType
	flags     FrameFlags
	stream    uint32
	maxLen    uint32
	rawHeader [defaultFrameSize]byte
	payload   []byte
	fr        Frame
	arena     *offheap.Arena
}

// AcquireFrameHeader fetches a clean FrameHeader from memory pools.
func AcquireFrameHeader() *FrameHeader {
	fr := frameHeaderPool.Get().(*FrameHeader)
	fr.Reset()

	return fr
}

// ReleaseFrameHeader returns a FrameHeader and its attached body to memory pools.
func ReleaseFrameHeader(fr *FrameHeader) {
	if fr == nil {
		return
	}

	if fr.Body() != nil {
		ReleaseFrame(fr.Body())
	}

	frameHeaderPool.Put(fr)
}

// Reset clears frame header fields to default state.
func (f *FrameHeader) Reset() {
	f.kind = 0
	f.flags = 0
	f.stream = 0
	f.length = 0
	f.maxLen = defaultMaxLen
	f.fr = nil
	f.payload = f.payload[:0]
}

func (f *FrameHeader) Type() FrameType           { return f.kind }
func (f *FrameHeader) Flags() FrameFlags         { return f.flags }
func (f *FrameHeader) SetFlags(flags FrameFlags) { f.flags = flags }
func (f *FrameHeader) Stream() uint32            { return f.stream }
func (f *FrameHeader) SetStream(stream uint32)   { f.stream = stream }
func (f *FrameHeader) Len() int                  { return f.length }
func (f *FrameHeader) MaxLen() uint32            { return f.maxLen }

func (f *FrameHeader) parseValues(header []byte) {
	if hasVectorFrame && len(header) >= defaultFrameSize {
		f.length, f.kind, f.flags, f.stream = vectorUnpackFrameHeader(header)
		return
	}

	f.length = int(bytesToUint24(header[:3]))
	f.kind = FrameType(header[3])   //nolint:gosec
	f.flags = FrameFlags(header[4]) //nolint:gosec
	f.stream = bytesToUint32(header[5:]) & (1<<31 - 1)
}

// PackFrameHeader serializes frame header into dst (at least 9 bytes).
func PackFrameHeader(dst []byte, length int, kind FrameType, flags FrameFlags, stream uint32) {
	if hasVectorFrame && len(dst) >= defaultFrameSize {
		vectorPackFrameHeader(dst, length, kind, flags, stream)
		return
	}

	uint24ToBytes(dst[:3], uint32(length))
	dst[3] = byte(kind)
	dst[4] = byte(flags)
	uint32ToBytes(dst[5:], stream)
}

// UnpackFrameHeader deserializes 9-byte frame header from src.
func UnpackFrameHeader(src []byte) (length int, kind FrameType, flags FrameFlags, stream uint32) {
	if hasVectorFrame && len(src) >= defaultFrameSize {
		return vectorUnpackFrameHeader(src)
	}

	length = int(bytesToUint24(src[:3]))
	kind = FrameType(src[3])
	flags = FrameFlags(src[4])
	stream = bytesToUint32(src[5:]) & (1<<31 - 1)
	return length, kind, flags, stream
}

func (f *FrameHeader) parseHeader(header []byte) {
	if hasVectorFrame && len(header) >= defaultFrameSize {
		vectorPackFrameHeader(header, f.length, f.kind, f.flags, f.stream)
		return
	}

	uint24ToBytes(header[:3], uint32(f.length)) //nolint:gosec
	header[3] = byte(f.kind)                    //nolint:gosec
	header[4] = byte(f.flags)                   //nolint:gosec
	uint32ToBytes(header[5:], f.stream)
}

// ReadFrameFrom decodes the next HTTP/2 frame from reader using default bounds.
func ReadFrameFrom(br *bufio.Reader) (*FrameHeader, error) {
	return ReadFrameFromWithSize(br, maxFrameSize)
}

// ReadFrameFromWithSize decodes the next HTTP/2 frame enforcing max payload bounds.
func ReadFrameFromWithSize(br *bufio.Reader, max uint32) (*FrameHeader, error) {
	fr := AcquireFrameHeader()
	fr.maxLen = max

	if _, err := fr.readFrom(br); err != nil {
		if fr.Body() != nil {
			ReleaseFrameHeader(fr)
		} else {
			frameHeaderPool.Put(fr)
		}

		return nil, err
	}

	return fr, nil
}

func (f *FrameHeader) readFrom(br *bufio.Reader) (int64, error) {
	header, err := br.Peek(defaultFrameSize)
	if err != nil {
		return -1, err
	}

	_, _ = br.Discard(defaultFrameSize)
	rn := int64(defaultFrameSize)

	f.parseValues(header)

	if err = f.checkLen(); err != nil {
		if f.length > 0 {
			if _, err := io.CopyN(io.Discard, br, int64(f.length)); err != nil {
				return 0, err
			}

			rn += int64(f.length)
		}

		f.fr = nil

		return rn, nil
	}

	if f.kind > FrameContinuation {
		if f.length > 0 {
			if _, err := io.CopyN(io.Discard, br, int64(f.length)); err != nil {
				return 0, err
			}

			rn += int64(f.length)
		}

		f.fr = nil

		return rn, nil
	}

	if f.kind == FrameData {
		d := framePools[FrameData].Get().(*Data)
		d.Reset()
		f.fr = d

		if f.length > 0 {
			f.payload = resizeSlice(f.payload, f.length)

			n, err := io.ReadFull(br, f.payload[:f.length])
			if err != nil {
				ReleaseFrame(f.fr)
				return 0, err
			}

			rn += int64(n)
		}

		return rn, d.Deserialize(f)
	}

	if f.kind == FrameHeaders {
		h := framePools[FrameHeaders].Get().(*Headers)
		h.Reset()
		f.fr = h

		if f.length > 0 {
			f.payload = resizeSlice(f.payload, f.length)

			n, err := io.ReadFull(br, f.payload[:f.length])
			if err != nil {
				ReleaseFrame(f.fr)
				return 0, err
			}

			rn += int64(n)
		}

		return rn, h.Deserialize(f)
	}

	f.fr = AcquireFrameInArena(f.arena, f.kind)

	if f.length > 0 {
		f.payload = resizeSlice(f.payload, f.length)

		n, err := io.ReadFull(br, f.payload[:f.length])
		if err != nil {
			ReleaseFrame(f.fr)
			return 0, err
		}

		rn += int64(n)
	}

	return rn, f.fr.Deserialize(f)
}

// WriteTo serializes and transmits the frame and header payload to writer.
func (f *FrameHeader) WriteTo(w *bufio.Writer) (wb int64, err error) {
	f.fr.Serialize(f)

	f.length = len(f.payload)
	f.parseHeader(f.rawHeader[:])

	n, err := w.Write(f.rawHeader[:])
	if err != nil {
		return int64(n), err
	}

	wb += int64(n)

	n, err = w.Write(f.payload)
	wb += int64(n)

	return wb, err
}

func (f *FrameHeader) Body() Frame { return f.fr }

func (f *FrameHeader) SetBody(fr Frame) {
	if fr == nil {
		panic("h2engine: frame body cannot be nil")
	}

	f.kind = fr.Type()
	f.fr = fr
}

func (f *FrameHeader) setPayload(payload []byte) {
	f.payload = append(f.payload[:0], payload...)
}

func (f *FrameHeader) checkLen() error {
	if f.maxLen != 0 && f.length > int(f.maxLen) {
		return ErrPayloadExceeds
	}

	return nil
}
