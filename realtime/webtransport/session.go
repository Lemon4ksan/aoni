// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webtransport

import (
	"context"
	"io"
	"sync"
	"sync/atomic"

	"github.com/lemon4ksan/aoni/internal/quic"
	"github.com/lemon4ksan/aoni/internal/quic/quicvarint"
)

// Transport defines the underlying connection capabilities required for WebTransport over HTTP/3.
type Transport interface {
	OpenStream() (*quic.Stream, error)
	OpenStreamSync(ctx context.Context) (*quic.Stream, error)
	OpenUniStream() (*quic.SendStream, error)
	OpenUniStreamSync(ctx context.Context) (*quic.SendStream, error)
	SendDatagram(p []byte) error
}

// Session represents an established WebTransport session over HTTP/3 (draft-ietf-webtrans-http3-16).
type Session struct {
	sessionID     uint64
	controlStream io.ReadWriteCloser
	transport     Transport

	ctx    context.Context
	cancel context.CancelFunc

	bidiStreams chan *Stream
	uniStreams  chan *ReceiveStream
	datagrams   chan []byte

	closed   atomic.Bool
	draining atomic.Bool

	mu       sync.RWMutex
	closeErr error
}

// NewSession creates an initialized WebTransport [Session] for the given session ID and transport.
func NewSession(
	parentCtx context.Context,
	sessionID uint64,
	controlStream io.ReadWriteCloser,
	transport Transport,
) *Session {
	ctx, cancel := context.WithCancel(parentCtx)

	s := &Session{
		sessionID:     sessionID,
		controlStream: controlStream,
		transport:     transport,
		ctx:           ctx,
		cancel:        cancel,
		bidiStreams:   make(chan *Stream, 64),
		uniStreams:    make(chan *ReceiveStream, 64),
		datagrams:     make(chan []byte, 128),
	}

	go s.controlLoop()

	return s
}

// SessionID returns the unique WebTransport session identifier (Stream ID of the Extended CONNECT request).
func (s *Session) SessionID() uint64 {
	return s.sessionID
}

// OpenStream opens a new bidirectional WebTransport stream (draft-ietf-webtrans-http3-16 §4.3).
func (s *Session) OpenStream() (*Stream, error) {
	if s.closed.Load() {
		return nil, ErrSessionClosed
	}

	if s.draining.Load() {
		return nil, ErrSessionDraining
	}

	raw, err := s.transport.OpenStream()
	if err != nil {
		return nil, err
	}

	return newOutgoingStream(raw, s.sessionID, uint64(raw.StreamID())), nil
}

// OpenStreamSync opens a new bidirectional WebTransport stream, blocking until available.
func (s *Session) OpenStreamSync(ctx context.Context) (*Stream, error) {
	if s.closed.Load() {
		return nil, ErrSessionClosed
	}

	if s.draining.Load() {
		return nil, ErrSessionDraining
	}

	raw, err := s.transport.OpenStreamSync(ctx)
	if err != nil {
		return nil, err
	}

	return newOutgoingStream(raw, s.sessionID, uint64(raw.StreamID())), nil
}

// AcceptStream accepts the next incoming bidirectional stream opened by the peer for this session.
func (s *Session) AcceptStream(ctx context.Context) (*Stream, error) {
	select {
	case <-s.ctx.Done():
		s.mu.RLock()
		defer s.mu.RUnlock()

		if s.closeErr != nil {
			return nil, s.closeErr
		}

		return nil, ErrSessionClosed

	case <-ctx.Done():
		return nil, ctx.Err()

	case str, ok := <-s.bidiStreams:
		if !ok {
			return nil, ErrSessionClosed
		}

		return str, nil
	}
}

// OpenUniStream opens a new outgoing unidirectional WebTransport stream (draft-ietf-webtrans-http3-16 §4.2).
func (s *Session) OpenUniStream() (*SendStream, error) {
	if s.closed.Load() {
		return nil, ErrSessionClosed
	}

	if s.draining.Load() {
		return nil, ErrSessionDraining
	}

	raw, err := s.transport.OpenUniStream()
	if err != nil {
		return nil, err
	}

	return newSendStream(raw, s.sessionID, uint64(raw.StreamID())), nil
}

