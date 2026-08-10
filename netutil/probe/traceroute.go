// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package probe

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// TraceMode defines the strategy used for traceroute path discovery.
type TraceMode int

const (
	// TraceModeAuto automatically selects Privileged ICMP if running as root, falling back to Unprivileged TCP.
	TraceModeAuto TraceMode = iota
	// TraceModeTCP forces unprivileged TCP SYN probing on a target port (No root required).
	TraceModeTCP
	// TraceModeICMP forces raw ICMP Echo probing (Requires root / CAP_NET_RAW).
	TraceModeICMP
)

// Hop represents a single intermediate router or final host discovered on a network path.
type Hop struct {
	Hop     int
	IP      net.IP
	RTT     time.Duration
	Reached bool
}

// TracerouteResult holds the complete path trajectory discovered during a traceroute probe.
type TracerouteResult struct {
	Target  string
	IP      net.IP
	Port    int
	Hops    []Hop
	Reached bool
}

// Traceroute discovers the network path to target by incrementing TCP SYN packet TTL or ICMP probes.
//
// Rootless Execution:
// Leverages socket-level IP_TTL options on standard TCP dialers combined with unprivileged
// ICMP listeners to capture Time Exceeded responses without requiring root/CAP_NET_RAW.
//
// On success, returns a [*TracerouteResult] containing the ordered list of intermediate router [Hop] entries.
func Traceroute(
	ctx context.Context,
	target string,
	port int,
	maxHops int,
	timeoutPerHop time.Duration,
) (*TracerouteResult, error) {
	if port <= 0 {
		port = 80
	}

	if maxHops <= 0 {
		maxHops = 30
	}

	if timeoutPerHop <= 0 {
		timeoutPerHop = 1 * time.Second
	}

	ipAddr, err := net.ResolveIPAddr("ip", target)
	if err != nil {
		return nil, fmt.Errorf("aoni/probe: resolve ip failed: %w", err)
	}

	isV6 := ipAddr.IP.To4() == nil

	icmpConn, _, _ := listenICMP(isV6)
	if icmpConn != nil {
		defer icmpConn.Close()
	}

	res := &TracerouteResult{
		Target: target,
		IP:     ipAddr.IP,
		Port:   port,
		Hops:   make([]Hop, 0, maxHops),
	}

	targetAddr := net.JoinHostPort(ipAddr.IP.String(), strconv.Itoa(port))

	for ttl := 1; ttl <= maxHops; ttl++ {
		select {
		case <-ctx.Done():
			return res, ctx.Err()
		default:
		}

		hop, reached := probeHop(ctx, targetAddr, ttl, isV6, timeoutPerHop, icmpConn)
		res.Hops = append(res.Hops, hop)

		if reached {
			res.Reached = true
			break
		}
	}

	return res, nil
}

// SmartTraceroute executes network path discovery using the optimal mode for current OS privileges.
func SmartTraceroute(
	ctx context.Context,
	target string,
	port int,
	maxHops int,
	mode TraceMode,
) (*TracerouteResult, error) {
	switch mode {
	case TraceModeAuto:
		if isRootUser() {
			res, err := TracerouteRawICMP(ctx, target, maxHops, 1*time.Second)
			if err == nil {
				return res, nil
			}
		}

		return Traceroute(ctx, target, port, maxHops, 1*time.Second)

	case TraceModeICMP:
		return TracerouteRawICMP(ctx, target, maxHops, 1*time.Second)
	default:
		return Traceroute(ctx, target, port, maxHops, 1*time.Second)
	}
}

func isRootUser() bool {
	return os.Geteuid() == 0
}

func probeHop(
	ctx context.Context,
	targetAddr string,
	ttl int,
	isV6 bool,
	timeout time.Duration,
	icmpConn *icmp.PacketConn,
) (Hop, bool) {
	dialer := &net.Dialer{
		Timeout: timeout,
		Control: func(_, _ string, c syscall.RawConn) error {
			var setErr error

			_ = c.Control(func(fd uintptr) {
				setErr = setSocketTTL(fd, ttl, isV6)
			})

			return setErr
		},
	}

	hopCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	conn, err := dialer.DialContext(hopCtx, "tcp", targetAddr)
	rtt := time.Since(start)

	if err == nil {
		_ = conn.Close()
		remoteIP := extractRemoteIP(conn)

		return Hop{
			Hop:     ttl,
			IP:      remoteIP,
			RTT:     rtt,
			Reached: true,
		}, true
	}

	hopIP := captureICMPTimeExceededIP(icmpConn, isV6, timeout)

	return Hop{
		Hop:     ttl,
		IP:      hopIP,
		RTT:     rtt,
		Reached: false,
	}, false
}

func captureICMPTimeExceededIP(icmpConn *icmp.PacketConn, isV6 bool, timeout time.Duration) net.IP {
	if icmpConn == nil {
		return nil
	}

	_ = icmpConn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 1500)

	n, peer, err := icmpConn.ReadFrom(buf)
	if err != nil {
		return nil
	}

	proto := 1
	if isV6 {
		proto = 58
	}

	msg, err := icmp.ParseMessage(proto, buf[:n])
	if err != nil {
		return nil
	}

	if isTimeExceeded(msg.Type) {
		return extractIPFromAddr(peer)
	}

	return nil
}

func isTimeExceeded(typ icmp.Type) bool {
	return typ == ipv4.ICMPTypeTimeExceeded || typ == ipv6.ICMPTypeTimeExceeded
}

func extractIPFromAddr(addr net.Addr) net.IP {
	if addr == nil {
		return nil
	}

	switch a := addr.(type) {
	case *net.IPAddr:
		return a.IP
	case *net.UDPAddr:
		return a.IP
	default:
		host, _, err := net.SplitHostPort(addr.String())
		if err == nil {
			return net.ParseIP(host)
		}

		return nil
	}
}
