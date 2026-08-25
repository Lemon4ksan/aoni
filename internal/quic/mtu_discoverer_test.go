// Copyright (c) 2016 the quic-go authors. All rights reserved.
// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package quic

import (
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/internal/quic/internal/monotime"
	"github.com/lemon4ksan/aoni/internal/quic/internal/protocol"
	"github.com/lemon4ksan/aoni/internal/quic/internal/utils"
)

func TestMTUDiscovererTiming(t *testing.T) {
	const rtt = 100 * time.Millisecond

	rttStats := utils.NewRTTStats()
	rttStats.UpdateRTT(rtt, 0)
	d := newMTUDiscoverer(rttStats, 1000, 2000)

	now := monotime.Now()
	require.False(t, d.ShouldSendProbe(now))
	d.Start(now)
	require.False(t, d.ShouldSendProbe(now))
	require.False(t, d.ShouldSendProbe(now.Add(rtt*9/2)))
	now = now.Add(5 * rtt)
	require.True(t, d.ShouldSendProbe(now))

	// only a single outstanding probe packet is permitted
	ping, _ := d.GetPing(now)
	require.False(t, d.ShouldSendProbe(now))
	now = now.Add(5 * rtt)
	require.False(t, d.ShouldSendProbe(now))
	ping.Handler.OnLost(ping.Frame)
	require.True(t, d.ShouldSendProbe(now))
}

func TestMTUDiscovererAckAndLoss(t *testing.T) {
	const rtt = 200 * time.Millisecond

	rttStats := utils.NewRTTStats()
	rttStats.UpdateRTT(rtt, 0)
	d := newMTUDiscoverer(rttStats, 1000, 2000)
	now := monotime.Now()
	ping, size := d.GetPing(now)
	require.Equal(t, protocol.ByteCount(1500), size)
	// the MTU is reduced if the frame is lost
	ping.Handler.OnLost(ping.Frame)
	require.Equal(t, protocol.ByteCount(1000), d.CurrentSize()) // no change to the MTU yet

	now = now.Add(5 * rtt)
	require.True(t, d.ShouldSendProbe(now))
	ping, size = d.GetPing(now)
	require.Equal(t, protocol.ByteCount(1250), size)
	ping.Handler.OnAcked(ping.Frame)
	require.Equal(t, protocol.ByteCount(1250), d.CurrentSize()) // the MTU is increased

	// Even though the 1500 byte MTU probe packet was lost, we try again with a higher MTU.
	// This protects against regular (non-MTU-related) packet loss.
	now = now.Add(5 * rtt)
	require.True(t, d.ShouldSendProbe(now))
	ping, size = d.GetPing(now)
	require.Greater(t, size, protocol.ByteCount(1500))
	ping.Handler.OnAcked(ping.Frame)
	require.Equal(t, size, d.CurrentSize())

	// We continue probing until the MTU is close to the maximum.
	var steps int

	oldSize := size

	now = now.Add(5 * rtt)
	for d.ShouldSendProbe(now) {
		ping, size = d.GetPing(now)
		require.Greater(t, size, oldSize)
		oldSize = size

		ping.Handler.OnAcked(ping.Frame)

		steps++
		require.Less(t, steps, 10)

		now = now.Add(5 * rtt)
	}

	require.Less(t, 2000-maxMTUDiff, size)
}

func TestMTUDiscovererMTUDiscovery(t *testing.T) {
	for i := range 5 {
		t.Run(fmt.Sprintf("test %d", i), func(t *testing.T) {
			testMTUDiscovererMTUDiscovery(t)
		})
	}
}

func testMTUDiscovererMTUDiscovery(t *testing.T) {
	const (
		rtt                         = 100 * time.Millisecond
		startMTU protocol.ByteCount = 1000
	)

	rttStats := utils.NewRTTStats()
	rttStats.UpdateRTT(rtt, 0)

	maxMTU := protocol.ByteCount(rand.IntN(int(3000-startMTU))) + startMTU + 1

	d := newMTUDiscoverer(rttStats, startMTU, maxMTU)
	now := monotime.Now()
	d.Start(now)

	realMTU := protocol.ByteCount(rand.IntN(int(maxMTU-startMTU))) + startMTU
	t.Logf("MTU: %d, max: %d", realMTU, maxMTU)

	now = now.Add(mtuProbeDelay * rtt)

	var probes []protocol.ByteCount
	for d.ShouldSendProbe(now) {
		require.Less(t, len(probes), 25, fmt.Sprintf("too many iterations: %v", probes))

		ping, size := d.GetPing(now)

		probes = append(probes, size)
		if size <= realMTU {
			ping.Handler.OnAcked(ping.Frame)
		} else {
			ping.Handler.OnLost(ping.Frame)
		}

		now = now.Add(mtuProbeDelay * rtt)
	}

	currentMTU := d.CurrentSize()
	diff := realMTU - currentMTU
	require.GreaterOrEqual(t, diff, protocol.ByteCount(0))

	t.Logf("MTU discovered: %d (diff: %d)", currentMTU, diff)
	t.Logf("probes sent (%d): %v", len(probes), probes)
	require.LessOrEqual(t, diff, maxMTUDiff)
}