// OpenUniStreamSync opens a new outgoing unidirectional stream, blocking until available.
func (s *Session) OpenUniStreamSync(ctx context.Context) (*SendStream, error) {
	if s.closed.Load() {
		return nil, ErrSessionClosed
	}

	if s.draining.Load() {
		return nil, ErrSessionDraining
	}

	raw, err := s.transport.OpenUniStreamSync(ctx)
	if err != nil {
		return nil, err
	}

	return newSendStream(raw, s.sessionID, uint64(raw.StreamID())), nil
}

// AcceptUniStream accepts the next incoming unidirectional stream opened by the peer for this session.
func (s *Session) AcceptUniStream(ctx context.Context) (*ReceiveStream, error) {
	select {
	case <-s.ctx.Done():
		s.mu.RLock()
		defer s.mu.RUnlock()

		if s.closeErr != nil {
			return nil, s.closeErr
		}

		return nil, ErrSessionClosed

	case <-ctx.Done():
		return nil, ctx.Err()

	case str, ok := <-s.uniStreams:
		if !ok {
			return nil, ErrSessionClosed
		}

		return str, nil
	}
}

var dgramBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 2048)
		return &b
	},
}

// SendDatagram transmits an unreliable datagram mapped to this WebTransport session (draft-ietf-webtrans-http3-16 §4.5).
// Prepends the Quarter Stream ID (sessionID / 4) per RFC 9297 Section 2.1 in a zero-alloc pooled buffer.
func (s *Session) SendDatagram(p []byte) error {
	if s.closed.Load() {
		return ErrSessionClosed
	}

	quarterID := s.sessionID / 4
	idLen := quicvarint.Len(quarterID)
	totalLen := idLen + len(p)

	bp := dgramBufPool.Get().(*[]byte)

	buf := *bp
	if cap(buf) < totalLen {
		buf = make([]byte, totalLen)
		*bp = buf
	} else {
		buf = buf[:totalLen]
	}

	_ = quicvarint.Append(buf[:0], quarterID)
	copy(buf[idLen:], p)

	err := s.transport.SendDatagram(buf)

	dgramBufPool.Put(bp)

	return err
}

// ReceiveDatagram awaits and returns the next unreliable datagram received for this session.
func (s *Session) ReceiveDatagram(ctx context.Context) ([]byte, error) {
	select {
	case <-s.ctx.Done():
		s.mu.RLock()
		defer s.mu.RUnlock()

		if s.closeErr != nil {
			return nil, s.closeErr
		}

		return nil, ErrSessionClosed

	case <-ctx.Done():
		return nil, ctx.Err()

	case dgram, ok := <-s.datagrams:
		if !ok {
			return nil, ErrSessionClosed
		}

		return dgram, nil
	}
}

// Drain sends a DRAIN_WEBTRANSPORT_SESSION capsule (0x78ae) signaling that no new streams will be opened.
func (s *Session) Drain() error {
	if s.closed.Load() {
		return ErrSessionClosed
	}

	if s.draining.CompareAndSwap(false, true) {
		var buf [16]byte

		n := EncodeDrainSessionCapsule(buf[:])

		if s.controlStream != nil {
			_, err := s.controlStream.Write(buf[:n])
			return err
		}
	}

	return nil
}

// CloseWithError terminates the session by sending a CLOSE_WEBTRANSPORT_SESSION capsule (0x2843).
func (s *Session) CloseWithError(code uint32, msg string) error {
	if s.closed.CompareAndSwap(false, true) {
		s.mu.Lock()
		s.closeErr = &SessionError{ErrorCode: code, Message: msg}
		s.mu.Unlock()

		s.cancel()

		var buf [512]byte

		n := EncodeCloseSessionCapsule(CloseSessionPayload{
			ApplicationErrorCode: code,
			ErrorMessage:         msg,
		}, buf[:])

		if s.controlStream != nil {
			go func(cs io.ReadWriteCloser, data []byte) {
				_, _ = cs.Write(data)
				_ = cs.Close()
			}(s.controlStream, buf[:n])
		}

		close(s.bidiStreams)
		close(s.uniStreams)
		close(s.datagrams)
	}

	return nil
}

