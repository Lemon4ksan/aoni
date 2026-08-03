// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import (
	"context"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/lemon4ksan/aoni/netutil/tun"
)

// BridgeOptions configures BCP 38 ingress filtering and MTU limits for BridgeTUN.
type BridgeOptions struct {
	AllowedPrefixes []netip.Prefix
	MaxMTU          int
}

// BridgeTUN connects a tun.Adapter to an RFC 9484 connect-ip MASQUE tunnel.
func BridgeTUN(ctx context.Context, adapter tun.Adapter, masqueConn net.Conn) error {
	return BridgeTUNWithOptions(ctx, adapter, masqueConn, BridgeOptions{})
}

// BridgeTUNWithOptions connects a tun.Adapter to a MASQUE tunnel enforcing BCP 38 uRPF and MTU limits.
//
// Preconditions:
//   - adapter must be an active Layer 3 TUN interface (Windows, Linux, or macOS).
//   - masqueConn must be an established connect-ip stream.
func BridgeTUNWithOptions(
	ctx context.Context,
	adapter tun.Adapter,
	masqueConn net.Conn,
	opts BridgeOptions,
) error {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		<-ctx.Done()

		_ = masqueConn.SetReadDeadline(time.Now())
		_ = adapter.Close()
	}()

	wg.Add(2)

	// Goroutine 1: OS Kernel -> MASQUE Proxy
	go func() {
		defer wg.Done()

		buf := make([]byte, 65535)

		for {
			select {
			case <-ctx.Done():
				return
			default:
				n, err := adapter.Read(buf)
				if err != nil {
					cancel()
					return
				}

				if n == 0 {
					continue
				}

				packet := buf[:n]

				// BCP 38 / BCP 84 uRPF Ingress Source Address Validation (RFC 2827 / RFC 3704)
				srcIP := ExtractSrcIP(packet)
				if err := ValidateIngressSourceAddress(srcIP, opts.AllowedPrefixes); err != nil {
					// Drop spoofed / Martian packet without forwarding
					continue
				}

				// RFC 9484 Section 10.1 PMTUD MTU Limit Check
				if opts.MaxMTU > 0 && n > opts.MaxMTU {
					if icmpPkt, err := BuildICMPPacketTooBig(
						packet,
						uint32(opts.MaxMTU),
					); err == nil &&
						len(icmpPkt) > 0 {
						_, _ = adapter.Write(icmpPkt)
					}

					continue
				}

				if _, writeErr := masqueConn.Write(packet); writeErr != nil {
					cancel()
					return
				}
			}
		}
	}()

	// Goroutine 2: MASQUE Proxy -> OS Kernel
	go func() {
		defer wg.Done()

		buf := make([]byte, 65535)

		for {
			select {
			case <-ctx.Done():
				return
			default:
				n, err := masqueConn.Read(buf)
				if err != nil {
					cancel()
					return
				}

				if n > 0 {
					if _, writeErr := adapter.Write(buf[:n]); writeErr != nil {
						cancel()
						return
					}
				}
			}
		}
	}()

	wg.Wait()

	return nil
}

// BuildICMPPacketTooBig automatically creates an IPv4 (RFC 1191) or IPv6 (RFC 4443) ICMP Packet Too Big error.
func BuildICMPPacketTooBig(packet []byte, mtu uint32) ([]byte, error) {
	if len(packet) == 0 {
		return nil, ErrInvalidIPHeader
	}

	version := packet[0] >> 4
	if version == 4 {
		return BuildICMPPacketTooBig4(packet, uint16(mtu)) //nolint:gosec
	}

	if version == 6 {
		return BuildICMPPacketTooBig6(packet, mtu)
	}

	return nil, ErrInvalidIPHeader
}
