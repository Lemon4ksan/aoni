// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ja4

import (
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
	// ALPN section should be "00" when no ALPN protocols
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

	// GREASE values should be excluded from counts and hashes
	ciphers := []uint16{0x0a0a, 0x1301, 0xfafa, 0x1302}
	extensions := []uint16{0x0a0a, 0x0000, 0xfafa}

	result := ComputeJA4(ciphers, extensions, nil, true, nil, nil)
	// 2 real ciphers (0x1301, 0x1302), 1 real extension (0x0000 SNI)
	assert.Contains(t, result, "0201") // 2 ciphers, 1 extension
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
	// Cipher count and ext count should be capped at 99
	assert.Contains(t, result, "t00d999900_")
}

func TestComputeJA4H_BasicGET(t *testing.T) {
	t.Parallel()

	headers := []string{"Host", "User-Agent", "Accept"}
	result := ComputeJA4H("GET", "HTTP/1.1", headers, false, false, "en-US", nil, nil)

	// ge11nn03enus _ hash _ 000000000000 _ 000000000000
	assert.Regexp(t, `^ge11nn03enus_[a-f0-9]{12}_000000000000_000000000000$`, result)
}

func TestComputeJA4H_PostWithCookies(t *testing.T) {
	t.Parallel()

	headers := []string{"Host", "Content-Type"}
	cookieNames := []string{"session", "token"}
	cookieValues := []string{"abc123", "xyz789"}

	result := ComputeJA4H("POST", "HTTP/1.1", headers, true, true, "en", cookieNames, cookieValues)

	// po11cr02en00 _ hash _ namesHash _ valuesHash
	assert.Regexp(t, `^po11cr02[a-z0-9]{4}_[a-f0-9]{12}_[a-f0-9]{12}_[a-f0-9]{12}$`, result)
}

func TestComputeJA4H_NoHeaders(t *testing.T) {
	t.Parallel()

	result := ComputeJA4H("GET", "HTTP/1.0", nil, false, false, "", nil, nil)
	assert.Regexp(t, `^ge10nn000000_[a-f0-9]{12}_000000000000_000000000000$`, result)
}

func TestComputeJA4H_HTTP2PseudoHeaders(t *testing.T) {
	t.Parallel()

	// Pseudo-headers (starting with ":") are NOT filtered by ComputeJA4H.
	// The caller should filter them before passing to the function.
	// This test verifies the function counts all provided headers.
	headers := []string{":method", ":path", ":authority", "Accept", "User-Agent"}
	result := ComputeJA4H("GET", "HTTP/2", headers, false, false, "", nil, nil)

	// All 5 headers counted
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
		{[]uint16{0x0304, 0x0303}, "13"}, // highest wins
		{[]uint16{0x0a0a, 0x0304}, "13"}, // GREASE filtered
		{nil, "00"},
		{[]uint16{0x0a0a}, "00"}, // only GREASE
		{[]uint16{0xffff}, "00"}, // unmapped highest version
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
		{[]string{"a"}, "aa"}, // single character ALPN
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
		{"-;,=", "0000"}, // non-alphanumeric chars
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

	// Build a minimal ClientHello with extensions
	raw := make([]byte, 0, 100)

	// Client version: TLS 1.2
	raw = append(raw, 0x03, 0x03)

	// Random (32 bytes)
	raw = append(raw, make([]byte, 32)...)

	// Session ID: empty
	raw = append(raw, 0x00)

	// Cipher suites: 2 ciphers
	raw = append(raw, 0x00, 0x04) // length
	raw = append(raw, 0x13, 0x01) // TLS_AES_128_GCM_SHA256
	raw = append(raw, 0x13, 0x02) // TLS_AES_256_GCM_SHA384

	// Compression methods: 1 (null)
	raw = append(raw, 0x01, 0x00)

	// Extensions
	var extBlock []byte

	// SNI extension (0x0000)
	sniData := []byte{0x00, 0x00, 0x00} // placeholder
	sniHeader := []byte{0x00, 0x00, byte(len(sniData) >> 8), byte(len(sniData))}
	extBlock = append(extBlock, sniHeader...)
	extBlock = append(extBlock, sniData...)

	// Signature algorithms extension (0x000d)
	sigData := []byte{0x04, 0x03, 0x08, 0x04} // ecdsa_secp256r1_sha256, rsa_pss_rsae_sha256
	sigHeader := []byte{0x00, 0x0d, byte(len(sigData) >> 8), byte(len(sigData))}
	extBlock = append(extBlock, sigHeader...)
	extBlock = append(extBlock, sigData...)

	// Extensions total length
	raw = append(raw, byte(len(extBlock)>>8), byte(len(extBlock)))
	raw = append(raw, extBlock...)

	exts, sigAlgos := ParseExtensionsFromRaw(raw)

	require.NotNil(t, exts)
	assert.Contains(t, exts, uint16(0x0000)) // SNI
	assert.Contains(t, exts, uint16(0x000d)) // signature algorithms

	require.Len(t, sigAlgos, 2)
	assert.Equal(t, uint16(0x0403), sigAlgos[0])
	assert.Equal(t, uint16(0x0804), sigAlgos[1])
}

