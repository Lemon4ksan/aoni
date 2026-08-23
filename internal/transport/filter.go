// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport

import (
	"context"
	"net"

	"github.com/lemon4ksan/foundation/silicon/sysnet"

	"github.com/lemon4ksan/aoni/netutil/fragment"
)

// ConnFilter defines the zero-allocation stream transformation contract.
// It receives an active [net.Conn], target host, and [DialConfig], applies an isolated network modification
// or protocol layer, and returns a transformed net.Conn stream wrapper.
type ConnFilter func(ctx context.Context, conn net.Conn, targetHost string, cfg *DialConfig) (net.Conn, error)

// ExecutePipeline sequentially executes a slice of active ConnFilter codecs over rawConn.
// If no filters are registered, rawConn is returned directly with zero allocations.
func ExecutePipeline(
	ctx context.Context,
	rawConn net.Conn,
	targetHost string,
	cfg *DialConfig,
	filters []ConnFilter,
) (net.Conn, error) {
	if len(filters) == 0 {
		return rawConn, nil
	}

	var err error

	currConn := rawConn

	for _, filter := range filters {
		if filter == nil {
			continue
		}

		select {
		case <-ctx.Done():
			_ = currConn.Close()
			return nil, ctx.Err()
		default:
		}

		currConn, err = filter(ctx, currConn, targetHost, cfg)
		if err != nil {
			_ = rawConn.Close()
			return nil, err
		}
	}

	return currConn, nil
}

// ApplyMSSLimit applies TCP MSS limits via OS socket options.
func ApplyMSSLimit(conn net.Conn, mss int) net.Conn {
	if mss <= 0 {
		return conn
	}

	tc, ok := conn.(*net.TCPConn)
	if !ok {
		return conn
	}

	raw, err := tc.SyscallConn()
	if err != nil {
		return conn
	}

	_ = raw.Control(func(fd uintptr) {
		sysnet.SetTCPMaxSeg(fd, mss)
	})

	return conn
}

// ApplyFragmentation wraps conn with packet chunk fragmentation settings.
func ApplyFragmentation(conn net.Conn, cfg fragment.Config) net.Conn {
	return &fragment.FragmentedConn{
		Conn:      conn,
		ChunkSize: cfg.ChunkSize,
		MaxDelay:  cfg.MaxDelay,
	}
}
