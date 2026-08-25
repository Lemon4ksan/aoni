// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import (
	"context"
	"errors"
	"sync"
	"time"
	"unsafe"

	"github.com/lemon4ksan/foundation/silicon/offheap"

	"github.com/lemon4ksan/aoni/tunnel/tun"
)

// BridgeTUNDatagram connects a Layer 3 TUN adapter to a MASQUE Datagram session (RFC 9484 §5 & §6)
// while enforcing BCP 38 / RFC 2827 uRPF ingress filtering (RFC 9484 §11), PMTUD ICMP Packet Too Big
// signaling (RFC 9484 §7.2.1 & §10.1), and TCP SYN MSS clamping (RFC 9293) with 0 B/op.
func BridgeTUNDatagram(
	ctx context.Context,
	adapter tun.Adapter,
	session *Session,
	opts BridgeOptions,
) error {
	if adapter == nil {
		return errors.New("aoni/masque: nil tun adapter")
	}

	if session == nil {
		return errors.New("aoni/masque: nil masque session")
	}

	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		<-ctx.Done()

		_ = session.Close()
		_ = adapter.Close()
	}()

	wg.Go(func() { forwardAdapterToSession(ctx, cancel, adapter, session, opts) })
	wg.Go(func() { forwardSessionToAdapter(ctx, cancel, adapter, session) })

	wg.Wait()

	return nil
}

func forwardAdapterToSession(
	ctx context.Context,
	cancel context.CancelFunc,
	adapter tun.Adapter,
	session *Session,
	opts BridgeOptions,
) {
	vtable := NewIPProtocolVTable()

	// Register fast-path handler for TCP (6) to enforce MSS Clamping
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

				if sendErr := session.SendIPPacket(packet); sendErr != nil {
					cancel()
					return
				}
			}
		}
	})
}

func forwardSessionToAdapter(
	ctx context.Context,
	cancel context.CancelFunc,
	adapter tun.Adapter,
	session *Session,
) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Read IP packet with a short deadline to check context
			readCtx, readCancel := context.WithTimeout(ctx, 500*time.Millisecond)
			pkt, err := session.ReceiveIPPacket(readCtx)

			readCancel()

			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					continue
				}

				if errors.Is(err, context.Canceled) || errors.Is(err, netErrClosed) {
					return
				}

				cancel()

				return
			}

			if len(pkt) > 0 {
				if _, writeErr := adapter.Write(pkt); writeErr != nil {
					cancel()
					return
				}
			}
		}
	}
}
