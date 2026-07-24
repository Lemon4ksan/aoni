// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h2engine

import (
	"bufio"
	"io"
	"sync"
)

const (
	DefaultFrameSize = 9
	defaultMaxLen    = 1 << 14
)

const (
	FlagAck        FrameFlags = 0x1
	FlagEndStream  FrameFlags = 0x1
	FlagEndHeaders FrameFlags = 0x4
	FlagPadded     FrameFlags = 0x8
	FlagPriority   FrameFlags = 0x20
)

var frameHeaderPool = sync.Pool{
	New: func() any { return &FrameHeader{} },
}

// FrameHeader encapsulates the fixed 9-byte wire header and payload of an HTTP/2 frame.
type FrameHeader struct {
	length    int
	kind      FrameType
	flags     FrameFlags
	stream    uint32
	maxLen    uint32
	rawHeader [DefaultFrameSize]byte
	payload   []byte
	fr        Frame
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

	ReleaseFrame(fr.Body())
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
	f.length = int(bytesToUint24(header[:3]))
	f.kind = FrameType(header[3])
	f.flags = FrameFlags(header[4])
	f.stream = bytesToUint32(header[5:]) & (1<<31 - 1)
}

func (f *FrameHeader) parseHeader(header []byte) {
	uint24ToBytes(header[:3], uint32(f.length))
	header[3] = byte(f.kind)
	header[4] = byte(f.flags)
	uint32ToBytes(header[5:], f.stream)
}

// ReadFrameFrom decodes the next HTTP/2 frame from reader using default bounds.
func ReadFrameFrom(br *bufio.Reader) (*FrameHeader, error) {
	return ReadFrameFromWithSize(br, defaultMaxLen)
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
	header, err := br.Peek(DefaultFrameSize)
	if err != nil {
		return -1, err
	}

	_, _ = br.Discard(DefaultFrameSize)
	rn := int64(DefaultFrameSize)

	f.parseValues(header)
	if err = f.checkLen(); err != nil {
		return 0, err
	}

	if f.kind > FrameContinuation {
		_, _ = br.Discard(f.length)
		return 0, ErrUnknownFrameType
	}

	f.fr = AcquireFrame(f.kind)

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
		panic("aoni h2engine: frame body cannot be nil")
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
