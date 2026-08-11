// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package fragment provides socket-level TCP payload write chunking to evade Deep Packet Inspection (DPI) systems.
package fragment

import (
	"net"
	"sync"
	"time"

	"github.com/lemon4ksan/aoni/internal/bytesconv"
)

// Config configures write chunking and inter-chunk delays for TCP packet fragmentation.
type Config struct {
	LimitBytes    int64
	MaxDelay      time.Duration
	MinDelay      time.Duration
	ChunkSize     int
	MinChunkSize  int
	MaxChunkSize  int
	Pattern       []byte
	PatternOffset int
}

// FragmentedConn wraps a [net.Conn] and splits socket writes into variable-sized chunks with inter-chunk delays.
type FragmentedConn struct {
	net.Conn
	ChunkSize     int
	MaxDelay      time.Duration
	MinDelay      time.Duration
	MaxChunkSize  int
	MinChunkSize  int
	LimitBytes    int64
	Pattern       []byte
	PatternOffset int

	totalWritten int64
	mu           sync.Mutex
}

// NewFragmentedConn wraps a [net.Conn] with socket fragmentation and jitter delay wrappers.
func NewFragmentedConn(conn net.Conn, cfg *Config) net.Conn {
	if cfg == nil {
		return conn
	}

	var limit int64
	switch cfg.LimitBytes {
	case -1:
		limit = 0
	case 0:
		limit = 4096
	default:
		limit = cfg.LimitBytes
	}

	return &FragmentedConn{
		Conn:          conn,
		ChunkSize:     cfg.ChunkSize,
		MaxDelay:      cfg.MaxDelay,
		MinDelay:      cfg.MinDelay,
		MaxChunkSize:  cfg.MaxChunkSize,
		MinChunkSize:  cfg.MinChunkSize,
		LimitBytes:    limit,
		Pattern:       cfg.Pattern,
		PatternOffset: cfg.PatternOffset,
	}
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

	if len(c.Pattern) > 0 {
		slicer := bytesconv.NewPatternSlicer(c.Pattern, c.PatternOffset)
		if chunks, ok := slicer.Slice(b); ok && len(chunks) > 1 {
			for idx, chunk := range chunks {
				if idx > 0 && c.MaxDelay > 0 {
					c.sleepWithJitter()
				}

				nw, err := c.Conn.Write(chunk)

				n += nw
				if err != nil {
					return n, err
				}
			}

			return n, nil
		}
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
