// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webtransport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/aoni/internal/quic"
	"github.com/lemon4ksan/aoni/internal/quic/quicvarint"
)

type mockTransport struct {
	mu            sync.Mutex
	datagramQueue chan []byte
	sentDatagrams [][]byte
}

func newMockTransport() *mockTransport {
	return &mockTransport{
		datagramQueue: make(chan []byte, 64),
		sentDatagrams: make([][]byte, 0),
	}
}

func (m *mockTransport) OpenStream() (*quic.Stream, error) {
	return nil, errors.New("mock: direct quic stream open not supported in unit test")
}

func (m *mockTransport) OpenStreamSync(ctx context.Context) (*quic.Stream, error) {
	return nil, errors.New("mock: direct quic stream open not supported in unit test")
}

func (m *mockTransport) OpenUniStream() (*quic.SendStream, error) {
	return nil, errors.New("mock: direct quic stream open not supported in unit test")
}

func (m *mockTransport) OpenUniStreamSync(ctx context.Context) (*quic.SendStream, error) {
	return nil, errors.New("mock: direct quic stream open not supported in unit test")
}

func (m *mockTransport) SendDatagram(p []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cp := make([]byte, len(p))
	copy(cp, p)
	m.sentDatagrams = append(m.sentDatagrams, cp)

	return nil
}

type mockRawStream struct {
	net.Conn
}

func (m *mockRawStream) CancelRead(code quic.StreamErrorCode)  {}
func (m *mockRawStream) CancelWrite(code quic.StreamErrorCode) {}
func (m *mockRawStream) Context() context.Context              { return context.Background() }

type mockRawSendStream struct {
	io.WriteCloser
}

func (m *mockRawSendStream) CancelWrite(code quic.StreamErrorCode) {}
func (m *mockRawSendStream) SetWriteDeadline(t time.Time) error    { return nil }
func (m *mockRawSendStream) Context() context.Context              { return context.Background() }

type mockRawReceiveStream struct {
	io.Reader
}

func (m *mockRawReceiveStream) CancelRead(code quic.StreamErrorCode) {}
func (m *mockRawReceiveStream) SetReadDeadline(t time.Time) error    { return nil }

