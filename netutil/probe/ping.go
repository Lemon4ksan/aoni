// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package probe

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/lemon4ksan/foundation/silicon/rand"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// PingResult holds the outcome and timing metadata of an ICMP echo probe.
type PingResult struct {
	Target string
	IP     net.IP
	RTT    time.Duration
	Bytes  int
}

// Ping measures round-trip latency to target using unprivileged or raw ICMP echo requests.
//
// Unprivileged Compatibility:
// Attempts unprivileged datagram ICMP sockets ("udp4"/"udp6") first, allowing rootless execution
// on Linux and macOS, falling back to raw ICMP sockets ("ip4:icmp") if elevated privileges are present.
//
// On success, returns a [*PingResult] populated with target IP, RTT duration, and payload byte size.
func Ping(ctx context.Context, target string, timeout time.Duration) (*PingResult, error) {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	ipAddr, err := net.ResolveIPAddr("ip", target)
	if err != nil {
		return nil, fmt.Errorf("aoni/probe: resolve ip failed: %w", err)
	}

	isV6 := ipAddr.IP.To4() == nil

	pconn, isUnprivileged, err := listenICMP(isV6)
	if err != nil {
		return nil, err
	}
	defer pconn.Close()

	id := os.Getpid() & 0xffff
	seq := generateRandomSeq()

	payload := make([]byte, 8)
	binary.BigEndian.PutUint64(payload, uint64(time.Now().UnixNano()))

	msgBytes, err := buildEchoMessage(isV6, id, seq, payload)
	if err != nil {
		return nil, err
	}

	var dst net.Addr = ipAddr
	if isUnprivileged {
		dst = &net.UDPAddr{IP: ipAddr.IP}
	}

	_ = pconn.SetDeadline(time.Now().Add(timeout))

	sendTime := time.Now()

	if _, err := pconn.WriteTo(msgBytes, dst); err != nil {
		return nil, fmt.Errorf("%w: send failed: %w", ErrICMPEchoFailed, err)
	}

	return readEchoReply(ctx, pconn, isV6, id, seq, sendTime, target, ipAddr.IP)
}

// listenICMP attempts to bind unprivileged datagram ICMP socket first, falling back to raw ICMP.
func listenICMP(isV6 bool) (*icmp.PacketConn, bool, error) {
	network := "udp4"

	bindAddr := "0.0.0.0"
	if isV6 {
		network = "udp6"
		bindAddr = "::"
	}

	pconn, err := icmp.ListenPacket(network, bindAddr)
	if err == nil {
		return pconn, true, nil
	}

	rawNetwork := "ip4:icmp"
	if isV6 {
		rawNetwork = "ip6:ipv6-icmp"
	}

	pconn, err = icmp.ListenPacket(rawNetwork, bindAddr)
	if err != nil {
		return nil, false, fmt.Errorf("aoni/probe: listen icmp failed: %w", err)
	}

	return pconn, false, nil
}

// buildEchoMessage constructs an ICMP Echo Request message with timestamp payload.
func buildEchoMessage(isV6 bool, id, seq int, payload []byte) ([]byte, error) {
	var msgType icmp.Type = ipv4.ICMPTypeEcho
	if isV6 {
		msgType = ipv6.ICMPTypeEchoRequest
	}

	msg := icmp.Message{
		Type: msgType,
		Code: 0,
		Body: &icmp.Echo{
			ID:   id,
			Seq:  seq,
			Data: payload,
		},
	}

	return msg.Marshal(nil)
}

// readEchoReply loops on the ICMP packet connection until matching Echo Reply is received or deadline expires.
func readEchoReply(
	ctx context.Context,
	pconn *icmp.PacketConn,
	isV6 bool,
	expectedID, expectedSeq int,
	sendTime time.Time,
	target string,
	ip net.IP,
) (*PingResult, error) {
	replyBuf := make([]byte, 1500)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		n, _, err := pconn.ReadFrom(replyBuf)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrICMPEchoFailed, err)
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

		if isEchoReply(msg.Type) {
			if echo, ok := msg.Body.(*icmp.Echo); ok {
				if echo.ID == expectedID && echo.Seq == expectedSeq {
					return &PingResult{
						Target: target,
						IP:     ip,
						RTT:    rtt,
						Bytes:  n,
					}, nil
				}
			}
		}
	}
}

// isEchoReply checks whether message type corresponds to an IPv4 or IPv6 Echo Reply.
func isEchoReply(typ icmp.Type) bool {
	return typ == ipv4.ICMPTypeEchoReply || typ == ipv6.ICMPTypeEchoReply
}

// generateRandomSeq produces a random 16-bit sequence integer for ICMP matching.
func generateRandomSeq() int {
	return int(rand.Uint32() & 0xFFFF)
}
