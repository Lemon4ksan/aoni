// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package netutil

import (
	"context"
	"net"
	"time"
)

// SocketController provides a low-level OS kernel hook executed after socket creation
// but prior to TCP SYN packet transmission.
type SocketController interface {
	// Control applies OS kernel socket options to file descriptor fd.
	Control(fd uintptr, network, address string) error
}

// HostRewriteConfig specifies host-to-host or host-to-IP remapping rules.
type HostRewriteConfig struct {
	Rules map[string]string
}

// TCPDelayRange defines minimum and maximum bounds for randomized pre-dial TCP delay jitter.
type TCPDelayRange struct {
	Min time.Duration
	Max time.Duration
}

// DNSResolver defines the hostname-to-IP lookup resolution contract.
type DNSResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}
