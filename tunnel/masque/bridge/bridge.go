// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bridge

import (
	"context"
	"net"
	"net/netip"
	"sync"
	"time"
	"unsafe"

	"github.com/lemon4ksan/foundation/net/packet/icmp"
	"github.com/lemon4ksan/foundation/net/packet/tcp"
	"github.com/lemon4ksan/foundation/silicon/offheap"

	"github.com/lemon4ksan/aoni/tunnel/masque/route"
	"github.com/lemon4ksan/aoni/tunnel/tun"
)

// Options configures BCP 38 / RFC 2827 ingress filtering (RFC 9484 §11), MTU boundaries (RFC 9484 §10.1), and MSS clamping (RFC 9293).
type Options struct {
	AllowedPrefixes []netip.Prefix
	MaxMTU          int
}

// TUN connects a Layer 3 TUN adapter to a MASQUE connect-ip session per RFC 9484.
func TUN(ctx context.Context, adapter tun.Adapter, masqueConn net.Conn) error {
	return TUNWithOptions(ctx, adapter, masqueConn, Options{})
}

// TUNWithOptions connects a TUN adapter to a MASQUE tunnel while enforcing BCP 38 / RFC 2827 uRPF (RFC 9484 §11),
// PMTUD ICMP Packet Too Big signaling (RFC 9484 §7.2.1 & §10.1), and TCP SYN MSS clamping (RFC 9293).
func TUNWithOptions(
	ctx context.Context,
	adapter tun.Adapter,
	masqueConn net.Conn,
	opts Options,
) error {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		<-ctx.Done()

		_ = masqueConn.SetReadDeadline(time.Now())
		_ = adapter.Close()
	}()

	wg.Go(func() { forwardAdapterToMasque(ctx, cancel, adapter, masqueConn, opts) })
	wg.Go(func() { forwardMasqueToAdapter(ctx, cancel, adapter, masqueConn) })

	wg.Wait()

	return nil
}

// BuildICMPPacketTooBig automatically creates an IPv4 (RFC 1191) or IPv6 (RFC 4443) ICMP Packet Too Big error.
func BuildICMPPacketTooBig(packet []byte, mtu uint32) ([]byte, error) {
	if len(packet) == 0 {
		return nil, icmp.ErrInvalidIPHeader
	}

	version := packet[0] >> 4
	switch version {
	case 4:
		return icmp.BuildPacketTooBig4(packet, uint16(mtu)) //nolint:gosec
	case 6:
		return icmp.BuildPacketTooBig6(packet, mtu)
	default:
		return nil, icmp.ErrInvalidIPHeader
	}
}

// forwardAdapterToMasque reads IP frames from the virtual TUN adapter and writes them to the MASQUE tunnel connection.
func forwardAdapterToMasque(
	ctx context.Context,
	cancel context.CancelFunc,
	adapter tun.Adapter,
	masqueConn net.Conn,
	opts Options,
) {
	vtable := route.NewIPProtocolVTable()

	// Register fast-path handlers for TCP (6), UDP (17), and ICMP (1/58)
	vtable.Register(6, func(packet []byte) error {
		if opts.MaxMTU > 0 {
			tcp.ClampMSSInPlace(packet, opts.MaxMTU)
		}

		return nil
	})

	_ = offheap.Scope(64*1024, func(arena *offheap.Arena) {
		ptr := arena.Alloc(65535)

		var buf []byte
		if ptr != nil {
			buf = unsafe.Slice((*byte)(ptr), 65535)
		} else {
			buf = make([]byte, 65535)
		}

		for {
			select {
			case <-ctx.Done():
				return
			default:
				n, err := adapter.Read(buf)
				if err != nil || n == 0 {
					if err != nil {
						cancel()
						return
					}

					continue
				}

				packet := buf[:n]

				srcIP := route.ExtractSrcIP(packet)
				if err := route.ValidateIngressSourceAddress(srcIP, opts.AllowedPrefixes); err != nil {
					continue
				}

				_ = vtable.DispatchIPPacket(packet)

				if opts.MaxMTU > 0 && n > opts.MaxMTU {
					handleMTUOverflow(adapter, packet, uint32(opts.MaxMTU))
					continue
				}

				if _, writeErr := masqueConn.Write(packet); writeErr != nil {
					cancel()
					return
				}
			}
		}
	})
}

// handleMTUOverflow generates an ICMP Packet Too Big response when ingress packet exceeds tunnel MTU bounds.
func handleMTUOverflow(adapter tun.Adapter, packet []byte, mtu uint32) {
	icmpPkt, err := BuildICMPPacketTooBig(packet, mtu)
	if err == nil && len(icmpPkt) > 0 {
		_, _ = adapter.Write(icmpPkt)
	}
}

// forwardMasqueToAdapter streams incoming IP packets from the MASQUE tunnel back to the virtual TUN device.
func forwardMasqueToAdapter(
	ctx context.Context,
	cancel context.CancelFunc,
	adapter tun.Adapter,
	masqueConn net.Conn,
) {
	_ = offheap.Scope(64*1024, func(arena *offheap.Arena) {
		ptr := arena.Alloc(65535)

		var buf []byte
		if ptr != nil {
			buf = unsafe.Slice((*byte)(ptr), 65535)
		} else {
			buf = make([]byte, 65535)
		}

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
	})
}
