// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webtransport

import (
	"context"
	"net"
	"testing"

	"github.com/lemon4ksan/aoni/internal/quic"
)

type noopTransport struct{}

func (n *noopTransport) OpenStream() (*quic.Stream, error) { return nil, nil }
func (n *noopTransport) OpenStreamSync(ctx context.Context) (*quic.Stream, error) {
	return nil, nil
}
func (n *noopTransport) OpenUniStream() (*quic.SendStream, error) { return nil, nil }
func (n *noopTransport) OpenUniStreamSync(ctx context.Context) (*quic.SendStream, error) {
	return nil, nil
}
func (n *noopTransport) SendDatagram(p []byte) error { return nil }

func BenchmarkEncodeCloseSessionCapsule(b *testing.B) {
	payload := CloseSessionPayload{
		ApplicationErrorCode: 100,
		ErrorMessage:         "benchmark close reason",
	}

	var buf [256]byte

	b.ReportAllocs()

	for b.Loop() {
		_ = EncodeCloseSessionCapsule(payload, buf[:])
	}
}

func BenchmarkDecodeCloseSessionPayloadTo(b *testing.B) {
	payload := CloseSessionPayload{
		ApplicationErrorCode: 100,
		ErrorMessage:         "benchmark close reason",
	}

	var buf [256]byte

	n := EncodeCloseSessionPayload(payload, buf[:])

	var target CloseSessionPayload

	b.ReportAllocs()

	for b.Loop() {
		_ = DecodeCloseSessionPayloadTo(buf[:n], &target)
	}
}

func BenchmarkSession_SendDatagram(b *testing.B) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sess := NewSession(ctx, 44, c1, &noopTransport{})
	defer sess.Close()

	payload := make([]byte, 1024)
	for i := range payload {
		payload[i] = byte(i)
	}

	b.ReportAllocs()

	for b.Loop() {
		_ = sess.SendDatagram(payload)
	}
}

func BenchmarkEncodeMaxStreamsCapsule(b *testing.B) {
	var buf [64]byte

	b.ReportAllocs()

	for b.Loop() {
		_, _ = EncodeMaxStreamsCapsule(CapsuleWTMaxStreamsBidi, 1000, buf[:])
	}
}

func BenchmarkEncodeMaxDataCapsule(b *testing.B) {
	var buf [64]byte

	b.ReportAllocs()

	for b.Loop() {
		_ = EncodeMaxDataCapsule(CapsuleWTMaxData, 1048576, buf[:])
	}
}
