// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package probe_test

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/netutil/probe"
)

func TestCheckCDN(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ip       string
		expected bool
		provider probe.CDNProvider
	}{
		{"104.16.1.1", true, probe.CDNCloudflare},
		{"172.64.0.1", true, probe.CDNCloudflare},
		{"23.1.2.3", true, probe.CDNAkamai},
		{"151.101.1.1", true, probe.CDNFastly},
		{"8.8.8.8", false, probe.CDNUnknown},
	}

	for _, tt := range tests {
		isCDN, provider := probe.CheckCDN(net.ParseIP(tt.ip))
		assert.Equalf(t, tt.expected, isCDN, "IP: %s", tt.ip)
		assert.Equalf(t, tt.provider, provider, "IP: %s", tt.ip)

		addr, _ := netip.ParseAddr(tt.ip)
		isCDNAddr, providerAddr := probe.CheckCDNAddr(addr)
		assert.Equalf(t, tt.expected, isCDNAddr, "Addr: %s", tt.ip)
		assert.Equalf(t, tt.provider, providerAddr, "Addr: %s", tt.ip)
	}

	isCDN, provider := probe.CheckCDN(nil)
	assert.False(t, isCDN)
	assert.Equal(t, probe.CDNUnknown, provider)

	isCDNAddr, providerAddr := probe.CheckCDNAddr(netip.Addr{})
	assert.False(t, isCDNAddr)
	assert.Equal(t, probe.CDNUnknown, providerAddr)
}

func BenchmarkCheckCDNAddr(b *testing.B) {
	addr, _ := netip.ParseAddr("104.16.1.1")

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_, _ = probe.CheckCDNAddr(addr)
	}
}

func BenchmarkCheckCDN_LegacyIP(b *testing.B) {
	ip := net.ParseIP("104.16.1.1")

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_, _ = probe.CheckCDN(ip)
	}
}

func TestInspectTLSChain(t *testing.T) {
	t.Parallel()

	t.Run("nil_state", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, probe.InspectTLSChain(nil))
	})

	t.Run("valid_state", func(t *testing.T) {
		t.Parallel()

		cert := &x509.Certificate{
			Raw:                     []byte("dummy cert raw bytes"),
			RawSubjectPublicKeyInfo: []byte("spki test bytes"),
			Subject:                 pkix.Name{CommonName: "example.com"},
			Issuer:                  pkix.Name{CommonName: "Example CA"},
			DNSNames:                []string{"example.com", "www.example.com"},
			SerialNumber:            big.NewInt(12345),
			NotAfter:                time.Now().Add(30 * 24 * time.Hour),
		}

		state := &tls.ConnectionState{
			Version:          tls.VersionTLS13,
			CipherSuite:      tls.TLS_AES_128_GCM_SHA256,
			PeerCertificates: []*x509.Certificate{cert},
		}

		info := probe.InspectTLSChain(state)
		require.NotNil(t, info)

		assert.Equal(t, "TLS 1.3", info.Version)
		assert.Contains(t, info.Subject, "example.com")
		assert.Contains(t, info.Issuer, "Example CA")
		assert.Contains(t, info.DNSNames, "example.com")
		assert.NotEmpty(t, info.FingerprintSHA256)
		assert.NotEmpty(t, info.SPKIFingerprint)
		assert.NotEmpty(t, info.SPKIPin)
		assert.Len(t, info.SPKIPins, 1)
		assert.GreaterOrEqual(t, info.DaysUntilExpiry, 29)
	})
}

func TestRunConnectionDiagnostics_Nil(t *testing.T) {
	t.Parallel()

	report := probe.RunConnectionDiagnostics(nil, "example.com")
	require.NotNil(t, report)
	assert.Equal(t, "example.com", report.Target)
	assert.Nil(t, report.TLSInfo)
}
