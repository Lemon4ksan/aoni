// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webtransport

import (
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/aoni/internal/quic"
	"github.com/lemon4ksan/aoni/internal/quic/quicvarint"
)

// RawStream defines the interface for underlying QUIC bidirectional streams.
type RawStream interface {
	io.ReadWriteCloser
	CancelRead(quic.StreamErrorCode)
	CancelWrite(quic.StreamErrorCode)
	SetDeadline(time.Time) error
	SetReadDeadline(time.Time) error
	SetWriteDeadline(time.Time) error
	Context() context.Context
}

// RawSendStream defines the interface for underlying QUIC unidirectional send streams.
type RawSendStream interface {
	io.WriteCloser
	CancelWrite(quic.StreamErrorCode)
	SetWriteDeadline(time.Time) error
	Context() context.Context
}

// RawReceiveStream defines the interface for underlying QUIC unidirectional receive streams.
type RawReceiveStream interface {
	io.Reader
	CancelRead(quic.StreamErrorCode)
	SetReadDeadline(time.Time) error
}

// Stream represents a bidirectional WebTransport stream multiplexed over QUIC (draft-ietf-webtrans-http3-16 §4.3).
// It implements standard [net.Conn] and [io.ReadWriteCloser].
type Stream struct {
	raw           RawStream
	sessionID     uint64
	streamID      uint64
	mu            sync.Mutex
	isClient      bool
	headerWritten atomic.Bool
	closed        atomic.Bool
}

// newOutgoingStream creates a new initiator-side bidirectional WebTransport stream.
func newOutgoingStream(raw RawStream, sessionID, streamID uint64) *Stream {
	return &Stream{
		raw:       raw,
		sessionID: sessionID,
		streamID:  streamID,
		isClient:  true,
	}
}

// newIncomingStream creates an incoming bidirectional WebTransport stream whose header has been read.
func newIncomingStream(raw RawStream, sessionID, streamID uint64) *Stream {
	s := &Stream{
		raw:       raw,
		sessionID: sessionID,
		streamID:  streamID,
		isClient:  false,
	}
	s.headerWritten.Store(true)

	return s
}

// Read reads data from the bidirectional stream.
func (s *Stream) Read(p []byte) (int, error) {
	if s.closed.Load() {
		return 0, ErrStreamClosed
	}

	return s.raw.Read(p)
}

// Write writes data to the bidirectional stream, automatically transmitting the frame header on first write.
func (s *Stream) Write(p []byte) (int, error) {
	if s.closed.Load() {
		return 0, ErrStreamClosed
	}

	if s.isClient && !s.headerWritten.Load() {
		s.mu.Lock()
		if !s.headerWritten.Load() {
			var hdrBuf [16]byte

			b := quicvarint.Append(hdrBuf[:0], FrameTypeWebTransportBidi)
			b = quicvarint.Append(b, s.sessionID)
			hdrLen := len(b)

			var (
				outBuf   []byte
				stackBuf [2048]byte
			)

			if hdrLen+len(p) <= len(stackBuf) {
				outBuf = stackBuf[:hdrLen+len(p)]
			} else {
				outBuf = make([]byte, hdrLen+len(p))
			}

			copy(outBuf, b)
			copy(outBuf[hdrLen:], p)

			s.headerWritten.Store(true)
			s.mu.Unlock()

			n, err := s.raw.Write(outBuf)
			if n > hdrLen {
				return n - hdrLen, err
			}

			return 0, err
		}

		s.mu.Unlock()
	}

	return s.raw.Write(p)
}

// Close gracefully closes the writing side of the stream.
func (s *Stream) Close() error {
	if s.closed.CompareAndSwap(false, true) {
		return s.raw.Close()
	}

	return nil
}

// CancelRead abruptly terminates reading on the stream with the specified error code.
// The error code is remapped into the WT_APPLICATION_ERROR space per draft-16 §4.4.
func (s *Stream) CancelRead(code uint32) {
	s.raw.CancelRead(quic.StreamErrorCode(WebTransportCodeToHTTPCode(code)))
}

// CancelWrite abruptly terminates writing on the stream with the specified error code.
// The error code is remapped into the WT_APPLICATION_ERROR space per draft-16 §4.4.
func (s *Stream) CancelWrite(code uint32) {
	s.raw.CancelWrite(quic.StreamErrorCode(WebTransportCodeToHTTPCode(code)))
}

// StreamID returns the underlying QUIC Stream ID.
func (s *Stream) StreamID() uint64 { return s.streamID }

// SessionID returns the WebTransport Session ID this stream belongs to.
func (s *Stream) SessionID() uint64 { return s.sessionID }

