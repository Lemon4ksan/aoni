// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ech_test

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/fingerprint/ech"
)

func TestDraft22_ECHConfigList_Roundtrip(t *testing.T) {
	t.Parallel()

	cfg := &ech.Config{
		Version: ech.VersionDraft22,
		Contents: ech.ConfigContents{
			KeyConfig: ech.KeyConfig{
				ConfigID:  42,
				KEMID:     ech.KEM_X25519_HKDF_SHA256,
				PublicKey: []byte("test-32-byte-public-key-for-ech-"),
				CipherSuites: []ech.CipherSuite{
					{KDFID: ech.KDF_HKDF_SHA256, AEADID: ech.AEAD_AES_128_GCM},
					{KDFID: ech.KDF_HKDF_SHA256, AEADID: ech.AEAD_CHACHA20_POLY1305},
				},
			},
			MaximumNameLength: 64,
			PublicName:        "cloudflare-ech.com",
			Extensions: []ech.Extension{
				{Type: 0x1a1a, Data: []byte("grease-ext")},
			},
		},
	}

	wireList, err := ech.MarshalConfigList([]*ech.Config{cfg})
	require.NoError(t, err)

	parsedList, err := ech.ParseConfigList(wireList)
	require.NoError(t, err)
	require.Len(t, parsedList, 1)

	parsed := parsedList[0]
	assert.Equal(t, ech.VersionDraft22, parsed.Version)
	assert.Equal(t, uint8(42), parsed.Contents.KeyConfig.ConfigID)
	assert.Equal(t, ech.KEM_X25519_HKDF_SHA256, parsed.Contents.KeyConfig.KEMID)
	assert.Equal(t, []byte("test-32-byte-public-key-for-ech-"), parsed.Contents.KeyConfig.PublicKey)
	assert.Len(t, parsed.Contents.KeyConfig.CipherSuites, 2)
	assert.Equal(t, uint8(64), parsed.Contents.MaximumNameLength)
	assert.Equal(t, "cloudflare-ech.com", parsed.Contents.PublicName)
	assert.Len(t, parsed.Contents.Extensions, 1)

	assert.True(t, parsed.SupportsCipherSuite(ech.KDF_HKDF_SHA256, ech.AEAD_AES_128_GCM))
	assert.False(t, parsed.SupportsCipherSuite(ech.KDF_HKDF_SHA384, ech.AEAD_AES_256_GCM))
}

func TestDraft22_PublicName_Validation(t *testing.T) {
	t.Parallel()

	// Valid DNS names
	assert.NoError(t, ech.ValidatePublicName("example.com"))
	assert.NoError(t, ech.ValidatePublicName("public.gateway.cloudflare.com"))
	assert.NoError(t, ech.ValidatePublicName("sub-domain.org"))

	// Invalid names per draft-ietf-tls-esni-22 §4
	assert.Error(t, ech.ValidatePublicName(""))
	assert.Error(t, ech.ValidatePublicName(".example.com"))
	assert.Error(t, ech.ValidatePublicName("example.com."))
	assert.Error(t, ech.ValidatePublicName("192.168.1.1")) // all-digit final label (IPv4 confusion)
	assert.Error(t, ech.ValidatePublicName("0x7f000001"))  // hex-prefix final label
	assert.Error(t, ech.ValidatePublicName("invalid_char!.com"))
}

func TestDraft22_Padding_Calculation(t *testing.T) {
	t.Parallel()

	// InnerLen = 200, SNILen = 15, MaxNameLen = 32
	// padding from name: max(0, 32 - 15) = 17
	// total = 200 + 17 = 217
	// 32-byte round: N = 31 - ((217 - 1) % 32) = 31 - 24 = 7
	// total padding: 17 + 7 = 24
	pad := ech.CalculatePadding(200, 15, 32)
	assert.Equal(t, 24, pad)
	assert.Equal(t, 0, (200+pad)%32)

	// Without SNI (SNILen = 0):
	// padding from name: 32 + 9 = 41
	// total = 200 + 41 = 241
	// 32-byte round: N = 31 - ((241 - 1) % 32) = 31 - 16 = 15
	// total padding: 41 + 15 = 56
	padNoSNI := ech.CalculatePadding(200, 0, 32)
	assert.Equal(t, 56, padNoSNI)
	assert.Equal(t, 0, (200+padNoSNI)%32)
}

func TestDraft22_Mandatory_Extensions(t *testing.T) {
	t.Parallel()

	extNonMandatory := ech.Extension{Type: 0x1234, Data: []byte{1}}
	assert.False(t, extNonMandatory.IsMandatory())

	extMandatory := ech.Extension{Type: 0x8001, Data: []byte{1}} // high-order bit set
	assert.True(t, extMandatory.IsMandatory())

	cfg := &ech.Config{
		Version: ech.VersionDraft22,
		Contents: ech.ConfigContents{
			PublicName: "example.com",
			Extensions: []ech.Extension{extMandatory},
		},
	}

	assert.True(t, cfg.HasUnsupportedMandatoryExtension(0x8002))
	assert.False(t, cfg.HasUnsupportedMandatoryExtension(0x8001))
}

func TestDraft22_Base64_Parsing(t *testing.T) {
	t.Parallel()

	cfg := &ech.Config{
		Version: ech.VersionDraft22,
		Contents: ech.ConfigContents{
			KeyConfig: ech.KeyConfig{
				ConfigID:  1,
				KEMID:     ech.KEM_X25519_HKDF_SHA256,
				PublicKey: []byte("12345678901234567890123456789012"),
				CipherSuites: []ech.CipherSuite{
					{KDFID: ech.KDF_HKDF_SHA256, AEADID: ech.AEAD_AES_128_GCM},
				},
			},
			MaximumNameLength: 32,
			PublicName:        "public.example.org",
		},
	}

	wire, err := ech.MarshalConfigList([]*ech.Config{cfg})
	require.NoError(t, err)

	b64 := base64.StdEncoding.EncodeToString(wire)
	parsed, err := ech.ParseBase64(b64)
	require.NoError(t, err)
	require.Len(t, parsed, 1)
	assert.Equal(t, "public.example.org", parsed[0].Contents.PublicName)
}
