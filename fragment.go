// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"
)

type fragmentCtxKey struct{}

// FragmentConfig specifies the chunk size and inter-chunk delay for connection fragmentation.
type FragmentConfig struct {
	ChunkSize int

	// LimitBytes specifies the maximum number of bytes to subject to fragmentation.
	// Once LimitBytes is exceeded, subsequent writes pass through seamlessly.
	// Set to -1 to fragment the entire stream.
	LimitBytes int64

	MinChunkSize int
	MaxChunkSize int

	MaxDelay time.Duration
	MinDelay time.Duration
}

type fragmentedConn struct {
	net.Conn
	chunkSize    int
	maxDelay     time.Duration
	minDelay     time.Duration
	maxChunkSize int
	minChunkSize int
	limitBytes   int64
	totalWritten int64
	mu           sync.Mutex // protects totalWritten
}

func (c *fragmentedConn) Write(b []byte) (n int, err error) {
	c.mu.Lock()
	limitExceeded := c.limitBytes > 0 && c.totalWritten >= c.limitBytes
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.totalWritten += int64(n)
		c.mu.Unlock()
	}()

	if limitExceeded {
		return c.Conn.Write(b)
	}

	if len(b) <= c.chunkSize {
		if c.maxDelay > 0 {
			time.Sleep(c.maxDelay)
		}

		return c.Conn.Write(b)
	}

	for n < len(b) {
		chunkSize := c.chunkSize

		if c.minChunkSize > 0 && c.maxChunkSize > c.minChunkSize {
			diff := c.maxChunkSize - c.minChunkSize
			ns := time.Now().UnixNano()
			chunkSize = c.minChunkSize + int(ns%int64(diff))
		}

		end := min(n+chunkSize, len(b))

		if c.maxDelay > 0 && n > 0 {
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

func (c *fragmentedConn) sleepWithJitter() {
	if c.maxDelay <= 0 {
		return
	}

	delay := c.maxDelay
	if c.minDelay > 0 && c.maxDelay > c.minDelay {
		// Calculate random jitter between minDelay and maxDelay
		diff := int64(c.maxDelay - c.minDelay)

		// Simple thread-safe pseudo-random generator
		ns := time.Now().UnixNano()
		jitter := time.Duration(ns % diff)
		delay = c.minDelay + jitter
	}

	time.Sleep(delay)
}

// WithFragmentation returns a RequestModifier that sets fragmentation configuration on the request context.
func WithFragmentation(cfg FragmentConfig) RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), fragmentCtxKey{}, cfg)
		*req = *req.WithContext(ctx)
	}
}

// NewFragmentedConn wraps a net.Conn with fragmentation and delay settings.
func NewFragmentedConn(conn net.Conn, cfg *FragmentConfig) net.Conn {
	var limit int64
	switch cfg.LimitBytes {
	case -1:
		// -1 means infinite fragmentation (no limit)
		limit = 0
	case 0:
		// Default safe limit to cover heavy/post-quantum TLS ClientHello (4 KB)
		limit = 4096
	default:
		// User-defined custom limit
		limit = cfg.LimitBytes
	}

	return &fragmentedConn{
		Conn:         conn,
		chunkSize:    cfg.ChunkSize,
		maxDelay:     cfg.MaxDelay,
		minDelay:     cfg.MinDelay,
		maxChunkSize: cfg.MaxChunkSize,
		minChunkSize: cfg.MinChunkSize,
		limitBytes:   limit,
	}
}

func wrapWithFragmentation(conn net.Conn, cfg FragmentConfig) net.Conn {
	return &fragmentedConn{
		Conn:      conn,
		chunkSize: cfg.ChunkSize,
		maxDelay:  cfg.MaxDelay,
	}
}
