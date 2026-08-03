// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import (
	"bufio"
	"bytes"
	"net"
	"net/http"
	"testing"
)

func BenchmarkBuildIPProxyURI(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		_ = BuildIPProxyURI("proxy.example.com", 8443, "2001:db8::1", "6")
	}
}

func BenchmarkEncodeVarint(b *testing.B) {
	buf := make([]byte, 8)

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		_ = EncodeVarint(1073741823, buf)
	}
}

func BenchmarkDecodeVarint(b *testing.B) {
	buf := make([]byte, 8)
	_ = EncodeVarint(1073741823, buf)

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		_, _, _ = DecodeVarint(buf)
	}
}

func BenchmarkEncodeAddressAssignHeader(b *testing.B) {
	buf := make([]byte, 16)

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		_ = EncodeAddressAssignHeader(1024, buf)
	}
}

func BenchmarkDecodeAddressAssignPayload(b *testing.B) {
	var (
		buf bytes.Buffer
		tmp [8]byte
	)

	// Entry 1: IPv4
	n := EncodeVarint(1, tmp[:])
	buf.Write(tmp[:n])
	buf.WriteByte(4)
	buf.Write(net.ParseIP("192.168.1.100").To4())
	buf.WriteByte(32)

	// Entry 2: IPv6
	n = EncodeVarint(2, tmp[:])
	buf.Write(tmp[:n])
	buf.WriteByte(6)
	buf.Write(net.ParseIP("2001:db8::1").To16())
	buf.WriteByte(128)

	payload := buf.Bytes()

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		_, _ = DecodeAddressAssignPayload(payload)
	}
}

func BenchmarkCONNECTIPHandshake(b *testing.B) {
	req, _ := http.NewRequest(http.MethodGet, "https://proxy.example.com/.well-known/masque/ip/*/*/", nil)
	req.Header.Set("Host", "proxy.example.com")
	req.Header.Set("Upgrade", ConnectIPUpgradeToken)
	req.Header.Set("Connection", "Upgrade")

	respData := []byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: connect-ip\r\nConnection: Upgrade\r\n\r\n")

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		clientConn, serverConn := net.Pipe()
		go func() {
			br := bufio.NewReader(serverConn)
			_, _ = http.ReadRequest(br)
			_, _ = serverConn.Write(respData)
		}()

		resp, err := performCONNECTIPHandshake(b.Context(), clientConn, req)
		if err == nil && resp != nil {
			_ = resp.Body.Close()
		}

		_ = clientConn.Close()
		_ = serverConn.Close()
	}
}