func TestParseExtensionsFromRaw_BoundaryAndErrorCases(t *testing.T) {
	t.Parallel()

	buildHelloPrefix := func(extBlock []byte) []byte {
		raw := make([]byte, 0, 100)
		raw = append(raw, 0x03, 0x03)                                  // client version
		raw = append(raw, make([]byte, 32)...)                         // random
		raw = append(raw, 0x00)                                        // session ID len = 0
		raw = append(raw, 0x00, 0x02, 0x13, 0x01)                      // cipher suites
		raw = append(raw, 0x01, 0x00)                                  // compression methods
		raw = append(raw, byte(len(extBlock)>>8), byte(len(extBlock))) // ext total len
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
		raw[34] = 10 // sessionIDLen is 10, offset is 35. 35+10 = 45 > 38.
		exts, sigAlgos := ParseExtensionsFromRaw(raw)
		assert.Nil(t, exts)
		assert.Nil(t, sigAlgos)
	})

	t.Run("cipher suites header out of bounds", func(t *testing.T) {
		t.Parallel()

		raw := make([]byte, 40)
		raw[34] = 5 // session id len = 5. offset + 2 (cipher suites len) will exceed 40.
		exts, sigAlgos := ParseExtensionsFromRaw(raw)
		assert.Nil(t, exts)
		assert.Nil(t, sigAlgos)
	})

	t.Run("cipher suites payload out of bounds", func(t *testing.T) {
		t.Parallel()

		raw := make([]byte, 42)
		raw[34] = 0
		binary.BigEndian.PutUint16(raw[35:37], 10) // cipherSuitesLen = 10
		exts, sigAlgos := ParseExtensionsFromRaw(raw)
		assert.Nil(t, exts)
		assert.Nil(t, sigAlgos)
	})

	t.Run("compression methods len out of bounds", func(t *testing.T) {
		t.Parallel()

		raw := make([]byte, 39)
		raw[34] = 0
		binary.BigEndian.PutUint16(raw[35:37], 2) // cipherSuitesLen = 2
		exts, sigAlgos := ParseExtensionsFromRaw(raw)
		assert.Nil(t, exts)
		assert.Nil(t, sigAlgos)
	})

	t.Run("compression methods payload out of bounds", func(t *testing.T) {
		t.Parallel()

		raw := make([]byte, 40)
		raw[34] = 0
		binary.BigEndian.PutUint16(raw[35:37], 2)
		raw[39] = 5 // compMethodsLen = 5
		exts, sigAlgos := ParseExtensionsFromRaw(raw)
		assert.Nil(t, exts)
		assert.Nil(t, sigAlgos)
	})

	t.Run("extensions total length out of bounds", func(t *testing.T) {
		t.Parallel()

		raw := make([]byte, 41)
		raw[34] = 0
		binary.BigEndian.PutUint16(raw[35:37], 2)
		raw[39] = 1 // compMethodsLen = 1
		exts, sigAlgos := ParseExtensionsFromRaw(raw)
		assert.Nil(t, exts)
		assert.Nil(t, sigAlgos)
	})

	t.Run("sigAlgos extDataLen too short", func(t *testing.T) {
		t.Parallel()

		extBlock := []byte{
			0x00, 0x0d, // extID = 0x000d (sigAlgos)
			0x00, 0x01, // extDataLen = 1 (too short)
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
			0x00, 0x0d, // extID = 0x000d (sigAlgos)
			0x00, 0x0a, // extDataLen = 10 (out of bounds)
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

	res := hash12("test")
	assert.Len(t, res, 12)
	assert.Regexp(t, `^[a-f0-9]{12}$`, res)
	assert.Equal(t, res, hash12("test"))
	assert.NotEqual(t, res, hash12("other"))
}
