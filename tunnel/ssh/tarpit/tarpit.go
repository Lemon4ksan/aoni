// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package tarpit provides an SSH tarpit implementation that locks connecting clients into an infinite banner loop.
package tarpit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"time"
)

// Trap locks a connecting client into an infinite RFC 4253 SSH banner loop,
// sending random text lines every delay interval to exhaust bot connection pools.
//
// Zero-CPU Cost:
// In Go, 10,000 trapped bots consume 0% CPU and negligible RAM thanks to runtime timers.
func Trap(ctx context.Context, conn net.Conn, delay time.Duration) {
	defer conn.Close()

	if delay <= 0 {
		delay = 10 * time.Second
	}

	var randBuf [8]byte

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
			_, _ = rand.Read(randBuf[:])
			banner := fmt.Sprintf("SSH-2.0-AoniTarpit_%s\r\n", hex.EncodeToString(randBuf[:]))

			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if _, err := conn.Write([]byte(banner)); err != nil {
				return
			}
		}
	}
}