// Close gracefully closes the session with error code 0.
func (s *Session) Close() error {
	return s.CloseWithError(0, "")
}

// EnqueueBidiStream delivers an incoming bidirectional stream to this session.
func (s *Session) EnqueueBidiStream(stream *Stream) {
	if s.closed.Load() {
		_ = stream.Close()
		return
	}

	select {
	case s.bidiStreams <- stream:
	default:
		_ = stream.Close()
	}
}

// EnqueueUniStream delivers an incoming unidirectional stream to this session.
func (s *Session) EnqueueUniStream(stream *ReceiveStream) {
	if s.closed.Load() {
		_ = stream.Close()
		return
	}

	select {
	case s.uniStreams <- stream:
	default:
		_ = stream.Close()
	}
}

// EnqueueDatagram delivers an incoming datagram to this session.
func (s *Session) EnqueueDatagram(data []byte) {
	if s.closed.Load() {
		return
	}

	select {
	case s.datagrams <- data:
	default:
		// Drop datagram when queue is full (RFC 9221 unreliability)
	}
}

// controlLoop reads capsules from the control stream until closed or EOF.
func (s *Session) controlLoop() {
	if s.controlStream == nil {
		return
	}

	for {
		// Read capsule type varint
		var firstByte [1]byte
		if _, err := io.ReadFull(s.controlStream, firstByte[:]); err != nil {
			s.cancel()
			return
		}

		tag := firstByte[0] >> 6
		varintLen := 1 << tag

		var typeBuf [8]byte

		typeBuf[0] = firstByte[0]
		if varintLen > 1 {
			if _, err := io.ReadFull(s.controlStream, typeBuf[1:varintLen]); err != nil {
				s.cancel()
				return
			}
		}

		capsuleType, _, err := DecodeVarint(typeBuf[:varintLen])
		if err != nil {
			s.cancel()
			return
		}

		// Read capsule payload length
		if _, err := io.ReadFull(s.controlStream, firstByte[:]); err != nil {
			s.cancel()
			return
		}

		tag = firstByte[0] >> 6
		varintLen = 1 << tag

		var lenBuf [8]byte

		lenBuf[0] = firstByte[0]
		if varintLen > 1 {
			if _, err := io.ReadFull(s.controlStream, lenBuf[1:varintLen]); err != nil {
				s.cancel()
				return
			}
		}

		payloadLen, _, err := DecodeVarint(lenBuf[:varintLen])
		if err != nil {
			s.cancel()
			return
		}

		payload := make([]byte, payloadLen)
		if payloadLen > 0 {
			if _, err := io.ReadFull(s.controlStream, payload); err != nil {
				s.cancel()
				return
			}
		}

		switch capsuleType {
		case CapsuleDrainWebTransportSession:
			s.draining.Store(true)

		case CapsuleCloseWebTransportSession:
			var closePayload CloseSessionPayload
			if err := DecodeCloseSessionPayloadTo(payload, &closePayload); err == nil {
				s.mu.Lock()
				s.closeErr = &SessionError{
					ErrorCode: closePayload.ApplicationErrorCode,
					Message:   closePayload.ErrorMessage,
				}
				s.mu.Unlock()
			}

			_ = s.Close()

			return

		case CapsuleWTMaxStreamsBidi, CapsuleWTMaxStreamsUni:
			// Parse flow control limits (draft-16 §5.6.2)
			_, _ = DecodeMaxStreamsPayload(payload)

		case CapsuleWTMaxData:
			// Parse session data limits (draft-16 §5.6.4)
			_, _ = DecodeMaxDataPayload(payload)

		case CapsuleWTStreamsBlockedBidi, CapsuleWTStreamsBlockedUni:
			// Informational signal (draft-16 §5.6.3)
			_, _ = DecodeMaxStreamsPayload(payload)

		case CapsuleWTDataBlocked:
			// Informational signal (draft-16 §5.6.5)
			_, _ = DecodeMaxDataPayload(payload)

		default:
			// Unknown capsule types MUST be silently ignored (RFC 9297 §3.2)
		}
	}
}
