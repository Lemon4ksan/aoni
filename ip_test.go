// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
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
		wg.Add(1)

		go func() {
			defer wg.Done()

			ip := rot.Next()
			assert.True(t, ip.String() == "10.0.0.1" || ip.String() == "10.0.0.2")
		}()
	}

	wg.Wait()
}
