// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !linux && !windows

package netdial

import (
	"context"
	"net"
)

func applyLinuxSocketOptions(_ uintptr, _ DialOptions) error {
	return nil
}

// DialRIOSocket falls back to standard DialDirectTCP on non-Windows OSes.
func DialRIOSocket(ctx context.Context, network, target string, opts DialOptions) (net.Conn, error) {
	return DialDirectTCP(ctx, network, target, "", opts)
}
