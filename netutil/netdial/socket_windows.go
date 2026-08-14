// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build windows

package netdial

import (
	"context"
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/windows"

	"github.com/lemon4ksan/aoni/internal/sysnet"
)

const wsaFlagRegisteredIO = 0x100

// DialRIOSocket creates a Windows socket with WSA_FLAG_REGISTERED_IO for Registered I/O (RIO) acceleration.
func DialRIOSocket(ctx context.Context, network, target string, opts DialOptions) (net.Conn, error) {
	af := windows.AF_INET
	if network == "tcp6" || network == "udp6" {
		af = windows.AF_INET6
	}

	sockType := windows.SOCK_STREAM
	if network == "udp" || network == "udp4" || network == "udp6" {
		sockType = windows.SOCK_DGRAM
	}

	proto := windows.IPPROTO_TCP
	if sockType == windows.SOCK_DGRAM {
		proto = windows.IPPROTO_UDP
	}

	flags := uint32(windows.WSA_FLAG_OVERLAPPED | wsaFlagRegisteredIO)

	handle, err := windows.WSASocket(int32(af), int32(sockType), int32(proto), nil, 0, flags)
	if err != nil {
		dialer := &net.Dialer{}

		return dialer.DialContext(ctx, network, target)
	}

	file := os.NewFile(uintptr(handle), target)
	defer file.Close()

	conn, err := net.FileConn(file)
	if err != nil {
		return nil, fmt.Errorf("aoni/netdial: failed to wrap RIO socket file handle: %w", err)
	}

	sysnet.TuneSocketConn(conn)

	return conn, nil
}

func applyLinuxSocketOptions(_ uintptr, _ DialOptions) error {
	return nil
}
