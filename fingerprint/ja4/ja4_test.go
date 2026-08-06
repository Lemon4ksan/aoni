// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ja4

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsGREASE(t *testing.T) {
	t.Parallel()

	grease := []uint16{
		0x0a0a, 0x1a1a, 0x2a2a, 0x3a3a, 0x4a4a, 0x5a5a, 0x6a6a, 0x7a7a,
		0x8a8a, 0x9a9a, 0xaaaa, 0xbaba, 0xcaca, 0xdada, 0xeaea, 0xfafa,
	}

	for _, v := range grease {
		assert.True(t, IsGREASE(v), "0x%04x should be GREASE", v)
	}

	notGREASE := []uint16{0x0000, 0x0001, 0x000d, 0x0010, 0x1301, 0xc02f, 0x0303, 0x0a0b}
	for _, v := range notGREASE {
		assert.False(t, IsGREASE(v), "0x%04x should not be GREASE", v)
	}
}

func TestFormatHex4(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    uint16
		expected string
	}{
		{0x0000, "0000"},
		{0x000f, "000f"},
		{0x002f, "002f"},
		{0x1301, "1301"},
		{0xc02f, "c02f"},
		{0xffff, "ffff"},
	}

	for _, tt := range tests {
		var bb bytes.Buffer
		writeHex4(&bb, tt.input)
		assert.Equal(t, tt.expected, bb.String())
	}
}

func TestWritePaddedTwoDigits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    int
		expected string
	}{
		{0, "00"},
		{5, "05"},
		{9, "09"},
		{10, "10"},
		{42, "42"},
		{99, "99"},
	}

	for _, tt := range tests {
		var bb bytes.Buffer
		writePaddedTwoDigits(&bb, tt.input)
		assert.Equal(t, tt.expected, bb.String())
	}
}

func TestComputeJA4_KnownVector(t *testing.T) {
	t.Parallel()

	// Example from JA4 spec: Chrome TLS 1.3 with domain SNI, 15 ciphers, 16 extensions, h2 ALPN
	ciphers := []uint16{
		0x002f, 0x0035, 0x009c, 0x009d, 0x1301, 0x1302, 0x1303,
		0xc013, 0xc014, 0xc02b, 0xc02c, 0xc02f, 0xc030, 0xcca8, 0xcca9,
	}
	extensions := []uint16{
		0x0000, 0x0005, 0x000a, 0x000b, 0x000d, 0x0010, 0x0012,
		0x0015, 0x0017, 0x001b, 0x0023, 0x002b, 0x002d, 0x0033, 0x4469, 0xff01,
	}
	supportedVersions := []uint16{0x0304} // TLS 1.3
	alpn := []string{"h2"}
	sigAlgos := []uint16{0x0403, 0x0804, 0x0401, 0x0503, 0x0805, 0x0501, 0x0806, 0x0601}

	result := ComputeJA4(ciphers, extensions, supportedVersions, true, alpn, sigAlgos)

	// Verify structure: protocol + version + sni + cipherCount + extCount + alpn _ hash _ hash
	assert.Regexp(t, `^t13d1516h2_[a-f0-9]{12}_[a-f0-9]{12}$`, result)
}

func TestComputeJA4_NoALPN(t *testing.T) {
	t.Parallel()

	ciphers := []uint16{0x1301, 0x1302}
	extensions := []uint16{0x0000, 0x000d}
	supportedVersions := []uint16{0x0303}
	sigAlgos := []uint16{0x0403}

	result := ComputeJA4(ciphers, extensions, supportedVersions, true, nil, sigAlgos)
	assert.Regexp(t, `^t12d020200_`, result)
}

func TestComputeJA4_NoSNI(t *testing.T) {
	t.Parallel()

	ciphers := []uint16{0x1301}
	extensions := []uint16{0x0010}
	supportedVersions := []uint16{0x0304}

	result := ComputeJA4(ciphers, extensions, supportedVersions, false, []string{"h2"}, nil)
	assert.Contains(t, result, "t13i")
}

func TestComputeJA4_EmptyCiphers(t *testing.T) {
	t.Parallel()

	result := ComputeJA4(nil, nil, nil, false, nil, nil)
	assert.Equal(t, "t00i000000_000000000000_000000000000", result)
}

