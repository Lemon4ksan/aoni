// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package svcb_test

import (
	"net"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/netutil/svcb"
)

func TestRFC9460_QueryNameConstruction(t *testing.T) {
	t.Parallel()

	// RFC 9460 §9.1: HTTPS query names
	assert.Equal(t, "example.com.", svcb.BuildHTTPSQueryName("example.com", 443))
	assert.Equal(t, "example.com.", svcb.BuildHTTPSQueryName("example.com.", 0))
	assert.Equal(t, "_8443._https.example.com.", svcb.BuildHTTPSQueryName("example.com", 8443))

	// RFC 9460 §2.3: General SVCB query names
	assert.Equal(t, "_foo.api.example.com.", svcb.BuildSVCBQueryName("foo", "api.example.com", 0))
	assert.Equal(t, "_8080._foo.api.example.com.", svcb.BuildSVCBQueryName("foo", "api.example.com", 8080))
}

func TestRFC9460_AliasMode_Parsing(t *testing.T) {
	t.Parallel()

	rec := &svcb.Record{
		Priority:   0,
		TargetName: "svc.example.net",
	}

	wire, err := rec.MarshalRDATA()
	require.NoError(t, err)

	parsed, err := svcb.ParseRDATA(wire)
	require.NoError(t, err)

	assert.True(t, parsed.IsAlias())
	assert.False(t, parsed.IsService())
	assert.Equal(t, "svc.example.net", parsed.TargetName)
	assert.Equal(t, "svc.example.net", parsed.EffectiveTarget("example.com"))
}

func TestRFC9460_ServiceMode_Parsing_And_Getters(t *testing.T) {
	t.Parallel()

	alpnWire := svcb.EncodeALPN([]string{"h2", "h3"})
	portWire := svcb.EncodePort(8443)
	ipv4Wire := svcb.EncodeIPv4Hints([]net.IP{net.ParseIP("192.0.2.1"), net.ParseIP("192.0.2.2")})
	ipv6Wire := svcb.EncodeIPv6Hints([]net.IP{net.ParseIP("2001:db8::1")})
	echBytes := []byte("ech-config-test-bytes")

	rec := &svcb.Record{
		Priority:   1,
		TargetName: ".",
		Params: map[svcb.SvcParamKey][]byte{
			svcb.ParamALPN:          alpnWire,
			svcb.ParamNoDefaultALPN: nil,
			svcb.ParamPort:          portWire,
			svcb.ParamIPv4Hint:      ipv4Wire,
			svcb.ParamECH:           echBytes,
			svcb.ParamIPv6Hint:      ipv6Wire,
		},
	}

	wire, err := rec.MarshalRDATA()
	require.NoError(t, err)

	parsed, err := svcb.ParseRDATA(wire)
	require.NoError(t, err)

	assert.False(t, parsed.IsAlias())
	assert.True(t, parsed.IsService())
	assert.Equal(t, ".", parsed.TargetName)
	assert.Equal(t, "example.com", parsed.EffectiveTarget("example.com"))

	// ALPN
	assert.Equal(t, []string{"h2", "h3"}, parsed.ALPN())
	assert.True(t, parsed.HasNoDefaultALPN())

	// Port
	port, ok := parsed.Port()
	assert.True(t, ok)
	assert.Equal(t, uint16(8443), port)

	// IPv4 Hints
	v4Hints := parsed.IPv4Hints()
	require.Len(t, v4Hints, 2)
	assert.Equal(t, netip.MustParseAddr("192.0.2.1"), v4Hints[0])
	assert.Equal(t, netip.MustParseAddr("192.0.2.2"), v4Hints[1])

	// IPv6 Hints
	v6Hints := parsed.IPv6Hints()
	require.Len(t, v6Hints, 1)
	assert.Equal(t, netip.MustParseAddr("2001:db8::1"), v6Hints[0])

	// ECH
	assert.Equal(t, echBytes, parsed.ECHConfig())
}

