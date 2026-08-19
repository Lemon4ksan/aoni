// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
)

// ListenSOCKS5 starts a local SOCKS5 proxy server on localAddr (e.g. "127.0.0.1:1080"),
// routing all inbound connections through the remote SSH tunnel (equivalent to `ssh -D 1080`).
func (c *Client) ListenSOCKS5(ctx context.Context, localAddr string) (net.Listener, error) {
	if c.closed.Load() || c.Client == nil {
		return nil, ErrSSHClosed
	}

	var lc net.ListenConfig

	ln, err := lc.Listen(ctx, "tcp", localAddr)
	if err != nil {
		return nil, fmt.Errorf("aoni ssh socks5: listen failed on %s: %w", localAddr, err)
	}

	go c.serveSOCKS5Loop(ctx, ln)

	return ln, nil
}

func (c *Client) serveSOCKS5Loop(ctx context.Context, ln net.Listener) {
	defer ln.Close()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				if c.closed.Load() {
					return
				}

				continue
			}
		}

		go c.handleSOCKS5Conn(ctx, conn)
	}
}

func (c *Client) handleSOCKS5Conn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	br := bufio.NewReader(conn)

	// SOCKS5 greeting: VER, NMETHODS, METHODS...
	var hdr [2]byte
	if _, err := io.ReadFull(br, hdr[:]); err != nil || hdr[0] != 0x05 {
		return
	}

	methods := make([]byte, int(hdr[1]))
	if _, err := io.ReadFull(br, methods); err != nil {
		return
	}

	// Accept No Auth (0x00)
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return
	}

	// Request header: VER (1B), CMD (1B), RSV (1B), ATYP (1B)
	var reqHdr [4]byte
	if _, err := io.ReadFull(br, reqHdr[:]); err != nil || reqHdr[0] != 0x05 || reqHdr[1] != 0x01 {
		_, _ = conn.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	var host string
	switch reqHdr[3] {
	case 0x01: // IPv4
		var ip [4]byte
		if _, err := io.ReadFull(br, ip[:]); err != nil {
			return
		}

		host = net.IP(ip[:]).String()

	case 0x03: // Domain name
		domainLen, err := br.ReadByte()
		if err != nil {
			return
		}

		domainBuf := make([]byte, int(domainLen))
		if _, err := io.ReadFull(br, domainBuf); err != nil {
			return
		}

		host = string(domainBuf)

	case 0x04: // IPv6
		var ip [16]byte
		if _, err := io.ReadFull(br, ip[:]); err != nil {
			return
		}

		host = net.IP(ip[:]).String()

	default:
		_, _ = conn.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	var portBuf [2]byte
	if _, err := io.ReadFull(br, portBuf[:]); err != nil {
		return
	}

	port := int(portBuf[0])<<8 | int(portBuf[1])
	targetAddr := net.JoinHostPort(host, strconv.Itoa(port))

	remoteConn, err := c.DialContext(ctx, "tcp", targetAddr)
	if err != nil {
		_, _ = conn.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	defer remoteConn.Close()

	// Success reply
	if _, err := conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 127, 0, 0, 1, byte(port >> 8), byte(port)}); err != nil {
		return
	}

	pipeSOCKSConns(conn, remoteConn)
}

func pipeSOCKSConns(c1, c2 net.Conn) {
	var wg sync.WaitGroup

	wg.Go(func() { _, _ = io.Copy(c1, c2) })
	wg.Go(func() { _, _ = io.Copy(c2, c1) })

	wg.Wait()
}
