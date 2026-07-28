// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h2engine

import (
	"encoding/binary"
	"fmt"
	"time"
)

// Continuation carries extended header block fragments across HTTP/2 frame boundaries.
type Continuation struct {
	endHeaders bool
	rawHeaders []byte
}

func (c *Continuation) Type() FrameType { return FrameContinuation }

func (c *Continuation) Reset() {
	c.endHeaders = false
	c.rawHeaders = c.rawHeaders[:0]
}

func (c *Continuation) Headers() []byte             { return c.rawHeaders }
func (c *Continuation) SetEndHeaders(v bool)        { c.endHeaders = v }
func (c *Continuation) EndHeaders() bool            { return c.endHeaders }
func (c *Continuation) SetHeader(b []byte)          { c.rawHeaders = append(c.rawHeaders[:0], b...) }
func (c *Continuation) AppendHeader(b []byte)       { c.rawHeaders = append(c.rawHeaders, b...) }
func (c *Continuation) Write(b []byte) (int, error) { c.AppendHeader(b); return len(b), nil }

func (c *Continuation) Deserialize(fr *FrameHeader) error {
	c.endHeaders = fr.Flags().Has(FlagEndHeaders)
	c.SetHeader(fr.payload)

	return nil
}

func (c *Continuation) Serialize(fr *FrameHeader) {
	if c.endHeaders {
		fr.SetFlags(fr.Flags().Add(FlagEndHeaders))
	}

	fr.setPayload(c.rawHeaders)
}

// Data encapsulates streaming payload bytes transmitted over an open HTTP/2 stream.
type Data struct {
	endStream  bool
	hasPadding bool
	b          []byte
}

func (d *Data) Type() FrameType             { return FrameData }
func (d *Data) Reset()                      { d.endStream = false; d.hasPadding = false; d.b = d.b[:0] }
func (d *Data) SetEndStream(v bool)         { d.endStream = v }
func (d *Data) EndStream() bool             { return d.endStream }
func (d *Data) Data() []byte                { return d.b }
func (d *Data) SetData(b []byte)            { d.b = append(d.b[:0], b...) }
func (d *Data) Padding() bool               { return d.hasPadding }
func (d *Data) SetPadding(v bool)           { d.hasPadding = v }
func (d *Data) Append(b []byte)             { d.b = append(d.b, b...) }
func (d *Data) Len() int                    { return len(d.b) }
func (d *Data) Write(b []byte) (int, error) { d.Append(b); return len(b), nil }

func (d *Data) Deserialize(fr *FrameHeader) error {
	payload := fr.payload

	if fr.Flags().Has(FlagPadded) {
		var err error

		payload, err = cutPadding(payload, fr.Len())
		if err != nil {
			return err
		}
	}

	d.endStream = fr.Flags().Has(FlagEndStream)
	d.b = append(d.b[:0], payload...)

	return nil
}

func (d *Data) Serialize(fr *FrameHeader) {
	if d.endStream {
		fr.SetFlags(fr.Flags().Add(FlagEndStream))
	}

	if d.hasPadding {
		fr.SetFlags(fr.Flags().Add(FlagPadded))

		d.b = addPadding(d.b)
	}

	fr.setPayload(d.b)
}

// GoAway signals connection shutdown or fatal protocol violations.
type GoAway struct {
	stream uint32
	code   ErrorCode
	data   []byte
}

func (ga *GoAway) Type() FrameType         { return FrameGoAway }
func (ga *GoAway) Reset()                  { ga.stream = 0; ga.code = 0; ga.data = ga.data[:0] }
func (ga *GoAway) Code() ErrorCode         { return ga.code }
func (ga *GoAway) SetCode(code ErrorCode)  { ga.code = code & (1<<31 - 1) }
func (ga *GoAway) Stream() uint32          { return ga.stream }
func (ga *GoAway) SetStream(stream uint32) { ga.stream = stream & (1<<31 - 1) }
func (ga *GoAway) Data() []byte            { return ga.data }
func (ga *GoAway) SetData(b []byte)        { ga.data = append(ga.data[:0], b...) }
func (ga *GoAway) Error() string {
	return fmt.Sprintf("stream=%d, code=%s, data=%s", ga.stream, ga.code, ga.data)
}

func (ga *GoAway) Deserialize(fr *FrameHeader) error {
	if len(fr.payload) < 8 {
		return ErrMissingBytes
	}

	ga.stream = bytesToUint32(fr.payload) & (1<<31 - 1)
	ga.code = ErrorCode(bytesToUint32(fr.payload[4:]))

	if len(fr.payload) > 8 {
		ga.data = append(ga.data[:0], fr.payload[8:]...)
	}

	return nil
}

