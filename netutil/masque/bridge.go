// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/lemon4ksan/aoni/netutil/tun"
)

// BridgeTUN connects a TUN Adapter to an RFC 9484 connect-ip MASQUE tunnel.
//
// Execution Flow:
//   - Goroutine 1 (Kernel -> MASQUE): Reads IP packets from TUN device and writes to masqueConn.
//   - Goroutine 2 (MASQUE -> Kernel): Reads IP packets from masqueConn and writes back to TUN device.
func BridgeTUN(ctx context.Context, adapter tun.Adapter, masqueConn net.Conn) error {
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

				if n > 0 {
					if _, writeErr := masqueConn.Write(buf[:n]); writeErr != nil {
						cancel()
						return
					}
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
