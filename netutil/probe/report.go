// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package probe provides network diagnostic utilities including ping, traceroute, and TLS chain inspection.
package probe

import (
	"context"
	"crypto/tls"
	"net"
	"time"
)

// FullReport aggregates Path MTU, Ping latency, Traceroute hops, and TLS chain metrics.
type FullReport struct {
	Target     string
	IP         net.IP
	Ping       *PingResult
	PMTU       *PathMTUInfo
	Traceroute *TracerouteResult
	TLSInfo    *CertChainInfo
	OpenPorts  []OpenPortResult
}

// RunFullDiagnostics executes a complete L3/L4/L7 diagnostic sequence against target.
func RunFullDiagnostics(ctx context.Context, conn net.Conn, target string, port int) *FullReport {
	report := &FullReport{
		Target: target,
	}

	if conn != nil {
		if pmtu, err := DiscoverPathMTU(conn); err == nil {
			report.PMTU = pmtu
		}

		if tc, ok := conn.(*tls.Conn); ok {
			state := tc.ConnectionState()
			report.TLSInfo = InspectTLSChain(&state)
		}
	}

	if pingRes, err := Ping(ctx, target, 2*time.Second); err == nil {
		report.Ping = pingRes
		report.IP = pingRes.IP
	}

	if traceRes, err := Traceroute(ctx, target, port, 20, 1*time.Second); err == nil {
		report.Traceroute = traceRes
	}

	if openPorts, err := ScanPorts(ctx, target, Top20Ports, 1*time.Second, 20); err == nil {
		report.OpenPorts = openPorts
	}

	return report
}
