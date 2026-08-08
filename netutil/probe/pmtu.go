// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package probe

import (
	"net"

	"golang.org/x/net/ipv6"
)

// PathMTUInfo holds Path MTU discovery results and recommended TCP MSS bounds.
type PathMTUInfo struct {
	MTU        int
	OptimalMSS int
	IsIPv6     bool
}

// DiscoverPathMTU queries the active Path MTU from a connected socket using IPv4/IPv6 control options.
//
// Mechanical Sympathy:
// Calculates optimal TCP MSS (Max Segment Size) based on real network path constraints,
// preventing socket-level IP packet fragmentation when p0f Don't Fragment (DF) is active.
func DiscoverPathMTU(conn net.Conn) (*PathMTUInfo, error) {
	if conn == nil {
		return nil, ErrSocketNotTCP
	}

	remoteIP := extractRemoteIP(conn)
	isV6 := remoteIP != nil && remoteIP.To4() == nil

	mtu, err := querySocketMTU(conn, isV6)
	if err != nil || mtu <= 0 {
		mtu = defaultFallbackMTU(isV6)
	}

	mss := calculateMSS(mtu, isV6)

	return &PathMTUInfo{
		MTU:        mtu,
		OptimalMSS: mss,
		IsIPv6:     isV6,
	}, nil
}

func querySocketMTU(conn net.Conn, isIPv6 bool) (int, error) {
	if isIPv6 {
		v6Conn := ipv6.NewConn(conn)

		mtu, err := v6Conn.PathMTU()
		if err == nil && mtu > 0 {
			return mtu, nil
		}
	}

	return queryRawSocketMTU(conn)
}

func queryRawSocketMTU(conn net.Conn) (int, error) {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return 0, ErrSocketNotTCP
	}

	raw, err := tcpConn.SyscallConn()
	if err != nil {
		return 0, err
	}

	var (
		mtu      int
		queryErr error
	)

	err = raw.Control(func(fd uintptr) {
		mtu, queryErr = getSocketMTUOption(fd)
	})
	if err != nil {
		return 0, err
	}

	return mtu, queryErr
}

func calculateMSS(mtu int, isIPv6 bool) int {
	ipHeaderLen := 20
	if isIPv6 {
		ipHeaderLen = 40
	}

	tcpHeaderLen := 20
	mss := mtu - ipHeaderLen - tcpHeaderLen

	if mss < 536 {
		return 536
	}

	return mss
}

func defaultFallbackMTU(isIPv6 bool) int {
	if isIPv6 {
		return 1280
	}

	return 1500
}

func extractRemoteIP(conn net.Conn) net.IP {
	if conn == nil || conn.RemoteAddr() == nil {
		return nil
	}

	host, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		return nil
	}

	return net.ParseIP(host)
}
