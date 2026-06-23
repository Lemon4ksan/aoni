// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"net"
	"net/http"
	"time"
)

type fragmentCtxKey struct{}

// FragmentConfig specifies the chunk size and inter-chunk delay for connection fragmentation.
type FragmentConfig struct {
	ChunkSize int
	MaxDelay  time.Duration
}

type fragmentedConn struct {
	net.Conn
	chunkSize int
	maxDelay  time.Duration
}

func (c *fragmentedConn) Write(b []byte) (n int, err error) {
	if len(b) <= c.chunkSize {
		if c.maxDelay > 0 {
			time.Sleep(c.maxDelay)
		}

		return c.Conn.Write(b)
	}

	var total int
	for total < len(b) {
		end := min(total+c.chunkSize, len(b))

		if c.maxDelay > 0 && total > 0 {
			time.Sleep(c.maxDelay)
		}

		nw, err := c.Conn.Write(b[total:end])

		total += nw
		if err != nil {
			return total, err
		}
	}

	return total, nil
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
	return &fragmentedConn{
		Conn:      conn,
		chunkSize: cfg.ChunkSize,
		maxDelay:  cfg.MaxDelay,
	}
}

func wrapWithFragmentation(conn net.Conn, cfg FragmentConfig) net.Conn {
	return &fragmentedConn{
		Conn:      conn,
		chunkSize: cfg.ChunkSize,
		maxDelay:  cfg.MaxDelay,
	}
}
