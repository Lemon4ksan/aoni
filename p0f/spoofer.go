// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package p0f

import (
	"net"
	"slices"
)

// Spoofer applies TCP/IP field spoofing based on a p0f signature.
type Spoofer struct {
	sig *Signature
}

// NewSpoofer creates a new Spoofer for the given signature.
func NewSpoofer(sig *Signature) *Spoofer {
	return &Spoofer{sig: sig}
}

// Apply sets spoofable TCP/IP fields on a raw TCP connection.
// Currently supports: TTL, DF flag, window size.
// Platform-specific syscall implementations are in platform_*.go files.
func (s *Spoofer) Apply(conn net.Conn) error {
	if s.sig == nil {
		return nil
	}

	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return nil
	}

	raw, err := tcpConn.SyscallConn()
	if err != nil {
		return nil //nolint:nilerr
	}

	applySignature(raw, s.sig)

	return nil
}

func hasQuirk(quirks []string, target string) bool {
	return slices.Contains(quirks, target)
}
