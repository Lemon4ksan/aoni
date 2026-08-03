// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque_test

import (
	"bufio"
	"bytes"
	"net"
	"net/http"
	"testing"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/netutil/masque"
)

func BenchmarkBuildIPProxyURI(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		_ = masque.BuildIPProxyURI("proxy.example.com", 8443, "2001:db8::1", "6")
	}
}

func BenchmarkEncodeVarint(b *testing.B) {
	buf := make([]byte, 8)

	b.ReportAllocs()

	for b.Loop() {
		_ = masque.EncodeVarint(1073741823, buf)
	}
}

func BenchmarkDecodeVarint(b *testing.B) {
	buf := make([]byte, 8)
	_ = masque.EncodeVarint(1073741823, buf)

	b.ReportAllocs()

	for b.Loop() {
		_, _, _ = masque.DecodeVarint(buf)
	}
}

func BenchmarkEncodeAddressAssignHeader(b *testing.B) {
	buf := make([]byte, 16)

	b.ReportAllocs()

	for b.Loop() {
		_ = masque.EncodeAddressAssignHeader(1024, buf)
	}
}

func BenchmarkDecodeAddressAssignPayloadTo_ZeroAlloc(b *testing.B) {
	var (
		buf bytes.Buffer
		tmp [8]byte
	)

	// Entry 1: IPv4
	n := masque.EncodeVarint(1, tmp[:])
	buf.Write(tmp[:n])
	buf.WriteByte(4)
	buf.Write(net.ParseIP("192.168.1.100").To4())
	buf.WriteByte(32)

	// Entry 2: IPv6
	n = masque.EncodeVarint(2, tmp[:])
	buf.Write(tmp[:n])
	buf.WriteByte(6)
	buf.Write(net.ParseIP("2001:db8::1").To16())
	buf.WriteByte(128)

	payload := buf.Bytes()
	dst := make([]masque.AssignedAddress, 0, 4)

	b.ReportAllocs()

	for b.Loop() {
		dst = dst[:0]

		var err error

		dst, err = masque.DecodeAddressAssignPayloadTo(payload, dst)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCONNECTIPHandshake(b *testing.B) {
	req, _ := http.NewRequest(http.MethodGet, "https://proxy.example.com/.well-known/masque/ip/*/*/", nil)
	req.Header.Set("Host", "proxy.example.com")
	req.Header.Set("Upgrade", masque.ConnectIPUpgradeToken)
	req.Header.Set("Connection", "Upgrade")

	respData := []byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: connect-ip\r\nConnection: Upgrade\r\n\r\n")

	b.ReportAllocs()

	for b.Loop() {
		clientConn, serverConn := net.Pipe()
		go func() {
			br := bufio.NewReader(serverConn)
			_, _ = http.ReadRequest(br)
			_, _ = serverConn.Write(respData)
		}()

		conn, resp, _ := masque.DialIPProxy(
			b.Context(),
			aoni.NewClient(nil),
			"https://proxy.example.com/.well-known/masque/ip/*/*/",
		)
		_ = conn
		_ = resp

		_ = clientConn.Close()
		_ = serverConn.Close()
	}
}
