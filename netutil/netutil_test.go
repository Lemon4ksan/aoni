// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package netutil_test

import (
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/netutil"
	"github.com/lemon4ksan/aoni/netutil/fragment"
	"github.com/lemon4ksan/aoni/netutil/ip"
)

func TestCleanHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		rawHost  string
		wantHost string
	}{
		{
			name:     "bracketed_ipv6_with_zone_id",
			rawHost:  "[fe80::1%eth0]",
			wantHost: "[fe80::1]",
		},
		{
			name:     "unbracketed_ipv6_with_zone_id",
			rawHost:  "fe80::1%eth0",
			wantHost: "fe80::1",
		},
		{
			name:     "idn_punycode_conversion",
			rawHost:  "президент.рф",
			wantHost: "xn--d1abbgf6aiiy.xn--p1ai",
		},
		{
			name:     "standard_ascii_hostname",
			rawHost:  "api.example.com",
			wantHost: "api.example.com",
		},
		{
			name:     "empty_host",
			rawHost:  "",
			wantHost: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.wantHost, netutil.CleanHost(tt.rawHost))
		})
	}
}

func TestCleanHostPort(t *testing.T) {
	t.Parallel()

	host, port := netutil.CleanHostPort("127.0.0.1:8080")
	assert.Equal(t, "127.0.0.1", host)
	assert.Equal(t, "8080", port)

	hostNoPort, portEmpty := netutil.CleanHostPort("example.com")
	assert.Equal(t, "example.com", hostNoPort)
	assert.Empty(t, portEmpty)
}

func TestSanitizeFileName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normal_filename",
			input:    "document.pdf",
			expected: "document.pdf",
		},
		{
			name:     "strip_path_traversal",
			input:    "../../../../etc/passwd",
			expected: "passwd",
		},
		{
			name:     "windows_reserved_con",
			input:    "CON.txt",
			expected: "downloaded_file",
		},
		{
			name:     "windows_reserved_nul",
			input:    "NUL",
			expected: "downloaded_file",
		},
		{
			name:     "windows_reserved_com1",
			input:    "COM1.png",
			expected: "downloaded_file",
		},
		{
			name:     "empty_filename_fallback",
			input:    "",
			expected: "downloaded_file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, netutil.SanitizeFileName(tt.input))
		})
	}
}

func TestExtractSanitizedFilename(t *testing.T) {
	t.Parallel()

	header := `attachment; filename*=UTF-8''foo%20bar%20%D0%BF%D1%80%D0%B8%D0%B2%D0%B5%D1%82.txt`
	got := netutil.ExtractSanitizedFilename(header)
	assert.Equal(t, "foo bar привет.txt", got)

	emptyHeader := netutil.ExtractSanitizedFilename("")
	assert.Equal(t, "downloaded_file", emptyHeader)
}

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

func TestFragmentedConn(t *testing.T) {
	t.Parallel()

	c1, c2 := net.Pipe()
	t.Cleanup(func() {
		_ = c1.Close()
		_ = c2.Close()
	})

	cfg := &fragment.Config{
		ChunkSize:  5,
		LimitBytes: 100,
	}

	fragConn := fragment.NewFragmentedConn(c1, cfg)

	var received []byte

	done := make(chan struct{})

	go func() {
		defer close(done)

		buf := make([]byte, 1024)
		for {
			n, err := c2.Read(buf)
			if n > 0 {
				received = append(received, buf[:n]...)
			}

			if err != nil || len(received) >= 15 {
				return
			}
		}
	}()

	payload := []byte("123456789012345")
	n, err := fragConn.Write(payload)
	require.NoError(t, err)
	assert.Equal(t, 15, n)

	select {
	case <-done:
		assert.Equal(t, payload, received)
	case <-time.After(1 * time.Second):
		t.Fatal("read timeout")
	}
}
