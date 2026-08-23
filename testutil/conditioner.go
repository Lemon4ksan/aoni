// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testutil

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"sync"
	"syscall"
	"time"
)

// ErrNetworkOffline is returned when attempting to execute requests through an offline conditioner.
var ErrNetworkOffline = errors.New("aoni/testutil: network link is offline")

// ErrSimulatedPacketLoss is returned when a request is dropped by the simulated packet loss rate.
var ErrSimulatedPacketLoss = errors.New("aoni/testutil: simulated packet loss")

// ErrSimulatedConnectionReset is returned when a connection is dropped via simulated ECONNRESET.
var ErrSimulatedConnectionReset = syscall.ECONNRESET

// NetworkCondition defines simulated link parameters for network degradation testing.
type NetworkCondition struct {
	// Latency specifies the base simulated latency added to each transaction.
	Latency time.Duration

	// Jitter specifies the maximum random duration variance applied to base latency (+/- Jitter).
	Jitter time.Duration

	// PacketLoss defines the probability (0.0 to 1.0) of a request dropping permanently.
	PacketLoss float64

	// ResetRate defines the probability (0.0 to 1.0) of a connection dropping via ECONNRESET.
	ResetRate float64

	// BandwidthRate specifies the maximum download throughput in bytes per second (0 = unlimited).
	BandwidthRate int64

	// Offline simulates a complete network interface blackout.
	Offline bool
}

// ProfileFlaky3G returns a simulation profile modeling unstable cellular 3G network conditions.
func ProfileFlaky3G() NetworkCondition {
	return NetworkCondition{
		Latency:       100 * time.Millisecond,
		Jitter:        25 * time.Millisecond,
		PacketLoss:    0.03,
		ResetRate:     0.01,
		BandwidthRate: 384 * 1024, // 384 KB/s
	}
}

// ProfileLossyWiFi returns a simulation profile modeling a crowded or degraded Wi-Fi link.
func ProfileLossyWiFi() NetworkCondition {
	return NetworkCondition{
		Latency:       25 * time.Millisecond,
		Jitter:        15 * time.Millisecond,
		PacketLoss:    0.08,
		ResetRate:     0.02,
		BandwidthRate: 5 * 1024 * 1024, // 5 MB/s
	}
}

// ProfileSlowSatellite returns a simulation profile modeling high-latency geostationary satellite links.
func ProfileSlowSatellite() NetworkCondition {
	return NetworkCondition{
		Latency:       600 * time.Millisecond,
		Jitter:        80 * time.Millisecond,
		PacketLoss:    0.02,
		BandwidthRate: 1024 * 1024, // 1 MB/s
	}
}

// ProfileBlackout returns a simulation profile where the network is completely disconnected.
func ProfileBlackout() NetworkCondition {
	return NetworkCondition{
		Offline: true,
	}
}

// Conditioner decorates an [http.RoundTripper] to simulate real-world degraded network links.
type Conditioner struct {
	inner     http.RoundTripper
	mu        sync.RWMutex
	condition NetworkCondition
}

// NewNetworkConditioner wraps an existing transport with simulated network link conditioning.
func NewNetworkConditioner(cond NetworkCondition, inner ...http.RoundTripper) *Conditioner {
	var rt http.RoundTripper
	if len(inner) > 0 && inner[0] != nil {
		rt = inner[0]
	} else {
		rt = http.DefaultTransport
	}

	return &Conditioner{
		inner:     rt,
		condition: cond,
	}
}

// SetCondition dynamically updates the active network simulation profile during tests.
func (c *Conditioner) SetCondition(cond NetworkCondition) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.condition = cond
}

// Condition returns the current active simulation condition snapshot.
func (c *Conditioner) Condition() NetworkCondition {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.condition
}

// RoundTrip simulates latency, jitter, packet loss, bandwidth throttling, and connection drops.
func (c *Conditioner) RoundTrip(req *http.Request) (*http.Response, error) {
	cond := c.Condition()

	if cond.Offline {
		return nil, ErrNetworkOffline
	}

	ctx := req.Context()

	if cond.PacketLoss > 0 && rand.Float64() < cond.PacketLoss {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
			return nil, ErrSimulatedPacketLoss
		}
	}

	if cond.ResetRate > 0 && rand.Float64() < cond.ResetRate {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Millisecond):
			return nil, ErrSimulatedConnectionReset
		}
	}

	totalDelay := cond.Latency
	if cond.Jitter > 0 {
		jitterRange := int64(cond.Jitter * 2)
		delta := time.Duration(rand.Int64N(jitterRange)) - cond.Jitter

		totalDelay += delta
		if totalDelay < 0 {
			totalDelay = 0
		}
	}

	if totalDelay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(totalDelay):
		}
	}

	resp, err := c.inner.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	if cond.BandwidthRate > 0 && resp != nil && resp.Body != nil {
		resp.Body = &throttledReadCloser{
			ctx:           ctx,
			reader:        resp.Body,
			bandwidthRate: cond.BandwidthRate,
		}
	}

	return resp, nil
}

type throttledReadCloser struct {
	ctx           context.Context
	reader        io.ReadCloser
	bandwidthRate int64 // bytes per second
}

func (t *throttledReadCloser) Read(p []byte) (int, error) {
	if err := t.ctx.Err(); err != nil {
		return 0, err
	}

	n, err := t.reader.Read(p)
	if n > 0 && t.bandwidthRate > 0 {
		sleepDuration := time.Duration(float64(n) / float64(t.bandwidthRate) * float64(time.Second))
		if sleepDuration > 0 {
			select {
			case <-t.ctx.Done():
				return n, t.ctx.Err()
			case <-time.After(sleepDuration):
			}
		}
	}

	return n, err
}

func (t *throttledReadCloser) Close() error {
	return t.reader.Close()
}
