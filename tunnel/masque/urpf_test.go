// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import (
	"net/netip"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
)

func TestIsMartianAddr(t *testing.T) {
	t.Parallel()

	martians := []string{
		"0.0.0.0",
		"0.1.2.3",
		"127.0.0.1",
		"127.0.0.53",
		"224.0.0.1",
		"239.255.255.250",
		"::",
		"::1",
		"ff02::1",
	}

	for _, ipStr := range martians {
		addr := netip.MustParseAddr(ipStr)
		assert.Truef(t, IsMartianAddr(addr), "addr %s should be martian", ipStr)
	}

	routable := []string{
		"192.168.1.1",
		"10.0.0.1",
		"172.16.0.1",
		"8.8.8.8",
		"1.1.1.1",
		"2001:db8::1",
	}

	for _, ipStr := range routable {
		addr := netip.MustParseAddr(ipStr)
		assert.Falsef(t, IsMartianAddr(addr), "addr %s should NOT be martian", ipStr)
	}

	assert.True(t, IsMartianAddr(netip.Addr{}), "invalid zero netip.Addr should be martian")
}

func TestValidateIngressSourceAddress(t *testing.T) {
	t.Parallel()

	allowedPrefixes := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/16"),
		netip.MustParsePrefix("2001:db8::/32"),
	}

	t.Run("valid address within allowed prefix", func(t *testing.T) {
		t.Parallel()

		err := ValidateIngressSourceAddress(netip.MustParseAddr("10.0.1.50"), allowedPrefixes)
		assert.NoError(t, err)

		err6 := ValidateIngressSourceAddress(netip.MustParseAddr("2001:db8:1::50"), allowedPrefixes)
		assert.NoError(t, err6)
	})

	t.Run("spoofed address outside allowed prefix", func(t *testing.T) {
		t.Parallel()

		err := ValidateIngressSourceAddress(netip.MustParseAddr("192.168.1.1"), allowedPrefixes)
		assert.ErrorIs(t, err, ErrSpoofedSourceAddress)

		err6 := ValidateIngressSourceAddress(netip.MustParseAddr("2001:dc8::1"), allowedPrefixes)
		assert.ErrorIs(t, err6, ErrSpoofedSourceAddress)
	})

	t.Run("martian address", func(t *testing.T) {
		t.Parallel()

		err := ValidateIngressSourceAddress(netip.MustParseAddr("127.0.0.1"), allowedPrefixes)
		assert.ErrorIs(t, err, ErrMartianAddress)

		err0 := ValidateIngressSourceAddress(netip.MustParseAddr("0.0.0.0"), allowedPrefixes)
		assert.ErrorIs(t, err0, ErrMartianAddress)
	})

	t.Run("empty allowed prefixes or invalid addr bypasses validation", func(t *testing.T) {
		t.Parallel()

		assert.NoError(t, ValidateIngressSourceAddress(netip.MustParseAddr("192.168.1.1"), nil))
		assert.NoError(t, ValidateIngressSourceAddress(netip.Addr{}, allowedPrefixes))
	})
}
