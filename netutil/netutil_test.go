// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package netutil_test

import (
	"crypto/tls"
	"net"
	"net/netip"
	"testing"

	"github.com/lemon4ksan/foundation/net/ip"
	"github.com/lemon4ksan/foundation/testing/assert"
	"github.com/lemon4ksan/foundation/testing/require"

	"github.com/lemon4ksan/aoni/netutil"
)







func TestWriteTrackingConn(t *testing.T) {
	t.Parallel()

	c1, c2 := net.Pipe()
	t.Cleanup(func() {
		_ = c1.Close()
		_ = c2.Close()
	})

	tracker := netutil.NewWriteTrackingConn(c1)
	require.NotNil(t, tracker)
	assert.Equal(t, int64(0), tracker.BytesWritten())

	go func() {
		buf := make([]byte, 1024)
		_, _ = c2.Read(buf)
	}()

	payload := []byte("hello tracking connection")
	n, err := tracker.Write(payload)
	require.NoError(t, err)
	assert.Equal(t, len(payload), n)
	assert.Equal(t, int64(len(payload)), tracker.BytesWritten())

	tracker.ResetBytesWritten()
	assert.Equal(t, int64(0), tracker.BytesWritten())
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
			assert.Equal(t, tt.isPrivate, ip.IsPrivateIP(parsed))
		})
	}
}

func TestSourceIPRotator(t *testing.T) {
	t.Parallel()

	addrs := []string{"192.168.1.1", "192.168.1.2", "2001:db8::1"}
	rotator, err := ip.NewSourceIPRotator(addrs)
	require.NoError(t, err)
	assert.Equal(t, 3, rotator.Size())

	// Test Round-Robin
	ip1 := rotator.Next()
	ip2 := rotator.Next()

	assert.Equal(t, "192.168.1.1", ip1.String())
	assert.Equal(t, "192.168.1.2", ip2.String())

	// Test NextForFamily
	v6 := rotator.NextForFamily(false)
	require.NotNil(t, v6)
	assert.Equal(t, "2001:db8::1", v6.String())

	// Test UpdatePool
	err = rotator.UpdatePool([]string{"10.0.0.1"})
	require.NoError(t, err)
	assert.Equal(t, 1, rotator.Size())
	assert.Equal(t, "10.0.0.1", rotator.Next().String())
}

func TestIPv6SubnetRotator(t *testing.T) {
	t.Parallel()

	rotator, err := ip.NewIPv6SubnetRotator("2001:db8::/64")
	require.NoError(t, err)

	ip1 := rotator.Next()
	require.NotNil(t, ip1)

	prefix, _ := netip.ParsePrefix("2001:db8::/64")
	parsedIP, _ := netip.ParseAddr(ip1.String())
	assert.True(t, prefix.Contains(parsedIP))
}

type mockSessionCacheProvider struct {
	cache tls.ClientSessionCache
}

func (m *mockSessionCacheProvider) StdTLSSessionCache() tls.ClientSessionCache {
	return m.cache
}