func (ga *GoAway) Serialize(fr *FrameHeader) {
	fr.payload = appendUint32Bytes(fr.payload[:0], ga.stream)
	fr.payload = appendUint32Bytes(fr.payload, uint32(ga.code))
	fr.payload = append(fr.payload, ga.data...)
}

// Headers carries HPACK-compressed HTTP metadata.
type Headers struct {
	hasPadding bool
	stream     uint32
	weight     uint8
	endStream  bool
	endHeaders bool
	priority   bool
	rawHeaders []byte
}

func (h *Headers) Type() FrameType           { return FrameHeaders }
func (h *Headers) Headers() []byte           { return h.rawHeaders }
func (h *Headers) SetHeaders(b []byte)       { h.rawHeaders = append(h.rawHeaders[:0], b...) }
func (h *Headers) AppendRawHeaders(b []byte) { h.rawHeaders = append(h.rawHeaders, b...) }
func (h *Headers) EndStream() bool           { return h.endStream }
func (h *Headers) SetEndStream(v bool)       { h.endStream = v }
func (h *Headers) EndHeaders() bool          { return h.endHeaders }
func (h *Headers) SetEndHeaders(v bool)      { h.endHeaders = v }
func (h *Headers) Stream() uint32            { return h.stream }
func (h *Headers) SetStream(stream uint32)   { h.stream = stream }
func (h *Headers) Weight() byte              { return h.weight }
func (h *Headers) SetWeight(w byte)          { h.weight = w }
func (h *Headers) Padding() bool             { return h.hasPadding }
func (h *Headers) SetPadding(v bool)         { h.hasPadding = v }

func (h *Headers) Reset() {
	h.hasPadding = false
	h.stream = 0
	h.weight = 0
	h.endStream = false
	h.endHeaders = false
	h.priority = false
	h.rawHeaders = h.rawHeaders[:0]
}

func (h *Headers) AppendHeaderField(hp *HPACK, hf *HeaderField, store bool) {
	h.rawHeaders = hp.AppendHeader(h.rawHeaders, hf, store)
}

func (h *Headers) Deserialize(frh *FrameHeader) error {
	flags := frh.Flags()
	payload := frh.payload

	if flags.Has(FlagPadded) {
		var err error

		payload, err = cutPadding(payload, len(payload))
		if err != nil {
			return err
		}
	}

	if flags.Has(FlagPriority) {
		if len(payload) < 5 {
			return ErrMissingBytes
		}

		h.priority = true
		h.stream = bytesToUint32(payload) & (1<<31 - 1)
		h.weight = payload[4]
		payload = payload[5:]
	}

	h.endStream = flags.Has(FlagEndStream)
	h.endHeaders = flags.Has(FlagEndHeaders)
	h.rawHeaders = append(h.rawHeaders, payload...)

	return nil
}

func (h *Headers) Serialize(frh *FrameHeader) {
	if h.endStream {
		frh.SetFlags(frh.Flags().Add(FlagEndStream))
	}

	if h.endHeaders {
		frh.SetFlags(frh.Flags().Add(FlagEndHeaders))
	}

	if h.priority {
		frh.SetFlags(frh.Flags().Add(FlagPriority))

		h.rawHeaders = append(h.rawHeaders, 0, 0, 0, 0, 0)
		copy(h.rawHeaders[5:], h.rawHeaders)
		uint32ToBytes(h.rawHeaders[0:4], frh.stream)
		h.rawHeaders[4] = h.weight
	}

	if h.hasPadding {
		frh.SetFlags(frh.Flags().Add(FlagPadded))

		h.rawHeaders = addPadding(h.rawHeaders)
	}

	frh.payload = append(frh.payload[:0], h.rawHeaders...)
}

// Ping verifies connection liveness and measures round-trip time.
type Ping struct {
	ack  bool
	data [8]byte
}

func (p *Ping) Type() FrameType             { return FramePing }
func (p *Ping) IsAck() bool                 { return p.ack }
func (p *Ping) SetAck(ack bool)             { p.ack = ack }
func (p *Ping) Reset()                      { p.ack = false }
func (p *Ping) Data() []byte                { return p.data[:] }
func (p *Ping) SetData(b []byte)            { copy(p.data[:], b) }
func (p *Ping) Write(b []byte) (int, error) { copy(p.data[:], b); return len(b), nil }

func (p *Ping) SetCurrentTime() {
	binary.BigEndian.PutUint64(p.data[:], uint64(time.Now().UnixNano()))
}

func (p *Ping) DataAsTime() time.Time {
	return time.Unix(0, int64(binary.BigEndian.Uint64(p.data[:])))
}

