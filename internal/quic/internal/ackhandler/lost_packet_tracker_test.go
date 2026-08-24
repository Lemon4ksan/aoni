// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ackhandler

import (
	"maps"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/internal/quic/internal/monotime"
	"github.com/lemon4ksan/aoni/internal/quic/internal/protocol"
)

func TestLostPacketTracker(t *testing.T) {
	lt := newLostPacketTracker(4)

	start := monotime.Now()
	lt.Add(1, start)
	lt.Add(5, start.Add(time.Second))
	lt.Add(8, start.Add(2*time.Second))
	require.Equal(t, map[protocol.PacketNumber]monotime.Time{
		1: start,
		5: start.Add(time.Second),
		8: start.Add(2 * time.Second),
	}, maps.Collect(lt.All()))

	// Lose 2 more packets. The first one should be removed.
	lt.Add(10, start.Add(3*time.Second))
	lt.Add(11, start.Add(4*time.Second))
	require.Equal(t, map[protocol.PacketNumber]monotime.Time{
		5:  start.Add(time.Second),
		8:  start.Add(2 * time.Second),
		10: start.Add(3 * time.Second),
		11: start.Add(4 * time.Second),
	}, maps.Collect(lt.All()))

	lt.Delete(5)
	lt.Delete(10)
	require.Equal(t, map[protocol.PacketNumber]monotime.Time{
		8:  start.Add(2 * time.Second),
		11: start.Add(4 * time.Second),
	}, maps.Collect(lt.All()))
}

func TestLostPacketTrackerDeleteBefore(t *testing.T) {
	lt := newLostPacketTracker(4)

	trackedPackets := func(lt *lostPacketTracker) []protocol.PacketNumber {
		var pns []protocol.PacketNumber
		for pn := range lt.All() {
			pns = append(pns, pn)
		}

		return pns
	}

	start := monotime.Now()
	lt.Add(1, start)
	lt.Add(5, start.Add(time.Second))
	lt.Add(8, start.Add(2*time.Second))
	lt.Add(10, start.Add(3*time.Second))

	require.Equal(t, []protocol.PacketNumber{1, 5, 8, 10}, trackedPackets(lt))

	lt.DeleteBefore(start) // this should be a no-op
	require.Equal(t, []protocol.PacketNumber{1, 5, 8, 10}, trackedPackets(lt))

	lt.DeleteBefore(start.Add(2 * time.Second))
	require.Equal(t, []protocol.PacketNumber{8, 10}, trackedPackets(lt))

	lt.DeleteBefore(start.Add(time.Second * 5 / 2))
	require.Equal(t, []protocol.PacketNumber{10}, trackedPackets(lt))

	lt.DeleteBefore(start.Add(time.Hour))
	require.Empty(t, trackedPackets(lt))
}
