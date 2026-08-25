// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webtransport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/lemon4ksan/aoni/internal/fast/h3engine"
	"github.com/lemon4ksan/aoni/internal/qpack"
	"github.com/lemon4ksan/aoni/internal/quic"
	"github.com/lemon4ksan/aoni/internal/quic/quicvarint"
)

// SessionHandler handles an active incoming WebTransport session.
type SessionHandler interface {
	ServeWebTransport(sess *Session)
}

// SessionHandlerFunc allows using a plain function as [SessionHandler].
type SessionHandlerFunc func(sess *Session)

// ServeWebTransport calls fn(sess).
func (fn SessionHandlerFunc) ServeWebTransport(sess *Session) {
	fn(sess)
}

// ServerConfig configures an embedded WebTransport over HTTP/3 server.
type ServerConfig struct {
	Handler     SessionHandler
	CheckOrigin func(r *http.Request) bool
	MaxSessions int
}

// Server implements an embedded WebTransport over HTTP/3 server per RFC 9114, RFC 9220, and draft-ietf-webtrans-http3-16.
type Server struct {
	cfg      ServerConfig
	mu       sync.RWMutex
	sessions map[uint64]*Session
	closed   atomic.Bool
	done     chan struct{}
}

// NewServer creates a new WebTransport [Server] with the specified configuration.
func NewServer(cfg ServerConfig) *Server {
	if cfg.MaxSessions <= 0 {
		cfg.MaxSessions = 100
	}

	return &Server{
		cfg:      cfg,
		sessions: make(map[uint64]*Session),
		done:     make(chan struct{}),
	}
}

// HandleSession negotiates an incoming WebTransport session over an active HTTP/3 stream and QUIC connection.
func (s *Server) HandleSession(
	ctx context.Context,
	stream io.ReadWriteCloser,
	sessionID uint64,
	transport Transport,
) error {
	if s.closed.Load() {
		return ErrSessionClosed
	}

	session := NewSession(ctx, sessionID, stream, transport)

	s.mu.Lock()
	s.sessions[sessionID] = session
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.sessions, sessionID)
		s.mu.Unlock()

		_ = session.Close()
	}()

	// Send 200 OK HEADERS response on the connect stream (RFC 9114 §4.1 & draft-16 §3.2)
	headers := []qpack.HeaderField{
		{Name: ":status", Value: "200"},
	}

	var headerBuf bytes.Buffer

	qpackEnc := qpack.NewEncoder(&headerBuf)
	for _, h := range headers {
		if err := qpackEnc.WriteField(h); err != nil {
			return fmt.Errorf("aoni/webtransport: encode response header: %w", err)
		}
	}

	encodedHeaders := headerBuf.Bytes()

	var frameHdr [16]byte

	b := quicvarint.Append(frameHdr[:0], h3engine.FrameTypeHeaders)
	b = quicvarint.Append(b, uint64(len(encodedHeaders)))

	if _, err := stream.Write(b); err != nil {
		return fmt.Errorf("aoni/webtransport: write 200 response header: %w", err)
	}

	if _, err := stream.Write(encodedHeaders); err != nil {
		return fmt.Errorf("aoni/webtransport: write 200 response payload: %w", err)
	}

	if s.cfg.Handler != nil {
		s.cfg.Handler.ServeWebTransport(session)
	}

	return nil
}

// RouteIncomingBidi routes an incoming bidirectional stream to its corresponding session.
func (s *Server) RouteIncomingBidi(stream *quic.Stream) error {
	fType, _, err := readVarintFromStream(stream)
	if err != nil || fType != FrameTypeWebTransportBidi {
		_ = stream.Close()
		return errors.New("aoni/webtransport: invalid bidi frame header")
	}

	sessID, _, err := readVarintFromStream(stream)
	if err != nil {
		_ = stream.Close()
		return err
	}

	s.mu.RLock()
	sess, ok := s.sessions[sessID]
	s.mu.RUnlock()

	if !ok || sess == nil {
		_ = stream.Close()
		return ErrStreamRejected
	}

	sess.EnqueueBidiStream(newIncomingStream(stream, sessID, uint64(stream.StreamID())))

	return nil
}

// RouteIncomingBidiWithID routes an incoming bidirectional stream whose frame type was already consumed.
func (s *Server) RouteIncomingBidiWithID(stream *quic.Stream, sessID uint64) error {
	s.mu.RLock()
	sess, ok := s.sessions[sessID]
	s.mu.RUnlock()

	if !ok || sess == nil {
		_ = stream.Close()
		return ErrStreamRejected
	}

	sess.EnqueueBidiStream(newIncomingStream(stream, sessID, uint64(stream.StreamID())))

	return nil
}

// RouteIncomingUni routes an incoming unidirectional stream to its corresponding session.
func (s *Server) RouteIncomingUni(stream *quic.ReceiveStream) error {
	sType, _, err := readVarintFromReceiveStream(stream)
	if err != nil || sType != StreamTypeWebTransportUni {
		return errors.New("aoni/webtransport: invalid uni stream header")
	}

	sessID, _, err := readVarintFromReceiveStream(stream)
	if err != nil {
		return err
	}

	s.mu.RLock()
	sess, ok := s.sessions[sessID]
	s.mu.RUnlock()

	if !ok || sess == nil {
		return ErrStreamRejected
	}

	sess.EnqueueUniStream(newReceiveStream(stream, sessID, uint64(stream.StreamID())))

	return nil
}

// RouteIncomingDatagram routes an incoming datagram to its corresponding session.
func (s *Server) RouteIncomingDatagram(dgram []byte) error {
	if len(dgram) == 0 {
		return nil
	}

	quarterID, n, err := DecodeVarint(dgram)
	if err != nil {
		return err
	}

	sessID := quarterID * 4

	s.mu.RLock()
	sess, ok := s.sessions[sessID]
	s.mu.RUnlock()

	if ok && sess != nil {
		sess.EnqueueDatagram(dgram[n:])
	}

	return nil
}

// Close terminates the server and all active sessions.
func (s *Server) Close() error {
	if s.closed.CompareAndSwap(false, true) {
		close(s.done)

		s.mu.Lock()
		for _, sess := range s.sessions {
			_ = sess.Close()
		}

		s.sessions = nil
		s.mu.Unlock()
	}

	return nil
}
