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

// FullReport aggregates Path MTU, Ping latency, Traceroute hops, open ports, CDN detection, and Hardware OUI.
type FullReport struct {
	Target      string
	IP          net.IP
	IsCDN       bool
	CDNProvider CDNProvider
	Hardware    *HardwareInfo
	Ping        *PingResult
	PMTU        *PathMTUInfo
	Traceroute  *TracerouteResult
	TLSInfo     *CertChainInfo
	OpenPorts   []OpenPortResult
	Predictions []PortPrediction
}

// RunFullDiagnostics executes a complete L3/L4/L7 diagnostic sequence against target.
func RunFullDiagnostics(ctx context.Context, conn net.Conn, target string, port int) *FullReport {
	report := &FullReport{
		Target: target,
	}

	if pingRes, err := Ping(ctx, target, 2*time.Second); err == nil {
		report.Ping = pingRes
		report.IP = pingRes.IP

		report.IsCDN, report.CDNProvider = CheckCDN(pingRes.IP)

		if hw, err := ResolveHardwareInfo(pingRes.IP.String()); err == nil {
			report.Hardware = hw
		}
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

	if traceRes, err := Traceroute(ctx, target, port, 20, 1*time.Second); err == nil {
		report.Traceroute = traceRes
	}

	if openPorts, err := ScanPorts(ctx, target, Top20Ports, 1*time.Second, 20); err == nil {
		report.OpenPorts = openPorts

		openPortNumbers := make([]int, 0, len(openPorts))
		for _, op := range openPorts {
			openPortNumbers = append(openPortNumbers, op.Port)
		}

		predictor := NewPredictor()
		report.Predictions = predictor.Predict(openPortNumbers, 0.20)
	}

	return report
}