func TestComputeJA4_GreaseFiltering(t *testing.T) {
	t.Parallel()

	ciphers := []uint16{0x0a0a, 0x1301, 0xfafa, 0x1302}
	extensions := []uint16{0x0a0a, 0x0000, 0xfafa}

	result := ComputeJA4(ciphers, extensions, nil, true, nil, nil)
	assert.Contains(t, result, "0201")
}

func TestComputeJA4_CappedAt99(t *testing.T) {
	t.Parallel()

	ciphers := make([]uint16, 110)
	for i := range ciphers {
		ciphers[i] = 0x1301
	}

	extensions := make([]uint16, 120)
	for i := range extensions {
		extensions[i] = 0x0005
	}

	result := ComputeJA4(ciphers, extensions, nil, true, nil, nil)
	assert.Contains(t, result, "t00d999900_")
}

func TestComputeJA4H_BasicGET(t *testing.T) {
	t.Parallel()

	headers := []string{"Host", "User-Agent", "Accept"}
	result := ComputeJA4H("GET", "HTTP/1.1", headers, false, false, "en-US", nil, nil)

	assert.Regexp(t, `^ge11nn03enus_[a-f0-9]{12}_000000000000_000000000000$`, result)
}

func TestComputeJA4H_PostWithCookies(t *testing.T) {
	t.Parallel()

	headers := []string{"Host", "Content-Type"}
	cookieNames := []string{"session", "token"}
	cookieValues := []string{"abc123", "xyz789"}

	result := ComputeJA4H("POST", "HTTP/1.1", headers, true, true, "en", cookieNames, cookieValues)

	assert.Regexp(t, `^po11cr02[a-z0-9]{4}_[a-f0-9]{12}_[a-f0-9]{12}_[a-f0-9]{12}$`, result)
}

func TestComputeJA4H_NoHeaders(t *testing.T) {
	t.Parallel()

	result := ComputeJA4H("GET", "HTTP/1.0", nil, false, false, "", nil, nil)
	assert.Regexp(t, `^ge10nn000000_[a-f0-9]{12}_000000000000_000000000000$`, result)
}

func TestComputeJA4H_HTTP2PseudoHeaders(t *testing.T) {
	t.Parallel()

	headers := []string{":method", ":path", ":authority", "Accept", "User-Agent"}
	result := ComputeJA4H("GET", "HTTP/2", headers, false, false, "", nil, nil)

	assert.Contains(t, result, "ge20nn05")
}

func TestComputeJA4H_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("short method", func(t *testing.T) {
		t.Parallel()

		result := ComputeJA4H("G", "HTTP/1.1", nil, false, false, "", nil, nil)
		assert.Regexp(t, `^0011nn`, result)
	})

	t.Run("unmapped protocol", func(t *testing.T) {
		t.Parallel()

		result := ComputeJA4H("GET", "HTTP/1.5", nil, false, false, "", nil, nil)
		assert.Regexp(t, `^ge00nn`, result)
	})
}

func TestComputeVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		versions []uint16
		expected string
	}{
		{[]uint16{0x0304}, "13"},
		{[]uint16{0x0303}, "12"},
		{[]uint16{0x0304, 0x0303}, "13"},
		{[]uint16{0x0a0a, 0x0304}, "13"},
		{nil, "00"},
		{[]uint16{0x0a0a}, "00"},
		{[]uint16{0xffff}, "00"},
	}

	for _, tt := range tests {
		result := computeVersion(tt.versions)
		assert.Equal(t, tt.expected, result, "versions=%v", tt.versions)
	}
}

func TestComputeALPN(t *testing.T) {
	t.Parallel()

	tests := []struct {
		protocols []string
		expected  string
	}{
		{[]string{"h2"}, "h2"},
		{[]string{"http/1.1"}, "h1"},
		{[]string{"spdy/3"}, "s3"},
		{nil, "00"},
		{[]string{""}, "00"},
		{[]string{"a"}, "aa"},
	}

	for _, tt := range tests {
		result := computeALPN(tt.protocols)
		assert.Equal(t, tt.expected, result, "protocols=%v", tt.protocols)
	}
}

func TestComputeLanguage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		lang     string
		expected string
	}{
		{"en-US,en;q=0.9", "enus"},
		{"en", "en00"},
		{"", "0000"},
		{"fr-FR,fr;q=0.9", "frfr"},
		{"zh-CN,zh;q=0.9", "zhcn"},
		{"-;,=", "0000"},
	}

	for _, tt := range tests {
		result := computeLanguage(tt.lang)
		assert.Equal(t, tt.expected, result, "lang=%q", tt.lang)
	}
}

