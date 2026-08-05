// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !linux

package probe

import "net"

// GetTCPInfo is a stub for non-Linux operating systems.
func GetTCPInfo(_ net.Conn) (*TCPInfo, error) {
	return nil, ErrPMTUDiscoveryFailed
}
