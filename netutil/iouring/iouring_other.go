// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !linux

// Package iouring provides io_uring stubs for non-Linux platforms.
package iouring

import (
	"context"
	"errors"
	"net"
)

// ErrIOUringUnsupported is returned on non-Linux platforms where io_uring is unavailable.
var ErrIOUringUnsupported = errors.New("aoni/iouring: io_uring is only supported on Linux")

// Ring is a stub type on non-Linux operating systems.
type Ring struct{}

// New returns [ErrIOUringUnsupported] on non-Linux platforms.
func New(entries uint32, flags ...uint32) (*Ring, error) {
	return nil, ErrIOUringUnsupported
}

// DialIOUring returns [ErrIOUringUnsupported] on non-Linux platforms.
func DialIOUring(ctx context.Context, network, address string) (net.Conn, error) {
	return nil, ErrIOUringUnsupported
}