func (p *Ping) Deserialize(frh *FrameHeader) error {
	p.ack = frh.Flags().Has(FlagAck)
	if len(frh.payload) != 8 {
		return ErrInvalidPingPayload
	}

	p.SetData(frh.payload)

	return nil
}

func (p *Ping) Serialize(fr *FrameHeader) {
	if p.ack {
		fr.SetFlags(fr.Flags().Add(FlagAck))
	}

	fr.setPayload(p.data[:])
}

// Priority specifies stream dependencies and weighting parameters.
type Priority struct {
	stream uint32
	weight byte
}

func (pry *Priority) Type() FrameType         { return FramePriority }
func (pry *Priority) Reset()                  { pry.stream = 0; pry.weight = 0 }
func (pry *Priority) Stream() uint32          { return pry.stream }
func (pry *Priority) SetStream(stream uint32) { pry.stream = stream & (1<<31 - 1) }
func (pry *Priority) Weight() byte            { return pry.weight }
func (pry *Priority) SetWeight(w byte)        { pry.weight = w }

func (pry *Priority) Deserialize(fr *FrameHeader) error {
	if len(fr.payload) < 5 {
		return ErrMissingBytes
	}

	pry.stream = bytesToUint32(fr.payload) & (1<<31 - 1)
	pry.weight = fr.payload[4]

	return nil
}

func (pry *Priority) Serialize(fr *FrameHeader) {
	fr.payload = appendUint32Bytes(fr.payload[:0], pry.stream)
	fr.payload = append(fr.payload, pry.weight)
}

// PushPromise notifies peers of server-initiated streams.
type PushPromise struct {
	pad    bool
	ended  bool
	stream uint32
	header []byte
}

func (pp *PushPromise) Type() FrameType { return FramePushPromise }

func (pp *PushPromise) Reset() {
	pp.pad = false
	pp.ended = false
	pp.stream = 0
	pp.header = pp.header[:0]
}

func (pp *PushPromise) SetHeader(h []byte) { pp.header = append(pp.header[:0], h...) }
func (pp *PushPromise) Write(b []byte) (int, error) {
	pp.header = append(pp.header, b...)
	return len(b), nil
}

func (pp *PushPromise) Deserialize(fr *FrameHeader) error {
	payload := fr.payload

	if fr.Flags().Has(FlagPadded) {
		var err error

		payload, err = cutPadding(payload, fr.Len())
		if err != nil {
			return err
		}
	}

	if len(payload) < 4 {
		return ErrMissingBytes
	}

	pp.stream = bytesToUint32(payload) & (1<<31 - 1)
	pp.header = append(pp.header[:0], payload[4:]...)
	pp.ended = fr.Flags().Has(FlagEndHeaders)

	return nil
}

func (pp *PushPromise) Serialize(fr *FrameHeader) {
	fr.payload = append(fr.payload[:0], pp.header...)
}

// RstStream terminates an active stream prematurely with an explicit error code.
type RstStream struct {
	code ErrorCode
}

func (rst *RstStream) Type() FrameType        { return FrameResetStream }
func (rst *RstStream) Code() ErrorCode        { return rst.code }
func (rst *RstStream) SetCode(code ErrorCode) { rst.code = code }
func (rst *RstStream) Reset()                 { rst.code = 0 }
func (rst *RstStream) Error() error           { return rst.code }

func (rst *RstStream) Deserialize(fr *FrameHeader) error {
	if len(fr.payload) < 4 {
		return ErrMissingBytes
	}

	rst.code = ErrorCode(bytesToUint32(fr.payload))

	return nil
}

func (rst *RstStream) Serialize(fr *FrameHeader) {
	fr.payload = appendUint32Bytes(fr.payload[:0], uint32(rst.code))
	fr.length = 4
}

// WindowUpdate adjusts connection or stream-level flow control capacity.
type WindowUpdate struct {
	increment int
}

func (wu *WindowUpdate) Type() FrameType      { return FrameWindowUpdate }
func (wu *WindowUpdate) Reset()               { wu.increment = 0 }
func (wu *WindowUpdate) Increment() int       { return wu.increment }
func (wu *WindowUpdate) SetIncrement(inc int) { wu.increment = inc }

func (wu *WindowUpdate) Deserialize(fr *FrameHeader) error {
	if len(fr.payload) < 4 {
		wu.increment = 0
		return ErrMissingBytes
	}

	wu.increment = int(bytesToUint32(fr.payload) & (1<<31 - 1))

	return nil
}

func (wu *WindowUpdate) Serialize(fr *FrameHeader) {
	fr.payload = appendUint32Bytes(fr.payload[:0], uint32(wu.increment)) //nolint:gosec
	fr.length = 4
}