// SetDeadline sets read and write deadlines.
func (s *Stream) SetDeadline(t time.Time) error { return s.raw.SetDeadline(t) }

// SetReadDeadline sets read deadline.
func (s *Stream) SetReadDeadline(t time.Time) error { return s.raw.SetReadDeadline(t) }

// SetWriteDeadline sets write deadline.
func (s *Stream) SetWriteDeadline(t time.Time) error { return s.raw.SetWriteDeadline(t) }

// LocalAddr returns local dummy network address for [net.Conn] compatibility.
func (s *Stream) LocalAddr() net.Addr { return wtAddr{sessionID: s.sessionID} }

// RemoteAddr returns remote dummy network address for [net.Conn] compatibility.
func (s *Stream) RemoteAddr() net.Addr { return wtAddr{sessionID: s.sessionID} }

type wtAddr struct {
	sessionID uint64
}

func (a wtAddr) Network() string { return "webtransport" }
func (a wtAddr) String() string  { return "webtransport:session" }

// SendStream represents a unidirectional WebTransport send stream (draft-ietf-webtrans-http3-16 §4.2).
type SendStream struct {
	raw           RawSendStream
	sessionID     uint64
	streamID      uint64
	mu            sync.Mutex
	headerWritten atomic.Bool
	closed        atomic.Bool
}

func newSendStream(raw RawSendStream, sessionID, streamID uint64) *SendStream {
	return &SendStream{
		raw:       raw,
		sessionID: sessionID,
		streamID:  streamID,
	}
}

// Write writes data to the unidirectional stream, coalescing the 0x54 + SessionID header on first write.
func (s *SendStream) Write(p []byte) (int, error) {
	if s.closed.Load() {
		return 0, ErrStreamClosed
	}

	if !s.headerWritten.Load() {
		s.mu.Lock()
		if !s.headerWritten.Load() {
			var hdrBuf [16]byte

			b := quicvarint.Append(hdrBuf[:0], StreamTypeWebTransportUni)
			b = quicvarint.Append(b, s.sessionID)
			hdrLen := len(b)

			var (
				outBuf   []byte
				stackBuf [2048]byte
			)

			if hdrLen+len(p) <= len(stackBuf) {
				outBuf = stackBuf[:hdrLen+len(p)]
			} else {
				outBuf = make([]byte, hdrLen+len(p))
			}

			copy(outBuf, b)
			copy(outBuf[hdrLen:], p)

			s.headerWritten.Store(true)
			s.mu.Unlock()

			n, err := s.raw.Write(outBuf)
			if n > hdrLen {
				return n - hdrLen, err
			}

			return 0, err
		}

		s.mu.Unlock()
	}

	return s.raw.Write(p)
}

// Close gracefully closes the unidirectional stream.
func (s *SendStream) Close() error {
	if s.closed.CompareAndSwap(false, true) {
		return s.raw.Close()
	}

	return nil
}

// CancelWrite abruptly terminates writing on the stream with the specified application error code.
// The error code is remapped into the WT_APPLICATION_ERROR space per draft-16 §4.4.
func (s *SendStream) CancelWrite(code uint32) {
	s.raw.CancelWrite(quic.StreamErrorCode(WebTransportCodeToHTTPCode(code)))
}

// StreamID returns the underlying QUIC Stream ID.
func (s *SendStream) StreamID() uint64 { return s.streamID }

// ReceiveStream represents an incoming unidirectional WebTransport stream (draft-ietf-webtrans-http3-16 §4.2).
type ReceiveStream struct {
	raw       RawReceiveStream
	sessionID uint64
	streamID  uint64
	closed    atomic.Bool
}

func newReceiveStream(raw RawReceiveStream, sessionID, streamID uint64) *ReceiveStream {
	return &ReceiveStream{
		raw:       raw,
		sessionID: sessionID,
		streamID:  streamID,
	}
}

// Read reads data from the incoming unidirectional stream.
func (s *ReceiveStream) Read(p []byte) (int, error) {
	if s.closed.Load() {
		return 0, ErrStreamClosed
	}

	return s.raw.Read(p)
}

// Close closes the receive stream.
func (s *ReceiveStream) Close() error {
	s.closed.Store(true)
	return nil
}

// CancelRead abruptly terminates reading on the stream with the specified application error code.
// The error code is remapped into the WT_APPLICATION_ERROR space per draft-16 §4.4.
func (s *ReceiveStream) CancelRead(code uint32) {
	s.raw.CancelRead(quic.StreamErrorCode(WebTransportCodeToHTTPCode(code)))
}

// StreamID returns the underlying QUIC Stream ID.
func (s *ReceiveStream) StreamID() uint64 { return s.streamID }
