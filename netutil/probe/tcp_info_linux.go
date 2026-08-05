// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux

package probe

import (
	"net"
	"time"

	"golang.org/x/sys/unix"
)

// GetTCPInfo extracts raw TCP_INFO metrics directly from the Linux kernel network stack.
func GetTCPInfo(conn net.Conn) (*TCPInfo, error) {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return nil, ErrSocketNotTCP
	}

	raw, err := tcpConn.SyscallConn()
	if err != nil {
		return nil, err
	}

	var (
		info   *unix.TCPInfo
		getErr error
	)

	err = raw.Control(func(fd uintptr) {
		info, getErr = unix.GetsockoptTCPInfo(int(fd), unix.IPPROTO_TCP, unix.TCP_INFO)
	})

	if err != nil {
		return nil, err
	}
	if getErr != nil {
		return nil, getErr
	}

	return &TCPInfo{
		RTT:          time.Duration(info.Rtt) * time.Microsecond,
		RTTVar:       time.Duration(info.Rttvar) * time.Microsecond,
		RttMin:       time.Duration(info.Min_rtt) * time.Microsecond,
		SndCwnd:      info.Snd_cwnd,
		Retransmits:  uint32(info.Retransmits),
		TotalRetrans: info.Total_retrans,
	}, nil
}