func TestComputeExtHash_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("empty extensions and sigAlgorithms", func(t *testing.T) {
		t.Parallel()

		res := computeExtHash(nil, nil)
		assert.Equal(t, "000000000000", res)
	})

	t.Run("empty extensions but non-empty sigAlgorithms", func(t *testing.T) {
		t.Parallel()

		res := computeExtHash(nil, []uint16{0x0403})
		assert.Len(t, res, 12)
	})
}

func TestParseExtensionsFromRaw(t *testing.T) {
	t.Parallel()

	raw := make([]byte, 0, 100)
	raw = append(raw, 0x03, 0x03)
	raw = append(raw, make([]byte, 32)...)
	raw = append(raw, 0x00)
	raw = append(raw, 0x00, 0x04, 0x13, 0x01, 0x13, 0x02)
	raw = append(raw, 0x01, 0x00)

	var extBlock []byte

	sniData := []byte{0x00, 0x00, 0x00}
	sniHeader := []byte{0x00, 0x00, byte(len(sniData) >> 8), byte(len(sniData))}
	extBlock = append(extBlock, sniHeader...)
	extBlock = append(extBlock, sniData...)

	sigData := []byte{0x04, 0x03, 0x08, 0x04}
	sigHeader := []byte{0x00, 0x0d, byte(len(sigData) >> 8), byte(len(sigData))}
	extBlock = append(extBlock, sigHeader...)
	extBlock = append(extBlock, sigData...)

	raw = append(raw, byte(len(extBlock)>>8), byte(len(extBlock)))
	raw = append(raw, extBlock...)

	exts, sigAlgos := ParseExtensionsFromRaw(raw)

	require.NotNil(t, exts)
	assert.Contains(t, exts, uint16(0x0000))
	assert.Contains(t, exts, uint16(0x000d))

	require.Len(t, sigAlgos, 2)
	assert.Equal(t, uint16(0x0403), sigAlgos[0])
	assert.Equal(t, uint16(0x0804), sigAlgos[1])
}

func TestParseExtensionsFromRaw_RecordHeaderWrapper(t *testing.T) {
	t.Parallel()

	// 5-byte TLS Record Header (0x16 = Handshake, 0x03 0x03 = TLS 1.2, length 42)
	recordHeader := []byte{0x16, 0x03, 0x03, 0x00, 0x2a}
	// 4-byte Handshake Header (0x01 = ClientHello, length 38)
	handshakeHeader := []byte{0x01, 0x00, 0x00, 0x26}

	clientHelloBody := make([]byte, 38)

	raw := append(recordHeader, handshakeHeader...) //nolint:gocritic
	raw = append(raw, clientHelloBody...)

	exts, sigAlgos := ParseExtensionsFromRaw(raw)
	assert.Nil(t, exts)
	assert.Nil(t, sigAlgos)
}

