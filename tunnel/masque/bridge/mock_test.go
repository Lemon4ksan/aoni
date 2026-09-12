// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bridge

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
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

	cp := make([]byte, s.writeBuf.Len())
	copy(cp, s.writeBuf.Bytes())

	return cp
}

type mockWSDialer struct {
	dialTLSFunc   func(ctx context.Context, addr string) (net.Conn, error)
	dialPlainFunc func(ctx context.Context, addr string) (net.Conn, error)
}

func (m *mockWSDialer) DialTLSForWS(ctx context.Context, addr string) (net.Conn, error) {
	if m.dialTLSFunc != nil {
		return m.dialTLSFunc(ctx, addr)
	}

	return nil, errors.New("mock dial tls not implemented")
}

func (m *mockWSDialer) DialPlainForWS(ctx context.Context, addr string) (net.Conn, error) {
	if m.dialPlainFunc != nil {
		return m.dialPlainFunc(ctx, addr)
	}

	return nil, errors.New("mock dial plain not implemented")
}
