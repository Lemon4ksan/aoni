// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package probe

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"golang.org/x/net/icmp"
)

// TracerouteRawICMP executes privileged RAW ICMP traceroute path discovery by sending
// raw ICMP echo request packets with incrementing TTL values.
//
// Privileged Execution:
// Requires root or CAP_NET_RAW privileges on Linux, or Administrator privileges on Windows,
// as it creates raw IP/ICMP sockets ("ip4:icmp" / "ip6:ipv6-icmp").
func TracerouteRawICMP(
	ctx context.Context,
	target string,
	maxHops int,
	timeoutPerHop time.Duration,
) (*TracerouteResult, error) {
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
	network := "ip4:icmp"

	bindAddr := "0.0.0.0"
	if isV6 {
		network = "ip6:ipv6-icmp"
		bindAddr = "::"
	}

	pconn, err := icmp.ListenPacket(network, bindAddr)
	if err != nil {
		return nil, fmt.Errorf("aoni/probe: listen raw icmp failed: %w", err)
	}
	defer pconn.Close()

	res := &TracerouteResult{
		Target: target,
		IP:     ipAddr.IP,
		Port:   0,
		Hops:   make([]Hop, 0, maxHops),
	}

	id := os.Getpid() & 0xffff

	for ttl := 1; ttl <= maxHops; ttl++ {
		select {
		case <-ctx.Done():
			return res, ctx.Err()
		default:
		}

		hop, reached, probeErr := probeRawICMPHop(ctx, pconn, ipAddr, isV6, id, ttl, timeoutPerHop)
		if probeErr != nil {
			res.Hops = append(res.Hops, Hop{Hop: ttl, RTT: timeoutPerHop, Reached: false})
			continue
		}

		res.Hops = append(res.Hops, hop)
		if reached {
			res.Reached = true
			break
		}
	}

	return res, nil
}

func probeRawICMPHop(
	ctx context.Context,
	pconn *icmp.PacketConn,
	dstIP *net.IPAddr,
	isV6 bool,
	id, ttl int,
	timeout time.Duration,
) (Hop, bool, error) {
	if err := setRawPacketTTL(pconn, ttl, isV6); err != nil {
		return Hop{}, false, err
	}

	seq := generateRandomSeq()
	payload := make([]byte, 8)

	msgBytes, err := buildEchoMessage(isV6, id, seq, payload)
	if err != nil {
		return Hop{}, false, err
	}

	_ = pconn.SetReadDeadline(time.Now().Add(timeout))

	sendTime := time.Now()

	if _, err := pconn.WriteTo(msgBytes, dstIP); err != nil {
		return Hop{}, false, err
	}

	return readRawICMPHopReply(ctx, pconn, isV6, id, seq, sendTime, dstIP.IP, ttl)
}

func setRawPacketTTL(pconn *icmp.PacketConn, ttl int, isV6 bool) error {
	if isV6 {
		if v6Conn := pconn.IPv6PacketConn(); v6Conn != nil {
			return v6Conn.SetHopLimit(ttl)
		}
	} else {
		if v4Conn := pconn.IPv4PacketConn(); v4Conn != nil {
			return v4Conn.SetTTL(ttl)
		}
	}

	return nil
}

func readRawICMPHopReply(
	ctx context.Context,
	pconn *icmp.PacketConn,
	isV6 bool,
	expectedID, expectedSeq int,
	sendTime time.Time,
	targetIP net.IP,
	ttl int,
) (Hop, bool, error) {
	replyBuf := make([]byte, 1500)

	for {
		select {
		case <-ctx.Done():
			return Hop{}, false, ctx.Err()
		default:
		}

		n, peer, err := pconn.ReadFrom(replyBuf)
		if err != nil {
			return Hop{Hop: ttl, RTT: time.Since(sendTime)}, false, err
		}

		rtt := time.Since(sendTime)

		proto := 1
		if isV6 {
			proto = 58
		}

		msg, err := icmp.ParseMessage(proto, replyBuf[:n])
		if err != nil {
			continue
		}

		hopIP := extractIPFromAddr(peer)

		if isEchoReply(msg.Type) {
			if echo, ok := msg.Body.(*icmp.Echo); ok {
				if echo.ID == expectedID && echo.Seq == expectedSeq {
					return Hop{
						Hop:     ttl,
						IP:      targetIP,
						RTT:     rtt,
						Reached: true,
					}, true, nil
				}
			}
		}

		if isTimeExceeded(msg.Type) {
			return Hop{
				Hop:     ttl,
				IP:      hopIP,
				RTT:     rtt,
				Reached: false,
			}, false, nil
		}
	}
}