func TestParseExtensionsFromRaw_BoundaryAndErrorCases(t *testing.T) {
	t.Parallel()

	buildHelloPrefix := func(extBlock []byte) []byte {
		raw := make([]byte, 0, 100)
		raw = append(raw, 0x03, 0x03)
		raw = append(raw, make([]byte, 32)...)
		raw = append(raw, 0x00)
		raw = append(raw, 0x00, 0x02, 0x13, 0x01)
		raw = append(raw, 0x01, 0x00)
		raw = append(raw, byte(len(extBlock)>>8), byte(len(extBlock)))
		raw = append(raw, extBlock...)

		return raw
	}

	t.Run("too short raw", func(t *testing.T) {
		t.Parallel()

		exts, sigAlgos := ParseExtensionsFromRaw([]byte{0x00})
		assert.Nil(t, exts)
		assert.Nil(t, sigAlgos)
	})

	t.Run("session id out of bounds", func(t *testing.T) {
		t.Parallel()

		raw := make([]byte, 38)
		raw[34] = 10
		exts, sigAlgos := ParseExtensionsFromRaw(raw)
		assert.Nil(t, exts)
		assert.Nil(t, sigAlgos)
	})

	t.Run("cipher suites header out of bounds", func(t *testing.T) {
		t.Parallel()

		raw := make([]byte, 40)
		raw[34] = 5
		exts, sigAlgos := ParseExtensionsFromRaw(raw)
		assert.Nil(t, exts)
		assert.Nil(t, sigAlgos)
	})

	t.Run("cipher suites payload out of bounds", func(t *testing.T) {
		t.Parallel()

		raw := make([]byte, 42)
		raw[34] = 0
		binary.BigEndian.PutUint16(raw[35:37], 10)
		exts, sigAlgos := ParseExtensionsFromRaw(raw)
		assert.Nil(t, exts)
		assert.Nil(t, sigAlgos)
	})

	t.Run("compression methods len out of bounds", func(t *testing.T) {
		t.Parallel()

		raw := make([]byte, 39)
		raw[34] = 0
		binary.BigEndian.PutUint16(raw[35:37], 2)
		exts, sigAlgos := ParseExtensionsFromRaw(raw)
		assert.Nil(t, exts)
		assert.Nil(t, sigAlgos)
	})

	t.Run("compression methods payload out of bounds", func(t *testing.T) {
		t.Parallel()

		raw := make([]byte, 40)
		raw[34] = 0
		binary.BigEndian.PutUint16(raw[35:37], 2)
		raw[39] = 5
		exts, sigAlgos := ParseExtensionsFromRaw(raw)
		assert.Nil(t, exts)
		assert.Nil(t, sigAlgos)
	})

	t.Run("extensions total length out of bounds", func(t *testing.T) {
		t.Parallel()

		raw := make([]byte, 41)
		raw[34] = 0
		binary.BigEndian.PutUint16(raw[35:37], 2)
		raw[39] = 1
		exts, sigAlgos := ParseExtensionsFromRaw(raw)
		assert.Nil(t, exts)
		assert.Nil(t, sigAlgos)
	})

	t.Run("sigAlgos extDataLen too short", func(t *testing.T) {
		t.Parallel()

		extBlock := []byte{
			0x00, 0x0d,
			0x00, 0x01,
			0x00,
		}
		raw := buildHelloPrefix(extBlock)
		exts, sigAlgos := ParseExtensionsFromRaw(raw)
		require.NotNil(t, exts)
		assert.Contains(t, exts, uint16(0x000d))
		assert.Nil(t, sigAlgos)
	})

	t.Run("sigAlgos offset out of bounds", func(t *testing.T) {
		t.Parallel()

		extBlock := []byte{
			0x00, 0x0d,
			0x00, 0x0a,
			0x01, 0x02,
		}
		raw := buildHelloPrefix(extBlock)
		exts, sigAlgos := ParseExtensionsFromRaw(raw)
		require.NotNil(t, exts)
		assert.Contains(t, exts, uint16(0x000d))
		assert.Nil(t, sigAlgos)
	})
}

func TestHash12(t *testing.T) {
	t.Parallel()

	res := hash12Hex([]byte("test"))
	assert.Len(t, res, 12)
	assert.Regexp(t, `^[a-f0-9]{12}$`, res)
	assert.Equal(t, res, hash12Hex([]byte("test")))
	assert.NotEqual(t, res, hash12Hex([]byte("other")))
}

func BenchmarkComputeJA4(b *testing.B) {
	ciphers := []uint16{
		0x002f, 0x0035, 0x009c, 0x009d, 0x1301, 0x1302, 0x1303,
		0xc013, 0xc014, 0xc02b, 0xc02c, 0xc02f, 0xc030, 0xcca8, 0xcca9,
	}
	extensions := []uint16{
		0x0000, 0x0005, 0x000a, 0x000b, 0x000d, 0x0010, 0x0012,
		0x0015, 0x0017, 0x001b, 0x0023, 0x002b, 0x002d, 0x0033, 0x4469, 0xff01,
	}
	supportedVersions := []uint16{0x0304}
	alpn := []string{"h2"}
	sigAlgos := []uint16{0x0403, 0x0804, 0x0401, 0x0503, 0x0805, 0x0501, 0x0806, 0x0601}

	b.ReportAllocs()

	for b.Loop() {
		_ = ComputeJA4(ciphers, extensions, supportedVersions, true, alpn, sigAlgos)
	}
}

func BenchmarkComputeJA4H(b *testing.B) {
	headers := []string{"Host", "User-Agent", "Accept", "Accept-Language", "Accept-Encoding"}
	cookieNames := []string{"session", "token"}
	cookieValues := []string{"abc123", "xyz789"}

	b.ReportAllocs()

	for b.Loop() {
		_ = ComputeJA4H("POST", "HTTP/1.1", headers, true, true, "en-US", cookieNames, cookieValues)
	}
}
