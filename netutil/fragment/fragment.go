// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package fragment provides socket-level TCP payload write chunking to evade Deep Packet Inspection (DPI) systems.
package fragment

import (
	"net"
	"sync"
	"time"
)

// FragmentedConn wraps a [net.Conn] and splits socket writes into variable-sized chunks with inter-chunk delays.
type FragmentedConn struct {
	net.Conn
	ChunkSize    int
	MaxDelay     time.Duration
	MinDelay     time.Duration
	MaxChunkSize int
	MinChunkSize int
	LimitBytes   int64

	totalWritten int64
	mu           sync.Mutex
}

func (c *FragmentedConn) Write(b []byte) (n int, err error) {
	c.mu.Lock()
	limitExceeded := c.LimitBytes > 0 && c.totalWritten >= c.LimitBytes
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.totalWritten += int64(n)
		c.mu.Unlock()
	}()

	if limitExceeded {
		return c.Conn.Write(b)
	}

	if len(b) <= c.ChunkSize {
		if c.MaxDelay > 0 {
			time.Sleep(c.MaxDelay)
		}

		return c.Conn.Write(b)
	}

	for n < len(b) {
		chunkSize := c.resolveChunkSize()
		end := min(n+chunkSize, len(b))

		if c.MaxDelay > 0 && n > 0 {
			c.sleepWithJitter()
		}

		nw, err := c.Conn.Write(b[n:end])
		n += nw

		if err != nil {
			return n, err
		}
	}

	return n, nil
}

func (c *FragmentedConn) resolveChunkSize() int {
	if c.MinChunkSize > 0 && c.MaxChunkSize > c.MinChunkSize {
		diff := c.MaxChunkSize - c.MinChunkSize
		ns := time.Now().UnixNano()
		return c.MinChunkSize + int(ns%int64(diff))
	}

	return c.ChunkSize
}

func (c *FragmentedConn) sleepWithJitter() {
	if c.MaxDelay <= 0 {
		return
	}

	delay := c.MaxDelay
	if c.MinDelay > 0 && c.MaxDelay > c.MinDelay {
		diff := int64(c.MaxDelay - c.MinDelay)
		ns := time.Now().UnixNano()
		delay = c.MinDelay + time.Duration(ns%diff)
	}

	time.Sleep(delay)
}