func TestRFC9460_Mandatory_Keys_And_Compatibility(t *testing.T) {
	t.Parallel()

	rec := &svcb.Record{
		Priority:   1,
		TargetName: "svc.example.com",
		Params: map[svcb.SvcParamKey][]byte{
			svcb.ParamMandatory: {0x00, 0x01, 0x00, 0x03}, // alpn (1), port (3)
			svcb.ParamALPN:      svcb.EncodeALPN([]string{"h3"}),
			svcb.ParamPort:      svcb.EncodePort(443),
		},
	}

	wire, err := rec.MarshalRDATA()
	require.NoError(t, err)

	parsed, err := svcb.ParseRDATA(wire)
	require.NoError(t, err)

	mand := parsed.MandatoryKeys()
	assert.Equal(t, []svcb.SvcParamKey{svcb.ParamALPN, svcb.ParamPort}, mand)

	// Compatible when client supports all mandatory keys
	assert.True(t, parsed.IsCompatible(svcb.ParamALPN, svcb.ParamPort, svcb.ParamECH))

	// Incompatible when missing mandatory key
	assert.False(t, parsed.IsCompatible(svcb.ParamALPN))
}

func TestRFC9460_WireValidationErrors(t *testing.T) {
	t.Parallel()

	t.Run("truncated_rdata", func(t *testing.T) {
		t.Parallel()

		_, err := svcb.ParseRDATA([]byte{0x00, 0x01})
		assert.ErrorIs(t, err, svcb.ErrTruncatedRDATA)
	})

	t.Run("unsorted_keys_rejected", func(t *testing.T) {
		t.Parallel()

		// SvcPriority: 1, TargetName: "." (0x00), Key3, Key1 (unsorted)
		invalidWire := []byte{
			0x00, 0x01, // Priority = 1
			0x00,       // TargetName = "."
			0x00, 0x03, // Key = 3 (port)
			0x00, 0x02, // Len = 2
			0x01, 0xBB, // Port = 443
			0x00, 0x01, // Key = 1 (alpn) -> Unsorted!
			0x00, 0x03, // Len = 3
			0x02, 'h', '2',
		}

		_, err := svcb.ParseRDATA(invalidWire)
		assert.ErrorIs(t, err, svcb.ErrUnsortedParamKeys)
	})

	t.Run("duplicate_keys_rejected", func(t *testing.T) {
		t.Parallel()

		// SvcPriority: 1, TargetName: "." (0x00), Key1, Key1 (duplicate)
		dupWire := []byte{
			0x00, 0x01, // Priority = 1
			0x00,       // TargetName = "."
			0x00, 0x01, // Key = 1
			0x00, 0x03, // Len = 3
			0x02, 'h', '2',
			0x00, 0x01, // Key = 1 (duplicate)
			0x00, 0x03,
			0x02, 'h', '3',
		}

		_, err := svcb.ParseRDATA(dupWire)
		assert.ErrorIs(t, err, svcb.ErrDuplicateParamKey)
	})
}

func TestRFC9460_ParamKeyParsing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected svcb.SvcParamKey
		name     string
	}{
		{"mandatory", svcb.ParamMandatory, "mandatory"},
		{"alpn", svcb.ParamALPN, "alpn"},
		{"no-default-alpn", svcb.ParamNoDefaultALPN, "no-default-alpn"},
		{"port", svcb.ParamPort, "port"},
		{"ipv4hint", svcb.ParamIPv4Hint, "ipv4hint"},
		{"ech", svcb.ParamECH, "ech"},
		{"ipv6hint", svcb.ParamIPv6Hint, "ipv6hint"},
		{"key65280", svcb.SvcParamKey(65280), "key65280"},
	}

	for _, tt := range tests {
		k, err := svcb.ParseParamKey(tt.input)
		require.NoError(t, err)
		assert.Equal(t, tt.expected, k)
		assert.Equal(t, tt.name, k.String())
	}

	_, err := svcb.ParseParamKey("unknown-non-key-name")
	assert.ErrorIs(t, err, svcb.ErrMalformedParam)
}
