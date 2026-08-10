// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package probe

import "errors"

var (
	// ErrSocketNotTCP indicates that a diagnostic operation requires an active TCP connection.
	ErrSocketNotTCP = errors.New("aoni/probe: connection is not a TCP socket")

	// ErrPMTUDiscoveryFailed is returned when Path MTU discovery fails or is unsupported on the host OS.
	ErrPMTUDiscoveryFailed = errors.New("aoni/probe: failed to discover path MTU")

	// ErrICMPEchoFailed is returned when an ICMP or UDP ping request fails to receive an echo response.
	ErrICMPEchoFailed = errors.New("aoni/probe: icmp echo request failed")

	// ErrTracerouteTimeout is returned when a traceroute hop probe times out without a response.
	ErrTracerouteTimeout = errors.New("aoni/probe: traceroute probe timed out")
)
