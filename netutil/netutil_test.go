// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package netutil_test

import (
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/net/ip"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/netutil"
	"github.com/lemon4ksan/aoni/netutil/fragment"
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

func TestParseContentDisposition_RFC6266(t *testing.T) {
	t.Parallel()

	// RFC 6266 Section 5 examples
	// Example 1: Attachment with simple filename
	ex1 := netutil.ParseContentDisposition("Attachment; filename=example.html")
	assert.Equal(t, "attachment", ex1.Type)
	assert.Equal(t, "example.html", ex1.Filename)

	// Example 2: Inline with quoted filename
	ex2 := netutil.ParseContentDisposition(`INLINE; FILENAME="an example.html"`)
	assert.Equal(t, "inline", ex2.Type)
	assert.Equal(t, "an example.html", ex2.Filename)

	// Example 3: RFC 5987 / 8187 filename* precedence over fallback filename
	ex3 := netutil.ParseContentDisposition(`attachment; filename="EURO rates"; filename*=utf-8''%e2%82%ac%20rates.pdf`)
	assert.Equal(t, "attachment", ex3.Type)
	assert.Equal(t, "€ rates.pdf", ex3.Filename)

	// Unknown disposition type fallback to "attachment" (RFC 6266 §4.2)
	ex4 := netutil.ParseContentDisposition(`unknown_type; filename="archive.zip"`)
	assert.Equal(t, "unknown_type", ex4.Type)
	assert.Equal(t, "archive.zip", ex4.Filename)

	// Path traversal stripping in filename (RFC 6266 §4.3)
	ex5 := netutil.ParseContentDisposition(`attachment; filename="../../secrets/passwords.txt"`)
	assert.Equal(t, "passwords.txt", ex5.Filename)
}

func TestFormatContentDisposition_RFC6266(t *testing.T) {
	t.Parallel()

	// ASCII filename
	hdr1 := netutil.FormatContentDisposition("attachment", "document.pdf")
	assert.Equal(t, `attachment; filename="document.pdf"`, hdr1)

	// Non-ASCII filename produces RFC 5987 filename* with ASCII fallback
	hdr2 := netutil.FormatContentDisposition("attachment", "отчёт 2026.pdf")
	assert.Contains(t, hdr2, `filename=`)
	assert.Contains(t, hdr2, `filename*=UTF-8''`)

	// Empty filename
	hdr3 := netutil.FormatContentDisposition("inline", "")
	assert.Equal(t, "inline", hdr3)
}

func TestRFC8187_Encode_Decode(t *testing.T) {
	t.Parallel()

	// RFC 8187 §3.2.3 Example 1: utf-8'en'%C2%A3%20rates
	cs, lang, val, err := netutil.DecodeRFC8187("utf-8'en'%C2%A3%20rates")
	require.NoError(t, err)
	assert.Equal(t, "utf-8", cs)
	assert.Equal(t, "en", lang)
	assert.Equal(t, "£ rates", val)

	// RFC 8187 §3.2.3 Example 2: UTF-8''%c2%a3%20and%20%e2%82%ac%20rates
	cs2, lang2, val2, err2 := netutil.DecodeRFC8187("UTF-8''%c2%a3%20and%20%e2%82%ac%20rates")
	require.NoError(t, err2)
	assert.Equal(t, "utf-8", cs2)
	assert.Empty(t, lang2)
	assert.Equal(t, "£ and € rates", val2)

	// Encode test matching RFC 8187 §3.2.1
	encodedWithLang := netutil.EncodeRFC8187("£ rates", "en")
	assert.Equal(t, "UTF-8'en'%C2%A3%20rates", encodedWithLang)

	encodedNoLang := netutil.EncodeRFC8187("£ and € rates", "")
	assert.Equal(t, "UTF-8''%C2%A3%20and%20%E2%82%AC%20rates", encodedNoLang)

	// DecodeRFC8187Value convenience helper
	assert.Equal(t, "£ and € rates", netutil.DecodeRFC8187Value(encodedNoLang))
}

func TestIsWindowsReservedName(t *testing.T) {
	t.Parallel()

	assert.True(t, netutil.IsWindowsReservedName("CON"))
	assert.True(t, netutil.IsWindowsReservedName("con.txt"))
	assert.True(t, netutil.IsWindowsReservedName("PRN.pdf"))
	assert.True(t, netutil.IsWindowsReservedName("aux"))
	assert.True(t, netutil.IsWindowsReservedName("NUL.tar.gz"))
	assert.True(t, netutil.IsWindowsReservedName("COM1.dat"))
	assert.True(t, netutil.IsWindowsReservedName("LPT9.png"))

	assert.False(t, netutil.IsWindowsReservedName("contract.pdf"))
	assert.False(t, netutil.IsWindowsReservedName("console.log"))
	assert.False(t, netutil.IsWindowsReservedName("constant.go"))
}

func TestISO88591ToUTF8(t *testing.T) {
	t.Parallel()

	// Latin-1 byte \xE9 is 'é', \xF1 is 'ñ', \xA9 is '©'
	latin1 := "\xE9l\xE9phant \xA9"
	utf8Str := netutil.ISO88591ToUTF8(latin1)
	assert.Equal(t, "éléphant ©", utf8Str)
}

func TestIsRFC8187AttrChar(t *testing.T) {
	t.Parallel()

	assert.True(t, netutil.IsRFC8187AttrChar('a'))
	assert.True(t, netutil.IsRFC8187AttrChar('Z'))
	assert.True(t, netutil.IsRFC8187AttrChar('9'))
	assert.True(t, netutil.IsRFC8187AttrChar('!'))
	assert.True(t, netutil.IsRFC8187AttrChar('#'))
	assert.True(t, netutil.IsRFC8187AttrChar('$'))
	assert.True(t, netutil.IsRFC8187AttrChar('&'))
	assert.True(t, netutil.IsRFC8187AttrChar('+'))
	assert.True(t, netutil.IsRFC8187AttrChar('-'))
	assert.True(t, netutil.IsRFC8187AttrChar('.'))
	assert.True(t, netutil.IsRFC8187AttrChar('^'))
	assert.True(t, netutil.IsRFC8187AttrChar('_'))
	assert.True(t, netutil.IsRFC8187AttrChar('`'))
	assert.True(t, netutil.IsRFC8187AttrChar('|'))
	assert.True(t, netutil.IsRFC8187AttrChar('~'))

	// Excluded from attr-char
	assert.False(t, netutil.IsRFC8187AttrChar('*'))
	assert.False(t, netutil.IsRFC8187AttrChar('\''))
	assert.False(t, netutil.IsRFC8187AttrChar('%'))
	assert.False(t, netutil.IsRFC8187AttrChar(' '))
	assert.False(t, netutil.IsRFC8187AttrChar('"'))
	assert.False(t, netutil.IsRFC8187AttrChar('/'))
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
