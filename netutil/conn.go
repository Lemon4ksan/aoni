// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package netutil

import (
	"net"
	"sync/atomic"
)

// WriteTrackingConn wraps a net.Conn to track exact byte counts written to the underlying socket.
type WriteTrackingConn struct {
	net.Conn
	written atomic.Int64
}

// NewWriteTrackingConn creates a WriteTrackingConn wrapping conn.
func NewWriteTrackingConn(conn net.Conn) *WriteTrackingConn {
	if conn == nil {
		return nil
	}

	return &WriteTrackingConn{Conn: conn}
}

func (c *WriteTrackingConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	if n > 0 {
		c.written.Add(int64(n))
	}

	return n, err
}

// BytesWritten returns the cumulative number of bytes written to the wire.
func (c *WriteTrackingConn) BytesWritten() int64 {
	return c.written.Load()
}

// ResetBytesWritten resets the write counter to zero for a new HTTP transaction.
func (c *WriteTrackingConn) ResetBytesWritten() {
	c.written.Store(0)
}
