// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ip

import (
	"net"
	"net/netip"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSourceIPRotator_Validation(t *testing.T) {
	t.Parallel()

	t.Run("valid_ips", func(t *testing.T) {
		t.Parallel()

		rot, err := NewSourceIPRotator([]string{"192.168.1.1", "2001:db8::1"})
		require.NoError(t, err)
		require.NotNil(t, rot)

		assert.Equal(t, 2, rot.Size())
		ips := rot.IPs()
		assert.Equal(t, "192.168.1.1", ips[0].String())
		assert.Equal(t, "2001:db8::1", ips[1].String())
	})

	t.Run("invalid_ip", func(t *testing.T) {
		t.Parallel()

		_, err := NewSourceIPRotator([]string{"invalid-ip"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid source IP")
	})

	t.Run("empty_ips", func(t *testing.T) {
		t.Parallel()

		_, err := NewSourceIPRotator([]string{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "pool cannot be empty")
	})
}

func TestSourceIPRotator_Next_Rotation(t *testing.T) {
	t.Parallel()

	rot, err := NewSourceIPRotator([]string{"10.0.0.1", "10.0.0.2", "10.0.0.3"})
	require.NoError(t, err)

	// Confirm strict sequential rotation
	assert.Equal(t, "10.0.0.1", rot.Next().String())
	assert.Equal(t, "10.0.0.2", rot.Next().String())
	assert.Equal(t, "10.0.0.3", rot.Next().String())
	assert.Equal(t, "10.0.0.1", rot.Next().String()) // Wraps around
}

func TestSourceIPRotator_UpdatePool(t *testing.T) {
	t.Parallel()

	rot, err := NewSourceIPRotator([]string{"1.1.1.1"})
	require.NoError(t, err)

	// Try update with valid IPs
	err = rot.UpdatePool([]string{"8.8.8.8", "8.8.4.4"})
	require.NoError(t, err)
	assert.Equal(t, 2, rot.Size())
	assert.Equal(t, "8.8.8.8", rot.Next().String())

	// Try update with invalid
	err = rot.UpdatePool([]string{"invalid"})
	assert.Error(t, err)

	// Try update with empty
	err = rot.UpdatePool([]string{})
	assert.Error(t, err)
}

func TestSourceIPRotator_NextForFamily(t *testing.T) {
	t.Parallel()

	t.Run("family_matching", func(t *testing.T) {
		t.Parallel()
		// Mixed pool
		rot, err := NewSourceIPRotator([]string{"192.168.1.1", "2001:db8::1", "192.168.1.2"})
		require.NoError(t, err)

		// Request IPv4
		ip4_1 := rot.NextForFamily(true)
		require.NotNil(t, ip4_1)
		assert.Equal(t, "192.168.1.1", ip4_1.String())

		// Request IPv6
		ip6 := rot.NextForFamily(false)
		require.NotNil(t, ip6)
		assert.Equal(t, "2001:db8::1", ip6.String())

		// Request IPv4 again
		ip4_2 := rot.NextForFamily(true)
		require.NotNil(t, ip4_2)
		assert.Equal(t, "192.168.1.2", ip4_2.String())
	})

	t.Run("no_matching_family", func(t *testing.T) {
		t.Parallel()
		// IPv4-only pool
		rot, err := NewSourceIPRotator([]string{"192.168.1.1"})
		require.NoError(t, err)

		// Request IPv6 (none available)
		ip6 := rot.NextForFamily(false)
		assert.Nil(t, ip6) // Should fall back smoothly to nil (default routing)
	})
}

func TestSourceIPRotator_Concurrency(t *testing.T) {
	t.Parallel()

	rot, err := NewSourceIPRotator([]string{"10.0.0.1", "10.0.0.2"})
	require.NoError(t, err)

	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			ip := rot.Next()
			assert.True(t, ip.String() == "10.0.0.1" || ip.String() == "10.0.0.2")
		})
	}

	wg.Wait()
}

func TestIsPrivateIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ip        string
		isPrivate bool
	}{
		{ip: "127.0.0.1", isPrivate: true},
		{ip: "10.0.1.5", isPrivate: true},
		{ip: "172.16.0.1", isPrivate: true},
		{ip: "192.168.1.1", isPrivate: true},
		{ip: "100.64.0.1", isPrivate: true}, // CGNAT (RFC 6598)
		{ip: "8.8.8.8", isPrivate: false},   // Public IPv4
		{ip: "1.1.1.1", isPrivate: false},   // Public IPv4
		{ip: "::1", isPrivate: true},        // IPv6 Loopback
		{ip: "fc00::1", isPrivate: true},    // IPv6 Unique Local Address
		{ip: "2001:db8::1", isPrivate: false},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			t.Parallel()

			parsed := net.ParseIP(tt.ip)
			require.NotNil(t, parsed)
			assert.Equal(t, tt.isPrivate, IsPrivateIP(parsed))
		})
	}
}

func TestNetutil_IP(t *testing.T) {
	t.Parallel()

	t.Run("discover_interface_ips", func(t *testing.T) {
		t.Parallel()

		ips, err := DiscoverInterfaceIPs()
		if err == nil {
			assert.NotEmpty(t, ips)
		}
	})

	t.Run("ipv6_subnet_rotator", func(t *testing.T) {
		t.Parallel()

		_, err := NewIPv6SubnetRotator("invalid_cidr")
		assert.ErrorIs(t, err, ErrInvalidCIDR)

		rotator, err := NewIPv6SubnetRotator("2001:db8::/64")
		require.NoError(t, err)
		require.NotNil(t, rotator)

		generatedIP := rotator.Next()
		require.NotNil(t, generatedIP)

		prefix, _ := netip.ParsePrefix("2001:db8::/64")
		parsedAddr, _ := netip.ParseAddr(generatedIP.String())
		assert.True(t, prefix.Contains(parsedAddr))
	})
}