func TestSession_SendReceiveDatagram(t *testing.T) {
	t.Parallel()

	transport := newMockTransport()

	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	go func() {
		var discard [512]byte
		for {
			if _, err := c2.Read(discard[:]); err != nil {
				return
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	sess := NewSession(ctx, 44, c1, transport) // Session ID 44 -> Quarter Stream ID 11
	defer sess.Close()

	payload := []byte("telemetry sample frame #123")
	if err := sess.SendDatagram(payload); err != nil {
		t.Fatalf("SendDatagram failed: %v", err)
	}

	transport.mu.Lock()
	if len(transport.sentDatagrams) != 1 {
		transport.mu.Unlock()
		t.Fatalf("expected 1 sent datagram, got %d", len(transport.sentDatagrams))
	}

	raw := transport.sentDatagrams[0]
	transport.mu.Unlock()

	// Verify Quarter Stream ID prefix
	quarterID, n, err := DecodeVarint(raw)
	if err != nil {
		t.Fatalf("decode quarter stream id failed: %v", err)
	}

	if quarterID != 11 {
		t.Fatalf("expected quarter stream ID 11 (44/4), got %d", quarterID)
	}

	if !bytes.Equal(raw[n:], payload) {
		t.Fatalf("payload mismatch: expected %q, got %q", string(payload), string(raw[n:]))
	}

	// Enqueue incoming datagram
	sess.EnqueueDatagram(payload)

	received, err := sess.ReceiveDatagram(ctx)
	if err != nil {
		t.Fatalf("ReceiveDatagram failed: %v", err)
	}

	if !bytes.Equal(received, payload) {
		t.Fatalf("received mismatch: expected %q, got %q", string(payload), string(received))
	}
}

func TestSession_BidirectionalStreamFraming(t *testing.T) {
	t.Parallel()

	p1, p2 := net.Pipe()
	defer p1.Close()
	defer p2.Close()

	rawClient := &mockRawStream{Conn: p1}
	clientStream := newOutgoingStream(rawClient, 44, 100)

	msg := []byte("hello webtransport")

	go func() {
		_, _ = clientStream.Write(msg)
	}()

	// Read on server pipe side
	var hdrBuf [64]byte

	n, err := p2.Read(hdrBuf[:])
	if err != nil {
		t.Fatalf("read from pipe failed: %v", err)
	}

	fType, off1, err := DecodeVarint(hdrBuf[:n])
	if err != nil {
		t.Fatalf("decode frame type failed: %v", err)
	}

	if fType != FrameTypeWebTransportBidi {
		t.Fatalf("expected frame type 0x%x, got 0x%x", FrameTypeWebTransportBidi, fType)
	}

	sessID, off2, err := DecodeVarint(hdrBuf[off1:n])
	if err != nil {
		t.Fatalf("decode session ID failed: %v", err)
	}

	if sessID != 44 {
		t.Fatalf("expected session ID 44, got %d", sessID)
	}

	readPayload := hdrBuf[off1+off2 : n]
	if !bytes.Equal(readPayload, msg) {
		t.Fatalf("payload mismatch: expected %q, got %q", string(msg), string(readPayload))
	}
}

func TestSession_UnidirectionalStreamFraming(t *testing.T) {
	t.Parallel()

	p1, p2 := net.Pipe()
	defer p1.Close()
	defer p2.Close()

	rawSend := &mockRawSendStream{WriteCloser: p1}
	sendStream := newSendStream(rawSend, 44, 102)

	msg := []byte("audio packet chunk")

	go func() {
		_, _ = sendStream.Write(msg)
	}()

	var hdrBuf [64]byte

	n, err := p2.Read(hdrBuf[:])
	if err != nil {
		t.Fatalf("read from pipe failed: %v", err)
	}

	sType, off1, err := DecodeVarint(hdrBuf[:n])
	if err != nil {
		t.Fatalf("decode stream type failed: %v", err)
	}

	if sType != StreamTypeWebTransportUni {
		t.Fatalf("expected stream type 0x%x, got 0x%x", StreamTypeWebTransportUni, sType)
	}

	sessID, off2, err := DecodeVarint(hdrBuf[off1:n])
	if err != nil {
		t.Fatalf("decode session ID failed: %v", err)
	}

	if sessID != 44 {
		t.Fatalf("expected session ID 44, got %d", sessID)
	}

	readPayload := hdrBuf[off1+off2 : n]
	if !bytes.Equal(readPayload, msg) {
		t.Fatalf("payload mismatch: expected %q, got %q", string(msg), string(readPayload))
	}
}

func TestSession_DrainAndClose(t *testing.T) {
	t.Parallel()

	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	sess := NewSession(ctx, 44, c1, newMockTransport())

	// Test Drain capsule
	go func() {
		_ = sess.Drain()
	}()

	var drainHdr [16]byte

	n, err := c2.Read(drainHdr[:])
	if err != nil {
		t.Fatalf("read drain capsule failed: %v", err)
	}

	cType, _, err := DecodeVarint(drainHdr[:n])
	if err != nil || cType != CapsuleDrainWebTransportSession {
		t.Fatalf("expected DRAIN capsule (0x%x), got 0x%x (err: %v)", CapsuleDrainWebTransportSession, cType, err)
	}

	// Verify session rejects opening new streams when draining
	if _, err := sess.OpenStream(); !errors.Is(err, ErrSessionDraining) {
		t.Fatalf("expected ErrSessionDraining, got %v", err)
	}

	if _, err := sess.OpenUniStream(); !errors.Is(err, ErrSessionDraining) {
		t.Fatalf("expected ErrSessionDraining, got %v", err)
	}

	// Test CloseWithError
	go func() {
		_ = sess.CloseWithError(404, "session closed by test")
	}()

	var closeBuf [512]byte

	n, err = c2.Read(closeBuf[:])
	if err != nil {
		t.Fatalf("read close capsule failed: %v", err)
	}

	cType, off1, err := DecodeVarint(closeBuf[:n])
	if err != nil || cType != CapsuleCloseWebTransportSession {
		t.Fatalf("expected CLOSE capsule (0x%x), got 0x%x (err: %v)", CapsuleCloseWebTransportSession, cType, err)
	}

	_, off2, err := DecodeVarint(closeBuf[off1:n])
	if err != nil {
		t.Fatalf("decode payload len failed: %v", err)
	}

	payload, err := DecodeCloseSessionPayload(closeBuf[off1+off2 : n])
	if err != nil {
		t.Fatalf("decode close payload failed: %v", err)
	}

	if payload.ApplicationErrorCode != 404 || payload.ErrorMessage != "session closed by test" {
		t.Fatalf("close payload mismatch: %+v", payload)
	}
}

func TestServer_RouteIncomingStreamsAndDatagrams(t *testing.T) {
	t.Parallel()

	srv := NewServer(ServerConfig{})
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctrl1, ctrl2 := net.Pipe()
	defer ctrl1.Close()
	defer ctrl2.Close()

	go func() {
		_ = srv.HandleSession(ctx, ctrl1, 44, newMockTransport())
	}()

	// Read 200 OK headers from server
	var buf [256]byte

	_, _ = ctrl2.Read(buf[:])

	// Route incoming datagram
	quarterID := uint64(11) // 44 / 4

	var dgramBuf [32]byte

	n := quicvarint.Append(dgramBuf[:0], quarterID)
	copy(dgramBuf[len(n):], []byte("dgram_payload"))

	if err := srv.RouteIncomingDatagram(dgramBuf[:len(n)+13]); err != nil {
		t.Fatalf("RouteIncomingDatagram failed: %v", err)
	}

	srv.mu.RLock()
	sess := srv.sessions[44]
	srv.mu.RUnlock()

	if sess == nil {
		t.Fatal("session 44 not found on server")
	}

	received, err := sess.ReceiveDatagram(ctx)
	if err != nil {
		t.Fatalf("ReceiveDatagram failed: %v", err)
	}

	if string(received) != "dgram_payload" {
		t.Fatalf("expected 'dgram_payload', got %q", string(received))
	}
}

func TestSession_ReceiveStream(t *testing.T) {
	t.Parallel()

	r := bytes.NewReader([]byte("stream data payload"))
	raw := &mockRawReceiveStream{Reader: r}
	recStr := newReceiveStream(raw, 44, 106)

	if recStr.StreamID() != 106 {
		t.Errorf("expected stream ID 106, got %d", recStr.StreamID())
	}

	buf := make([]byte, 64)

	n, err := recStr.Read(buf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if string(buf[:n]) != "stream data payload" {
		t.Errorf("payload mismatch: %q", string(buf[:n]))
	}

	recStr.CancelRead(0)

	if err := recStr.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	_, err = recStr.Read(buf)
	if !errors.Is(err, ErrStreamClosed) {
		t.Errorf("expected ErrStreamClosed after close, got %v", err)
	}
}

func TestWebTransportCodeConversion(t *testing.T) {
	t.Parallel()

	// Test boundary values per draft-16 §4.4
	codes := []uint32{0, 1, 0x1d, 0x1e, 0x1f, 30, 31, 404, 0xffffffff}

	for _, code := range codes {
		httpCode := WebTransportCodeToHTTPCode(code)

		// Verify within range [WTApplicationErrorFirst, WTApplicationErrorLast]
		if httpCode < WTApplicationErrorFirst || httpCode > WTApplicationErrorLast {
			t.Errorf(
				"code %d produced httpCode 0x%x out of range [0x%x, 0x%x]",
				code,
				httpCode,
				WTApplicationErrorFirst,
				WTApplicationErrorLast,
			)
		}

		// Verify not a reserved codepoint (0x1f*N + 0x21)
		if (httpCode-0x21)%0x1f == 0 {
			t.Errorf("code %d produced reserved httpCode 0x%x", code, httpCode)
		}

		// Verify round-trip conversion
		wtCode, ok := HTTPCodeToWebTransportCode(httpCode)
		if !ok {
			t.Errorf("failed to convert httpCode 0x%x back to WT code", httpCode)
		}

		if wtCode != code {
			t.Errorf("roundtrip mismatch: expected %d, got %d", code, wtCode)
		}
	}

	// Verify out-of-range HTTP codes return false
	_, okBelow := HTTPCodeToWebTransportCode(WTApplicationErrorFirst - 1)
	if okBelow {
		t.Errorf("expected false for code below WTApplicationErrorFirst")
	}

	_, okAbove := HTTPCodeToWebTransportCode(WTApplicationErrorLast + 1)
	if okAbove {
		t.Errorf("expected false for code above WTApplicationErrorLast")
	}

	// Verify reserved codepoint returns false
	reserved := WTApplicationErrorFirst
	for (reserved-0x21)%0x1f != 0 {
		reserved++
	}

	_, okReserved := HTTPCodeToWebTransportCode(reserved)
	if okReserved {
		t.Errorf("expected false for reserved HTTP/3 codepoint 0x%x", reserved)
	}
}

func TestSessionError_Format(t *testing.T) {
	t.Parallel()

	e1 := &SessionError{ErrorCode: 404, Message: "not found"}
	if e1.Error() != "aoni/webtransport: session closed with code 404: not found" {
		t.Errorf("unexpected error format: %q", e1.Error())
	}

	e2 := &SessionError{ErrorCode: 0, Message: ""}
	if e2.Error() != "aoni/webtransport: session closed with code 0" {
		t.Errorf("unexpected error format: %q", e2.Error())
	}
}
