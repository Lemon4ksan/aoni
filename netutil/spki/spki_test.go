// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package spki_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/netutil/spki"
)

func generateTestCert(t *testing.T, commonName string) (*x509.Certificate, []byte) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{"SPKI Test Org"},
		},
		NotBefore: time.Now().Add(-time.Hour),
		NotAfter:  time.Now().Add(24 * time.Hour),
		KeyUsage:  x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		DNSNames:  []string{commonName},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(certDER)
	require.NoError(t, err)

	return cert, certDER
}

func TestSPKI_CalculationAndVerification(t *testing.T) {
	t.Parallel()

	cert1, cert1DER := generateTestCert(t, "api.example.com")
	cert2, _ := generateTestCert(t, "backup.example.com")

	t.Run("compute_fingerprint_and_pin", func(t *testing.T) {
		t.Parallel()

		fp := spki.ComputeSPKIFingerprint(cert1)
		require.NotEmpty(t, fp)

		pin := spki.ComputeSPKIPin(cert1)
		assert.True(t, strings.HasPrefix(pin, `pin-sha256="`))
		assert.True(t, strings.HasSuffix(pin, `"`))
		assert.Contains(t, pin, fp)

		fpDER, err := spki.ComputeSPKIFingerprintFromDER(cert1DER)
		require.NoError(t, err)
		assert.Equal(t, fp, fpDER)

		fpRaw := spki.ComputeSPKIFingerprintFromSPKI(cert1.RawSubjectPublicKeyInfo)
		assert.Equal(t, fp, fpRaw)
	})

	t.Run("verify_pins_matching", func(t *testing.T) {
		t.Parallel()

		fp1 := spki.ComputeSPKIFingerprint(cert1)
		fp2 := spki.ComputeSPKIFingerprint(cert2)

		state := &tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{cert1},
		}

		assert.True(t, spki.VerifySPKIPins(state, []string{fp1}))
		assert.True(t, spki.VerifySPKIPins(state, []string{`pin-sha256="` + fp1 + `"`}))
		assert.True(t, spki.VerifySPKIPins(state, []string{fp2, fp1}))
		assert.False(t, spki.VerifySPKIPins(state, []string{fp2}))
		assert.False(t, spki.VerifySPKIPins(state, nil))
		assert.False(t, spki.VerifySPKIPins(nil, []string{fp1}))
	})

	t.Run("normalize_pin", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, "d6qzRu9zOECb90Uez27xWltNsj0e1Md7GkYYkVoZWmM=",
			spki.NormalizePin(`pin-sha256="d6qzRu9zOECb90Uez27xWltNsj0e1Md7GkYYkVoZWmM="`))
		assert.Equal(t, "d6qzRu9zOECb90Uez27xWltNsj0e1Md7GkYYkVoZWmM=",
			spki.NormalizePin(`d6qzRu9zOECb90Uez27xWltNsj0e1Md7GkYYkVoZWmM=`))
	})
}