func TestMTUDiscovererWithRandomLoss(t *testing.T) {
	for i := range 5 {
		t.Run(fmt.Sprintf("test %d", i), func(t *testing.T) {
			testMTUDiscovererWithRandomLoss(t)
		})
	}
}

func testMTUDiscovererWithRandomLoss(t *testing.T) {
	const (
		rtt                              = 100 * time.Millisecond
		startMTU      protocol.ByteCount = 1000
		maxRandomLoss                    = maxLostMTUProbes - 1
	)

	rttStats := utils.NewRTTStats()
	rttStats.SetInitialRTT(rtt)
	require.Equal(t, rtt, rttStats.SmoothedRTT())

	maxMTU := protocol.ByteCount(rand.IntN(int(3000-startMTU))) + startMTU + 1

	d := newMTUDiscoverer(rttStats, startMTU, maxMTU)
	d.Start(monotime.Now())
	now := monotime.Now()
	realMTU := protocol.ByteCount(rand.IntN(int(maxMTU-startMTU))) + startMTU
	t.Logf("MTU: %d, max: %d", realMTU, maxMTU)

	now = now.Add(mtuProbeDelay * rtt)

	var probes, randomLosses []protocol.ByteCount

	for d.ShouldSendProbe(now) {
		require.Less(t, len(probes), 32, fmt.Sprintf("too many iterations: %v", probes))

		ping, size := d.GetPing(now)
		probes = append(probes, size)
		packetFits := size <= realMTU

		var acked bool
		if packetFits {
			randomLoss := rand.IntN(maxLostMTUProbes) == 0 && len(randomLosses) < maxRandomLoss
			if randomLoss {
				randomLosses = append(randomLosses, size)
			} else {
				ping.Handler.OnAcked(ping.Frame)

				acked = true
			}
		}

		if !acked {
			ping.Handler.OnLost(ping.Frame)
		}

		now = now.Add(mtuProbeDelay * rtt)
	}

	currentMTU := d.CurrentSize()
	diff := realMTU - currentMTU
	require.GreaterOrEqual(t, diff, protocol.ByteCount(0))

	t.Logf("MTU discovered with random losses %v: %d (diff: %d)", randomLosses, currentMTU, diff)
	t.Logf("probes sent (%d): %v", len(probes), probes)
	require.LessOrEqual(t, diff, maxMTUDiff)
}

func TestMTUDiscovererReset(t *testing.T) {
	t.Run("probe on old path acknowledged", func(t *testing.T) {
		testMTUDiscovererReset(t, true)
	})
	t.Run("probe on old path lost", func(t *testing.T) {
		testMTUDiscovererReset(t, false)
	})
}

func testMTUDiscovererReset(t *testing.T, ackLastProbe bool) {
	const (
		startMTU protocol.ByteCount = 1000
		maxMTU                      = 1400
		rtt                         = 100 * time.Millisecond
	)

	rttStats := utils.NewRTTStats()
	rttStats.SetInitialRTT(rtt)

	now := monotime.Now()
	d := newMTUDiscoverer(rttStats, startMTU, maxMTU)
	d.Start(now)

	ping, _ := d.GetPing(now.Add(5 * rtt))
	ping.Handler.OnAcked(ping.Frame)
	require.Greater(t, d.CurrentSize(), startMTU)

	now = now.Add(5 * rtt)

	// send another probe packet, but neither acknowledge nor lose it before resetting
	ping, _ = d.GetPing(now.Add(5 * rtt))
	now = now.Add(2 * rtt) // advance the timer by an arbitrary amount

	const (
		newStartMTU protocol.ByteCount = 900
		newMaxMTU   protocol.ByteCount = 1500
	)

	d.Reset(now, newStartMTU, newMaxMTU)
	require.Equal(t, d.CurrentSize(), newStartMTU)

	// Now acknowledge / lose the probe packet.
	// This should be ignored, since it's on the old path.
	if ackLastProbe {
		ping.Handler.OnAcked(ping.Frame)
	} else {
		ping.Handler.OnLost(ping.Frame)
	}

	// the MTU should not have changed
	require.Equal(t, d.CurrentSize(), newStartMTU)
	// the next probe should be sent after 5 RTTs
	require.False(t, d.ShouldSendProbe(now.Add(5*rtt).Add(-time.Microsecond)))
	require.True(t, d.ShouldSendProbe(now.Add(5*rtt)))
}
