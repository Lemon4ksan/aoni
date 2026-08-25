// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"
)

type mockDatagramTransport struct {
	mu       sync.Mutex
	sent     [][]byte
	incoming chan []byte
	closed   bool
}

func newMockDatagramTransport() *mockDatagramTransport {
	return &mockDatagramTransport{
		incoming: make(chan []byte, 100),
	}
}

func (m *mockDatagramTransport) SendDatagram(p []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return errors.New("closed")
	}

	cp := make([]byte, len(p))
	copy(cp, p)
	m.sent = append(m.sent, cp)

	return nil
}

func (m *mockDatagramTransport) ReceiveDatagram(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case data, ok := <-m.incoming:
		if !ok {
			return nil, io.EOF
		}

		return data, nil
	}
}

func (m *mockDatagramTransport) InjectDatagram(p []byte) {
	m.incoming <- p
}

type mockStream struct {
	readBuf  *bytes.Buffer
	writeBuf *bytes.Buffer
	mu       sync.Mutex
	closed   bool
}

func newMockStream() *mockStream {
	return &mockStream{
		readBuf:  new(bytes.Buffer),
		writeBuf: new(bytes.Buffer),
	}
}

func (s *mockStream) Read(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed && s.readBuf.Len() == 0 {
		return 0, io.EOF
	}

	return s.readBuf.Read(b)
}

func (s *mockStream) Write(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return 0, errors.New("stream closed")
	}

	return s.writeBuf.Write(b)
}

func (s *mockStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closed = true

	return nil
}

func (s *mockStream) ProvideInput(b []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.readBuf.Write(b)
}

func (s *mockStream) WrittenBytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.writeBuf.Bytes()
}

func TestSession_SendReceiveIPPacket(t *testing.T) {
	t.Parallel()

	stream := newMockStream()
	dgrams := newMockDatagramTransport()
	sess := NewSession(stream, dgrams)

	defer sess.Close()

	// Send an IP packet
	ipPkt := []byte{0x45, 0x00, 0x00, 0x14, 0x00, 0x01, 0x00, 0x00, 0x40, 0x06, 0x00, 0x00, 10, 0, 0, 1, 10, 0, 0, 2}
	err := sess.SendIPPacket(ipPkt)
	require.NoError(t, err)

	dgrams.mu.Lock()
	require.Len(t, dgrams.sent, 1)
	sentDatagram := dgrams.sent[0]
	dgrams.mu.Unlock()

	// Context ID should be 0 (1 byte 0x00)
	assert.Equal(t, byte(0x00), sentDatagram[0])
	assert.Equal(t, ipPkt, sentDatagram[1:])

	// Inject incoming datagram with Context ID 0
	incomingRaw := append([]byte{0x00}, ipPkt...)
	dgrams.InjectDatagram(incomingRaw)

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	receivedPkt, err := sess.ReceiveIPPacket(ctx)
	require.NoError(t, err)
	assert.Equal(t, ipPkt, receivedPkt)
}

func TestSession_Capsules(t *testing.T) {
	t.Parallel()

	stream := newMockStream()
	dgrams := newMockDatagramTransport()
	sess := NewSession(stream, dgrams)

	defer sess.Close()

	// Write Capsule
	payload := []byte{0x01, 0x02, 0x03}
	err := sess.WriteCapsule(CapsuleAddressRequest, payload)
	require.NoError(t, err)

	written := stream.WrittenBytes()
	cType, n1, err := DecodeVarint(written)
	require.NoError(t, err)
	assert.Equal(t, CapsuleAddressRequest, cType)

	pLen, n2, err := DecodeVarint(written[n1:])
	require.NoError(t, err)
	assert.Equal(t, uint64(len(payload)), pLen)
	assert.Equal(t, payload, written[n1+n2:])

	// Read Capsule
	var inBuf [64]byte

	n := EncodeCapsule(CapsuleAddressAssign, payload, inBuf[:])
	stream.ProvideInput(inBuf[:n])

	readType, readPayload, err := sess.ReadCapsule()
	require.NoError(t, err)
	assert.Equal(t, CapsuleAddressAssign, readType)
	assert.Equal(t, payload, readPayload)
}

func TestSession_Closed(t *testing.T) {
	t.Parallel()

	stream := newMockStream()
	dgrams := newMockDatagramTransport()
	sess := NewSession(stream, dgrams)

	require.NoError(t, sess.Close())

	err := sess.SendIPPacket([]byte{0x45})
	assert.ErrorIs(t, err, netErrClosed)

	_, err = sess.ReceiveIPPacket(t.Context())
	assert.ErrorIs(t, err, netErrClosed)

	err = sess.WriteCapsule(1, []byte{1})
	assert.ErrorIs(t, err, netErrClosed)

	_, _, err = sess.ReadCapsule()
	assert.ErrorIs(t, err, netErrClosed)
}

func TestSession_NilTransport(t *testing.T) {
	t.Parallel()

	sess := NewSession(nil, nil)

	err := sess.SendIPPacket([]byte{0x45})
	assert.Error(t, err)

	_, err = sess.ReceiveIPPacket(t.Context())
	assert.Error(t, err)

	err = sess.WriteCapsule(1, []byte{1})
	assert.Error(t, err)

	_, _, err = sess.ReadCapsule()
	assert.Error(t, err)
}

func TestDialH3IPProxy_NilConn(t *testing.T) {
	t.Parallel()

	_, _, err := DialH3IPProxy(t.Context(), nil, "https://example.com", []RequestedAddress{
		{
			Addr:         netip.MustParseAddr("10.0.0.1"),
			RequestID:    1,
			IPVersion:    4,
			PrefixLength: 32,
		},
	})
	assert.Error(t, err)
}
