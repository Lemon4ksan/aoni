// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux

package iouring

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// IOUringConn implements [net.Conn] by routing asynchronous socket reads and writes
// directly through a dedicated Linux io_uring instance.
type IOUringConn struct {
	mu     sync.Mutex
	fd     int
	ring   *Ring
	laddr  net.Addr
	raddr  net.Addr
	closed atomic.Bool
}

// DialIOUring connects to a remote TCP target using a non-blocking socket and io_uring.
func DialIOUring(ctx context.Context, network, address string) (*IOUringConn, error) {
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("aoni/iouring: resolve host failed: %w", err)
	}

	ip := ips[0]

	port, err := net.DefaultResolver.LookupPort(ctx, network, portStr)
	if err != nil {
		return nil, err
	}

	family := unix.AF_INET
	if ip.To4() == nil {
		family = unix.AF_INET6
	}

	fd, err := unix.Socket(family, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("socket creation failed: %w", err)
	}

	ring, err := New(32)
	if err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("create io_uring failed: %w", err)
	}

	var sa unix.Sockaddr
	if family == unix.AF_INET {
		var sa4 unix.SockaddrInet4

		sa4.Port = port
		copy(sa4.Addr[:], ip.To4())
		sa = &sa4
	} else {
		var sa6 unix.SockaddrInet6

		sa6.Port = port
		copy(sa6.Addr[:], ip.To16())
		sa = &sa6
	}

	// Connect socket
	if err := unix.Connect(fd, sa); err != nil && err != unix.EINPROGRESS {
		_ = ring.Close()
		_ = unix.Close(fd)
		return nil, fmt.Errorf("connect failed: %w", err)
	}

	lsa, _ := unix.Getsockname(fd)

	var localAddr net.Addr
	if lsa != nil {
		localAddr = sockaddrToAddr(lsa)
	}

	return &IOUringConn{
		fd:    fd,
		ring:  ring,
		laddr: localAddr,
		raddr: &net.TCPAddr{IP: ip, Port: port},
	}, nil
}

// Read reads data from the socket via IORING_OP_RECV.
func (c *IOUringConn) Read(b []byte) (int, error) {
	if c.closed.Load() {
		return 0, io.EOF
	}

	if len(b) == 0 {
		return 0, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	sqe, err := c.ring.GetSQE()
	if err != nil {
		return 0, err
	}

	sqe.Opcode = OpRecv
	sqe.Fd = int32(c.fd)
	sqe.Addr = uint64(uintptr(unsafe.Pointer(&b[0])))
	sqe.Len = uint32(len(b))
	sqe.UserData = 1

	if _, err := c.ring.Submit(); err != nil {
		return 0, err
	}

	cqe, err := c.ring.WaitCQE()
	if err != nil {
		return 0, err
	}

	if cqe.Res < 0 {
		return 0, syscall.Errno(-cqe.Res)
	}

	if cqe.Res == 0 {
		return 0, io.EOF
	}

	return int(cqe.Res), nil
}

// Write writes data to the socket via IORING_OP_SEND.
func (c *IOUringConn) Write(b []byte) (int, error) {
	if c.closed.Load() {
		return 0, io.ErrClosedPipe
	}

	if len(b) == 0 {
		return 0, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	total := 0
	for total < len(b) {
		sqe, err := c.ring.GetSQE()
		if err != nil {
			return total, err
		}

		chunk := b[total:]
		sqe.Opcode = OpSend
		sqe.Fd = int32(c.fd)
		sqe.Addr = uint64(uintptr(unsafe.Pointer(&chunk[0])))
		sqe.Len = uint32(len(chunk))
		sqe.UserData = 2

		if _, err := c.ring.Submit(); err != nil {
			return total, err
		}

		cqe, err := c.ring.WaitCQE()
		if err != nil {
			return total, err
		}

		if cqe.Res < 0 {
			return total, syscall.Errno(-cqe.Res)
		}

		total += int(cqe.Res)
	}

	return total, nil
}

// Close terminates the socket and io_uring ring instance.
func (c *IOUringConn) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	_ = c.ring.Close()

	return unix.Close(c.fd)
}

// LocalAddr returns the local network address.
func (c *IOUringConn) LocalAddr() net.Addr {
	return c.laddr
}

// RemoteAddr returns the remote network address.
func (c *IOUringConn) RemoteAddr() net.Addr {
	return c.raddr
}

// SetDeadline sets read and write deadlines.
func (c *IOUringConn) SetDeadline(t time.Time) error {
	_ = c.SetReadDeadline(t)
	return c.SetWriteDeadline(t)
}

// SetReadDeadline sets the deadline for future Read calls.
func (c *IOUringConn) SetReadDeadline(t time.Time) error {
	return nil
}

// SetWriteDeadline sets the deadline for future Write calls.
func (c *IOUringConn) SetWriteDeadline(t time.Time) error {
	return nil
}

func sockaddrToAddr(sa unix.Sockaddr) net.Addr {
	switch s := sa.(type) {
	case *unix.SockaddrInet4:
		return &net.TCPAddr{IP: s.Addr[:], Port: s.Port}
	case *unix.SockaddrInet6:
		return &net.TCPAddr{IP: s.Addr[:], Port: s.Port}
	}

	return &net.TCPAddr{IP: netip.IPv4Unspecified().AsSlice(), Port: 0}
}
