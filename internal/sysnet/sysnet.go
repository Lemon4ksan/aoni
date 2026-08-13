// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package sysnet provides low-level OS socket syscall overrides via syscall.RawConn,
// configuring TCP_QUICKACK, TCP_NODELAY, TCP_FASTOPEN, and SO_BUSY_POLL to minimize network tail latency.
package sysnet

import "net"

// TuneSocketConn applies low-latency OS syscall flags (TCP_NODELAY and socket buffer tuning) to a [net.Conn].
func TuneSocketConn(conn net.Conn) {
	TuneSocketConnWithFlags(conn, 0)
}

// TuneSocketConnWithFlags applies low-latency OS syscall flags (TCP_NODELAY, TCP_FASTOPEN, SO_BUSY_POLL) to a [net.Conn].
func TuneSocketConnWithFlags(conn net.Conn, flags uint64) {
	if conn == nil {
		return
	}

	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetNoDelay(true)
		_ = tcpConn.SetKeepAlive(true)

		rawConn, err := tcpConn.SyscallConn()
		if err != nil {
			return
		}

		_ = rawConn.Control(func(fd uintptr) {
			if flags == 0 {
				tuneSocketFD(fd)
			} else {
				tuneSocketFlagsFD(fd, flags)
			}
		})
	}
}

// WriteVectorBuffers writes multiple byte slice buffers to conn in a single OS vectorized syscall (WSASend on Windows, writev on Linux).
func WriteVectorBuffers(conn net.Conn, buffers [][]byte) (int64, error) {
	if conn == nil || len(buffers) == 0 {
		return 0, nil
	}

	if sysConn, ok := conn.(interface {
		SyscallConn() (syscallConn, error)
	}); ok {
		raw, err := sysConn.SyscallConn()
		if err == nil {
			var (
				written int64
				sysErr  error
			)

			errCtrl := raw.Write(func(fd uintptr) bool {
				written, sysErr = writeVectorBuffersFD(fd, buffers)

				return true
			})
			if errCtrl == nil && sysErr == nil {
				return written, nil
			}
		}
	}

	netBufs := make(net.Buffers, len(buffers))
	copy(netBufs, buffers)

	return netBufs.WriteTo(conn)
}

type syscallConn interface {
	Control(func(fd uintptr)) error
	Read(func(fd uintptr) bool) error
	Write(func(fd uintptr) bool) error
}

// BatchUDPConn wraps a [net.PacketConn] to execute vectorized UDP datagram batching.
type BatchUDPConn struct {
	net.PacketConn
}

// NewBatchUDPConn wraps pconn with vectorized socket datagram batching capabilities.
func NewBatchUDPConn(pconn net.PacketConn) *BatchUDPConn {
	return &BatchUDPConn{PacketConn: pconn}
}

// WriteVector writes multiple UDP datagram payload buffers in a single vectorized syscall.
func (c *BatchUDPConn) WriteVector(buffers [][]byte) (int64, error) {
	if c == nil || c.PacketConn == nil || len(buffers) == 0 {
		return 0, nil
	}

	if sysConn, ok := c.PacketConn.(interface {
		SyscallConn() (syscallConn, error)
	}); ok {
		raw, err := sysConn.SyscallConn()
		if err == nil {
			var (
				written int64
				sysErr  error
			)

			errCtrl := raw.Write(func(fd uintptr) bool {
				written, sysErr = writeVectorBuffersFD(fd, buffers)

				return true
			})
			if errCtrl == nil && sysErr == nil {
				return written, nil
			}
		}
	}

	var total int64
	for _, buf := range buffers {
		if len(buf) > 0 {
			n, err := c.WriteTo(buf, nil)

			total += int64(n)
			if err != nil {
				return total, err
			}
		}
	}

	return total, nil
}

type SyscallConnector interface {
	SyscallConn() (syscallConn, error)
}

// WriteVectorTo writes multiple UDP datagram payload buffers to a target destination address in a single vectorized syscall.
func (c *BatchUDPConn) WriteVectorTo(buffers [][]byte, addr net.Addr) (int64, error) {
	if c == nil || c.PacketConn == nil || len(buffers) == 0 {
		return 0, nil
	}

	if sysConn, ok := c.PacketConn.(SyscallConnector); ok {
		raw, err := sysConn.SyscallConn()
		if err == nil {
			var (
				written int64
				sysErr  error
			)

			errCtrl := raw.Write(func(fd uintptr) bool {
				written, sysErr = writeVectorBuffersFD(fd, buffers)

				return true
			})
			if errCtrl == nil && sysErr == nil {
				return written, nil
			}
		}
	}

	var total int64
	for _, buf := range buffers {
		if len(buf) > 0 {
			n, err := c.WriteTo(buf, addr)

			total += int64(n)
			if err != nil {
				return total, err
			}
		}
	}

	return total, nil
}

// SetReadBuffer sets the size of the operating system's receive buffer associated with the UDP connection.
func (c *BatchUDPConn) SetReadBuffer(bytes int) error {
	if u, ok := c.PacketConn.(*net.UDPConn); ok {
		return u.SetReadBuffer(bytes)
	}

	return nil
}

// SetWriteBuffer sets the size of the operating system's transmit buffer associated with the UDP connection.
func (c *BatchUDPConn) SetWriteBuffer(bytes int) error {
	if u, ok := c.PacketConn.(*net.UDPConn); ok {
		return u.SetWriteBuffer(bytes)
	}

	return nil
}

// SyscallConn returns a raw network connection for OS syscall access.
func (c *BatchUDPConn) SyscallConn() (syscallConn, error) {
	if sys, ok := c.PacketConn.(interface {
		SyscallConn() (syscallConn, error)
	}); ok {
		return sys.SyscallConn()
	}

	return nil, nil
}
