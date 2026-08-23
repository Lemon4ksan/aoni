// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package netdial

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/foundation/generic"
)

// ErrNoNICsAvailable is returned when no valid local network interfaces or IPs are configured.
var ErrNoNICsAvailable = errors.New("aoni/netdial: no local network interfaces or IPs available")

// ContextDialer specifies a dialer that supports context-cancellable network connection creation.
type ContextDialer interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

// MultiNICConfig configures multi-interface socket racing parameters.
type MultiNICConfig struct {
	// Interfaces specifies the names of local network interfaces to race (e.g. "eth0", "wlan0").
	Interfaces []string

	// LocalAddrs specifies explicit local IP addresses to bind and race.
	LocalAddrs []net.IP

	// StaggerDelay defines the staggered start interval between consecutive interface dial attempts.
	// Defaults to 25ms (RFC 8305 / Apple Happy Eyeballs standard).
	StaggerDelay time.Duration

	// DialTimeout specifies the maximum per-attempt connection timeout.
	DialTimeout time.Duration

	// FallbackDialer is used when all multi-NIC attempts fail or if no NICs match.
	FallbackDialer ContextDialer
}

// MultiNICDialer races TCP/UDP connection attempts across multiple physical network interfaces.
type MultiNICDialer struct {
	cfg        MultiNICConfig
	localIPs   []net.IP
	candidates []net.Addr
}

// NewMultiNICDialer constructs a [MultiNICDialer] configured with the provided options.
func NewMultiNICDialer(cfg MultiNICConfig) (*MultiNICDialer, error) {
	cfg.StaggerDelay = generic.Coalesce(cfg.StaggerDelay, 25*time.Millisecond)
	cfg.DialTimeout = generic.Coalesce(cfg.DialTimeout, 10*time.Second)

	d := &MultiNICDialer{
		cfg: cfg,
	}

	for _, ip := range cfg.LocalAddrs {
		if ip != nil {
			d.localIPs = append(d.localIPs, ip)
			d.candidates = append(d.candidates, &net.TCPAddr{IP: ip})
		}
	}

	for _, ifaceName := range cfg.Interfaces {
		iface, err := net.InterfaceByName(ifaceName)
		if err != nil {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			if ip != nil && !ip.IsLoopback() {
				d.localIPs = append(d.localIPs, ip)
				d.candidates = append(d.candidates, &net.TCPAddr{IP: ip})
			}
		}
	}

	if len(d.candidates) == 0 && cfg.FallbackDialer == nil {
		return nil, ErrNoNICsAvailable
	}

	return d, nil
}

// DialContext races connections across all resolved local interfaces using staggered attempts.
func (d *MultiNICDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if len(d.candidates) == 0 {
		if d.cfg.FallbackDialer != nil {
			return d.cfg.FallbackDialer.DialContext(ctx, network, addr)
		}

		var stdDialer net.Dialer

		return stdDialer.DialContext(ctx, network, addr)
	}

	if len(d.candidates) == 1 {
		dialer := &net.Dialer{
			LocalAddr: d.candidates[0],
			Timeout:   d.cfg.DialTimeout,
		}

		return dialer.DialContext(ctx, network, addr)
	}

	// Race multiple candidates
	return d.raceCandidates(ctx, network, addr)
}

type dialResult struct {
	conn net.Conn
	err  error
}

func (d *MultiNICDialer) raceCandidates(ctx context.Context, network, addr string) (net.Conn, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan dialResult, len(d.candidates))

	var (
		wg        sync.WaitGroup
		completed atomic.Bool
	)

	for i, localAddr := range d.candidates {
		if i > 0 && d.cfg.StaggerDelay > 0 {
			timer := time.NewTimer(d.cfg.StaggerDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case res := <-results:
				timer.Stop()

				if res.err == nil {
					completed.Store(true)

					return res.conn, nil
				}

			case <-timer.C:
			}
		}

		if completed.Load() {
			break
		}

		wg.Add(1)

		go func(targetAddr net.Addr) {
			defer wg.Done()

			dialer := &net.Dialer{
				LocalAddr: targetAddr,
				Timeout:   d.cfg.DialTimeout,
			}

			conn, err := dialer.DialContext(ctx, network, addr)
			if err != nil {
				results <- dialResult{conn: nil, err: err}
				return
			}

			if completed.CompareAndSwap(false, true) {
				results <- dialResult{conn: conn, err: nil}
			} else {
				// Another connection already won the race
				_ = conn.Close()
			}
		}(localAddr)
	}

	// Drain remaining attempts until success or total failure
	var lastErr error

	for range len(d.candidates) {
		select {
		case res := <-results:
			if res.err == nil {
				return res.conn, nil
			}

			lastErr = res.err

		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("aoni/netdial: all multi-nic attempts failed: %w", lastErr)
	}

	return nil, ErrNoNICsAvailable
}
