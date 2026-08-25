// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import (
	"net/netip"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"
)

func TestIPAM_AllocationAndRelease(t *testing.T) {
	t.Parallel()

	v4Prefix := netip.MustParsePrefix("10.8.0.0/28") // 16 addresses
	v6Prefix := netip.MustParsePrefix("fd00:a011::/64")
	ipam := NewIPAM(v4Prefix, v6Prefix)

	// 1. Dynamic allocation
	assigned1, err := ipam.Allocate(nil)
	require.NoError(t, err)
	assert.True(t, v4Prefix.Contains(assigned1.Addr))
	assert.True(t, ipam.IsAllocated(assigned1.Addr))

	assigned2, err := ipam.Allocate(nil)
	require.NoError(t, err)
	assert.NotEqual(t, assigned1.Addr, assigned2.Addr)
	assert.True(t, ipam.IsAllocated(assigned2.Addr))

	// 2. Specific address request
	reqIP := netip.MustParseAddr("10.8.0.7")
	assignedReq, err := ipam.Allocate([]RequestedAddress{
		{
			Addr:      reqIP,
			RequestID: 5,
			IPVersion: 4,
		},
	})
	require.NoError(t, err)
	assert.Equal(t, reqIP, assignedReq.Addr)
	assert.Equal(t, uint64(5), assignedReq.RequestID)

	// 3. Release address
	ipam.Release(assigned1.Addr)
	assert.False(t, ipam.IsAllocated(assigned1.Addr))
}

func TestIPAM_PoolExhaustion(t *testing.T) {
	t.Parallel()

	v4Prefix := netip.MustParsePrefix("192.168.100.0/30") // 4 addresses: .0 network, .1 gw, .2 host, .3 broadcast
	ipam := NewIPAM(v4Prefix, netip.Prefix{})

	// Allocate only available host .2
	assigned1, err := ipam.Allocate(nil)
	require.NoError(t, err)
	assert.Equal(t, "192.168.100.2", assigned1.Addr.String())

	// Next allocation should exhaust pool
	_, err = ipam.Allocate(nil)
	assert.ErrorIs(t, err, ErrIPPoolExhausted)
}
