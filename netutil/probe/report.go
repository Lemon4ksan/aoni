// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package probe provides focused network diagnostic utilities including TLS chain inspection,
// CDN IP detection, and kernel TCP connection metrics.
package probe

import (
	"crypto/tls"
	"net"
)

// FullReport aggregates TLS certificate chain details, CDN detection, and TCP connection metrics for an active connection.
type FullReport struct {
	Target      string
	IP          net.IP
	IsCDN       bool
	CDNProvider CDNProvider
	TLSInfo     *CertChainInfo
	TCPInfo     *TCPInfo
}

// RunConnectionDiagnostics extracts TLS chain info, CDN metadata, and TCP socket stats from an active connection.
func RunConnectionDiagnostics(conn net.Conn, target string) *FullReport {
	report := &FullReport{
		Target: target,
	}

	if conn == nil {
		return report
	}

	if remoteAddr := conn.RemoteAddr(); remoteAddr != nil {
		if tcpAddr, ok := remoteAddr.(*net.TCPAddr); ok {
			report.IP = tcpAddr.IP
			report.IsCDN, report.CDNProvider = CheckCDN(tcpAddr.IP)
		}
	}

	if tc, ok := conn.(*tls.Conn); ok {
		state := tc.ConnectionState()
		report.TLSInfo = InspectTLSChain(&state)
	}

	if tcpInfo, err := GetTCPInfo(conn); err == nil {
		report.TCPInfo = tcpInfo
	}

	return report
}
