// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license can be found in the LICENSE file.

// Package fragment provides socket-level TCP payload write fragmentation.
//
// To evade Deep Packet Inspection (DPI) systems that inspect initial TLS ClientHello packet boundaries,
// [FragmentedConn] splits outbound socket writes into small, variable-sized chunks with pseudo-random inter-chunk delays.
package fragment

import (
	"net"
	"sync"
	"time"
)

// FragmentedConn wraps a net.Conn and fragments writes into chunks of specified size.
type FragmentedConn struct {
	net.Conn
	ChunkSize    int
	MaxDelay     time.Duration
	MinDelay     time.Duration
	MaxChunkSize int
	MinChunkSize int
	LimitBytes   int64
	totalWritten int64
	mu           sync.Mutex // protects totalWritten
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
		chunkSize := c.ChunkSize

		if c.MinChunkSize > 0 && c.MaxChunkSize > c.MinChunkSize {
			diff := c.MaxChunkSize - c.MinChunkSize
			ns := time.Now().UnixNano()
			chunkSize = c.MinChunkSize + int(ns%int64(diff))
		}

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

	return n, err
}

func (c *FragmentedConn) sleepWithJitter() {
	if c.MaxDelay <= 0 {
		return
	}

	delay := c.MaxDelay
	if c.MinDelay > 0 && c.MaxDelay > c.MinDelay {
		// Calculate random jitter between minDelay and maxDelay
		diff := int64(c.MaxDelay - c.MinDelay)

		// Simple thread-safe pseudo-random generator
		ns := time.Now().UnixNano()
		jitter := time.Duration(ns % diff)
		delay = c.MinDelay + jitter
	}

	time.Sleep(delay)
}
