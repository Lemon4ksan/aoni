// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import (
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"sync"
	"time"
	"unsafe"

	"github.com/lemon4ksan/aoni/internal/offheap"
	"github.com/lemon4ksan/aoni/internal/sysnet"
	"github.com/lemon4ksan/aoni/tunnel/tun"
)

// BridgeOptions configures BCP 38 ingress filtering, MTU boundaries, and MSS clamping.
type BridgeOptions struct {
	AllowedPrefixes []netip.Prefix
	MaxMTU          int
}

// BridgeTUN connects a Layer 3 TUN adapter to a MASQUE connect-ip session.
func BridgeTUN(ctx context.Context, adapter tun.Adapter, masqueConn net.Conn) error {
	return BridgeTUNWithOptions(ctx, adapter, masqueConn, BridgeOptions{})
}

// BridgeTUNWithOptions connects a TUN adapter to a MASQUE tunnel while enforcing BCP 38 uRPF,
// PMTUD ICMP signaling, and TCP SYN MSS clamping.
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

	wg.Go(func() { forwardAdapterToMasque(ctx, cancel, adapter, masqueConn, opts) })
	wg.Go(func() { forwardMasqueToAdapter(ctx, cancel, adapter, masqueConn) })

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

func forwardAdapterToMasque(
	ctx context.Context,
	cancel context.CancelFunc,
	adapter tun.Adapter,
	masqueConn net.Conn,
	opts BridgeOptions,
) {
	vtable := NewIPProtocolVTable()

	// Register fast-path handlers for TCP (6), UDP (17), and ICMP (1/58)
	vtable.Register(6, func(packet []byte) error {
		if opts.MaxMTU > 0 {
			ClampTCPMSSInPlace(packet, opts.MaxMTU)
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

				srcIP := ExtractSrcIP(packet)
				if err := ValidateIngressSourceAddress(srcIP, opts.AllowedPrefixes); err != nil {
					continue
				}

				_ = vtable.DispatchIPPacket(packet)

				if opts.MaxMTU > 0 && n > opts.MaxMTU {
					handleMTUOverflow(adapter, packet, uint32(opts.MaxMTU))
					continue
				}

				if _, writeErr := sysnet.WriteVectorBuffers(masqueConn, [][]byte{packet}); writeErr != nil {
					cancel()
					return
				}
			}
		}
	})
}

func handleMTUOverflow(adapter tun.Adapter, packet []byte, mtu uint32) {
	icmpPkt, err := BuildICMPPacketTooBig(packet, mtu)
	if err == nil && len(icmpPkt) > 0 {
		_, _ = adapter.Write(icmpPkt)
	}
}

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

// ClampTCPMSSInPlace inspects TCP SYN packets and overwrites the MSS option
// if it exceeds the calculated maximum MSS for the tunnel MTU.
func ClampTCPMSSInPlace(packet []byte, maxMTU int) {
	if len(packet) < 20 || maxMTU <= 40 {
		return
	}

	version := packet[0] >> 4

	maxMSS, ipHdrLen, ok := calculateMaxMSS(packet, version, maxMTU)
	if !ok {
		return
	}

	tcpHdr := packet[ipHdrLen:]
	if len(tcpHdr) < 20 || (tcpHdr[13]&0x02) == 0 {
		return
	}

	tcpDataOffset := int(tcpHdr[12]>>4) * 4
	if len(tcpHdr) < tcpDataOffset || tcpDataOffset < 20 {
		return
	}

	options := tcpHdr[20:tcpDataOffset]
	if updateMSSOption(options, maxMSS) {
		recalculateTCPChecksum(packet, version, ipHdrLen)
	}
}

func calculateMaxMSS(packet []byte, version byte, maxMTU int) (uint16, int, bool) {
	if version == 4 {
		ipHdrLen := int(packet[0]&0x0f) * 4
		if len(packet) < ipHdrLen+20 || packet[9] != 6 {
			return 0, 0, false
		}

		return uint16(maxMTU - 40), ipHdrLen, true
	}

	if version == 6 {
		ipHdrLen := 40
		if len(packet) < ipHdrLen+20 || packet[6] != 6 || maxMTU <= 60 {
			return 0, 0, false
		}

		return uint16(maxMTU - 60), ipHdrLen, true
	}

	return 0, 0, false
}

func updateMSSOption(options []byte, maxMSS uint16) bool {
	optIdx := 0
	for optIdx < len(options) {
		optKind := options[optIdx]
		if optKind == 0 {
			break
		}

		if optKind == 1 {
			optIdx++
			continue
		}

		if optIdx+1 >= len(options) {
			break
		}

		optLen := int(options[optIdx+1]) //nolint:gosec
		if optLen < 2 || optIdx+optLen > len(options) {
			break
		}

		if optKind == 2 && optLen == 4 {
			currentMSS := binary.BigEndian.Uint16(options[optIdx+2 : optIdx+4])
			if currentMSS > maxMSS {
				binary.BigEndian.PutUint16(options[optIdx+2:optIdx+4], maxMSS)
				return true
			}

			return false
		}

		optIdx += optLen
	}

	return false
}

func recalculateTCPChecksum(packet []byte, version byte, ipHdrLen int) {
	tcpHdr := packet[ipHdrLen:]
	tcpLen := len(packet) - ipHdrLen

	tcpHdr[16] = 0
	tcpHdr[17] = 0

	var sum uint32

	if version == 4 {
		sum += uint32(binary.BigEndian.Uint16(packet[12:14]))
		sum += uint32(binary.BigEndian.Uint16(packet[14:16]))
		sum += uint32(binary.BigEndian.Uint16(packet[16:18]))
		sum += uint32(binary.BigEndian.Uint16(packet[18:20]))
		sum += uint32(6)
		sum += uint32(tcpLen)
	} else {
		for i := 8; i < 40; i += 2 {
			sum += uint32(binary.BigEndian.Uint16(packet[i : i+2]))
		}

		sum += uint32(tcpLen)
		sum += uint32(6)
	}

	for i := 0; i < tcpLen-1; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(tcpHdr[i : i+2]))
	}

	if tcpLen%2 == 1 {
		sum += uint32(tcpHdr[tcpLen-1]) << 8
	}

	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}

	binary.BigEndian.PutUint16(tcpHdr[16:18], ^uint16(sum))
}
