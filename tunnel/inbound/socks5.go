// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inbound

import (
	"bufio"
	"context"
	"io"
	"net"
	"strconv"

	"github.com/lemon4ksan/aoni/netutil/netdial"
)

const (
	socks5Version = 0x05

	socksCmdConnect = 0x01

	socksAddrIPv4   = 0x01
	socksAddrDomain = 0x03
	socksAddrIPv6   = 0x04

	socksAuthNone     = 0x00
	socksAuthUserPass = 0x02
	socksAuthNoAccept = 0xFF

	socksRespSuccess = 0x00
	socksRespCmdFail = 0x01
)

func handleSOCKS5Conn(ctx context.Context, srv *Server, conn net.Conn, br *bufio.Reader) error {
	if err := handshakeSOCKS5Auth(srv, conn, br); err != nil {
		return err
	}

	targetHost, targetPort, err := parseSOCKS5Request(br)
	if err != nil {
		_ = sendSOCKS5Reply(conn, socksRespCmdFail, nil, 0)
		return err
	}

	if srv.EnableMITM && targetPort == 443 {
		if err := sendSOCKS5Reply(conn, socksRespSuccess, net.ParseIP("127.0.0.1"), 443); err != nil {
			return err
		}

		return handleHTTP2TLSInterception(ctx, srv, conn, br, targetHost, targetPort)
	}

	targetAddr := net.JoinHostPort(targetHost, strconv.Itoa(targetPort))

	outboundConn, err := netdial.DialL4(ctx, "tcp", targetAddr, netdial.DialOptions{
		SSRFGuard: false,
	})
	if err != nil {
		_ = sendSOCKS5Reply(conn, socksRespCmdFail, nil, 0)
		return err
	}
	defer outboundConn.Close()

	if err := sendSOCKS5Reply(conn, socksRespSuccess, net.ParseIP("127.0.0.1"), targetPort); err != nil {
		return err
	}

	pipeConns(conn, outboundConn)

	return nil
}

func handshakeSOCKS5Auth(srv *Server, conn net.Conn, br *bufio.Reader) error {
	var hdr [2]byte
	if _, err := io.ReadFull(br, hdr[:]); err != nil {
		return err
	}

	if hdr[0] != socks5Version {
		return ErrInvalidSocks5Header
	}

	numMethods := int(hdr[1])

	methods := make([]byte, numMethods)
	if _, err := io.ReadFull(br, methods); err != nil {
		return err
	}

	if srv.Auth == nil {
		_, err := conn.Write([]byte{socks5Version, socksAuthNone})
		return err
	}

	if !containsMethod(methods, socksAuthUserPass) {
		_, _ = conn.Write([]byte{socks5Version, socksAuthNoAccept})
		return ErrAuthFailed
	}

	if _, err := conn.Write([]byte{socks5Version, socksAuthUserPass}); err != nil {
		return err
	}

	return authenticateUserPass(srv, conn, br)
}

func containsMethod(methods []byte, target byte) bool {
	for _, m := range methods {
		if m == target {
			return true
		}
	}

	return false
}

func authenticateUserPass(srv *Server, conn net.Conn, br *bufio.Reader) error {
	var verByte [1]byte
	if _, err := io.ReadFull(br, verByte[:]); err != nil || verByte[0] != 0x01 {
		return ErrAuthFailed
	}

	userLenByte, err := br.ReadByte()
	if err != nil {
		return err
	}

	userBuf := make([]byte, int(userLenByte))
	if _, err := io.ReadFull(br, userBuf); err != nil {
		return err
	}

	passLenByte, err := br.ReadByte()
	if err != nil {
		return err
	}

	passBuf := make([]byte, int(passLenByte))
	if _, err := io.ReadFull(br, passBuf); err != nil {
		return err
	}

	if !srv.Auth(string(userBuf), string(passBuf)) {
		_, _ = conn.Write([]byte{0x01, 0x01})
		return ErrAuthFailed
	}

	_, err = conn.Write([]byte{0x01, 0x00})

	return err
}

func parseSOCKS5Request(br *bufio.Reader) (string, int, error) {
	var reqHdr [4]byte
	if _, err := io.ReadFull(br, reqHdr[:]); err != nil {
		return "", 0, err
	}

	if reqHdr[1] != socksCmdConnect {
		return "", 0, ErrUnsupportedCommand
	}

	var host string
	switch reqHdr[3] {
	case socksAddrIPv4:
		var ipBuf [4]byte
		if _, err := io.ReadFull(br, ipBuf[:]); err != nil {
			return "", 0, err
		}

		host = net.IP(ipBuf[:]).String()

	case socksAddrDomain:
		domainLen, err := br.ReadByte()
		if err != nil {
			return "", 0, err
		}

		domainBuf := make([]byte, int(domainLen))
		if _, err := io.ReadFull(br, domainBuf); err != nil {
			return "", 0, err
		}

		host = string(domainBuf)

	case socksAddrIPv6:
		var ipBuf [16]byte
		if _, err := io.ReadFull(br, ipBuf[:]); err != nil {
			return "", 0, err
		}

		host = net.IP(ipBuf[:]).String()

	default:
		return "", 0, ErrInvalidSocks5Header
	}

	var portBuf [2]byte
	if _, err := io.ReadFull(br, portBuf[:]); err != nil {
		return "", 0, err
	}

	port := int(portBuf[0])<<8 | int(portBuf[1])

	return host, port, nil
}

func sendSOCKS5Reply(conn net.Conn, status byte, bindIP net.IP, bindPort int) error {
	reply := []byte{socks5Version, status, 0x00, socksAddrIPv4, 0, 0, 0, 0, 0, 0}
	if bindIP != nil && bindIP.To4() != nil {
		copy(reply[4:8], bindIP.To4())
	}

	reply[8] = byte(bindPort >> 8)
	reply[9] = byte(bindPort)

	_, err := conn.Write(reply)

	return err
}
