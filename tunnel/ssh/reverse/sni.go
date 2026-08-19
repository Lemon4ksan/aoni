// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package reverse

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"io"
	"net"
)

// PeekSNI peeks the initial TLS ClientHello bytes of conn to extract the SNI hostname
// without terminating or decrypting the TLS session.
//
// Zero-Copy Guarantee:
// Returns a wrapped [net.Conn] that replays the peeked bytes during subsequent reads.
func PeekSNI(conn net.Conn) (string, net.Conn, error) {
	br := bufio.NewReaderSize(conn, 4096)

	header, err := br.Peek(5)
	if err != nil || len(header) < 5 || header[0] != 0x16 {
		return "", &bufferedConnBridge{Conn: conn, br: br}, ErrInvalidTLSHeader
	}

	recordLen := int(header[3])<<8 | int(header[4])

	helloBytes, err := br.Peek(5 + recordLen)
	if err != nil {
		return "", &bufferedConnBridge{Conn: conn, br: br}, err
	}

	var sniHost string

	tlsCfg := &tls.Config{
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			sniHost = hello.ServerName
			return nil, nil
		},
	}

	_ = tls.Server(&readOnlyConn{r: bytes.NewReader(helloBytes)}, tlsCfg).Handshake() //nolint:noctx

	return sniHost, &bufferedConnBridge{Conn: conn, br: br}, nil
}

type bufferedConnBridge struct {
	net.Conn
	br *bufio.Reader
}

func (b *bufferedConnBridge) Read(p []byte) (int, error) {
	if b.br.Buffered() > 0 {
		return b.br.Read(p)
	}

	return b.Conn.Read(p)
}

type readOnlyConn struct {
	net.Conn
	r io.Reader
}

func (c *readOnlyConn) Read(p []byte) (int, error)  { return c.r.Read(p) }
func (c *readOnlyConn) Write(_ []byte) (int, error) { return 0, io.EOF }
