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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
		assert.Equal(t, tt.expected, isCDN, "IP: %s", tt.ip)
		assert.Equal(t, tt.provider, provider, "IP: %s", tt.ip)
	}

	isCDN, provider := probe.CheckCDN(nil)
	assert.False(t, isCDN)
	assert.Equal(t, probe.CDNUnknown, provider)
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
			Raw:          []byte("dummy cert raw bytes"),
			Subject:      pkix.Name{CommonName: "example.com"},
			Issuer:       pkix.Name{CommonName: "Example CA"},
			DNSNames:     []string{"example.com", "www.example.com"},
			SerialNumber: big.NewInt(12345),
			NotAfter:     time.Now().Add(30 * 24 * time.Hour),
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
		assert.GreaterOrEqual(t, info.DaysUntilExpiry, 29)
	})
}

func TestPredictor(t *testing.T) {
	t.Parallel()

	predictor := probe.NewPredictor()
	require.NotNil(t, predictor)

	predictions := predictor.Predict([]int{80}, 0.10)
	require.NotEmpty(t, predictions)

	found443 := false
	for _, pred := range predictions {
		if pred.Port == 443 {
			found443 = true

			assert.Greater(t, pred.Confidence, 0.5)
		}
	}

	assert.True(t, found443, "Predictor should recommend port 443 when port 80 is open")
}

func TestPortScan_Helpers(t *testing.T) {
	t.Parallel()

	assert.NotEmpty(t, probe.Top20Ports)
	assert.Contains(t, probe.Top20Ports, 80)
	assert.Contains(t, probe.Top20Ports, 443)
}
