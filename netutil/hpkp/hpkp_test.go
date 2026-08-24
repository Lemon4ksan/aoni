// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package hpkp_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/netutil/hpkp"
)

func generateTestCert(t *testing.T, commonName string) (*x509.Certificate, []byte) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{"HPKP Test Org"},
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

func TestRFC7469_ParseHeader(t *testing.T) {
	t.Parallel()

	t.Run("basic_two_pins_with_max_age", func(t *testing.T) {
		t.Parallel()

		header := `max-age=3000; pin-sha256="d6qzRu9zOECb90Uez27xWltNsj0e1Md7GkYYkVoZWmM="; pin-sha256="E9CZ9INDbd+2eRQozYqqbQ2yXLVKB9+xcprMF+44U1g="`
		policy, err := hpkp.ParseHeader(header)
		require.NoError(t, err)
		assert.Equal(t, 3000*time.Second, policy.MaxAge)
		assert.Len(t, policy.Pins, 2)
		assert.False(t, policy.IncludeSubDomains)
		assert.Empty(t, policy.ReportURI)
		assert.True(t, policy.HasPin("d6qzRu9zOECb90Uez27xWltNsj0e1Md7GkYYkVoZWmM="))
		assert.True(t, policy.HasPin("E9CZ9INDbd+2eRQozYqqbQ2yXLVKB9+xcprMF+44U1g="))
	})

	t.Run("full_header_with_report_uri_and_subdomains", func(t *testing.T) {
		t.Parallel()

		header := `pin-sha256="d6qzRu9zOECb90Uez27xWltNsj0e1Md7GkYYkVoZWmM="; pin-sha256="E9CZ9INDbd+2eRQozYqqbQ2yXLVKB9+xcprMF+44U1g="; pin-sha256="LPJNul+wow4m6DsqxbninhsWHlwfp0JecwQzYpOLmCQ="; max-age=10000; includeSubDomains; report-uri="https://report.example.com/pkp"`
		policy, err := hpkp.ParseHeader(header)
		require.NoError(t, err)
		assert.Equal(t, 10000*time.Second, policy.MaxAge)
		assert.Len(t, policy.Pins, 3)
		assert.True(t, policy.IncludeSubDomains)
		assert.Equal(t, "https://report.example.com/pkp", policy.ReportURI)

		formatted := policy.String()
		assert.Contains(t, formatted, "max-age=10000")
		assert.Contains(t, formatted, `pin-sha256="d6qzRu9zOECb90Uez27xWltNsj0e1Md7GkYYkVoZWmM="`)
		assert.Contains(t, formatted, "includeSubDomains")
		assert.Contains(t, formatted, `report-uri="https://report.example.com/pkp"`)
	})

	t.Run("missing_max_age_in_enforce_mode", func(t *testing.T) {
		t.Parallel()

		header := `pin-sha256="d6qzRu9zOECb90Uez27xWltNsj0e1Md7GkYYkVoZWmM="`
		_, err := hpkp.ParseHeader(header)
		assert.ErrorIs(t, err, hpkp.ErrMissingMaxAge)
	})

	t.Run("missing_pins", func(t *testing.T) {
		t.Parallel()

		header := `max-age=5000; includeSubDomains`
		_, err := hpkp.ParseHeader(header)
		assert.ErrorIs(t, err, hpkp.ErrMissingPins)
	})

	t.Run("empty_header", func(t *testing.T) {
		t.Parallel()

		_, err := hpkp.ParseHeader("")
		assert.ErrorIs(t, err, hpkp.ErrEmptyPinningHeader)
	})
}

func TestRFC7469_FromResponse_And_FromHeader(t *testing.T) {
	t.Parallel()

	t.Run("extract_public_key_pins", func(t *testing.T) {
		t.Parallel()

		resp := &http.Response{
			Header: http.Header{
				hpkp.HeaderPublicKeyPins: []string{
					`max-age=5000; pin-sha256="d6qzRu9zOECb90Uez27xWltNsj0e1Md7GkYYkVoZWmM="`,
				},
			},
		}

		policy, err := hpkp.FromResponse(resp)
		require.NoError(t, err)
		assert.False(t, policy.ReportOnly)
		assert.Equal(t, 5000*time.Second, policy.MaxAge)
	})

	t.Run("extract_report_only", func(t *testing.T) {
		t.Parallel()

		resp := &http.Response{
			Header: http.Header{
				hpkp.HeaderPublicKeyPinsReportOnly: []string{
					`max-age=2592000; pin-sha256="d6qzRu9zOECb90Uez27xWltNsj0e1Md7GkYYkVoZWmM="; report-uri="https://other.example.net/pkp-report"`,
				},
			},
		}

		policy, err := hpkp.FromResponse(resp)
		require.NoError(t, err)
		assert.True(t, policy.ReportOnly)
		assert.Equal(t, "https://other.example.net/pkp-report", policy.ReportURI)
	})
}

func TestRFC7469_SPKIFingerprint_And_Validation(t *testing.T) {
	t.Parallel()

	cert1, raw1 := generateTestCert(t, "api.example.com")
	cert2, _ := generateTestCert(t, "backup.example.com")
	cert3, _ := generateTestCert(t, "unrelated.example.com")

	fp1 := hpkp.ComputeSPKIFingerprint(cert1)
	require.NotEmpty(t, fp1)

	fp2 := hpkp.ComputeSPKIFingerprint(cert2)
	require.NotEmpty(t, fp2)

	fp3 := hpkp.ComputeSPKIFingerprint(cert3)
	require.NotEmpty(t, fp3)

	pin1Str := hpkp.ComputeSPKIPin(cert1)
	assert.True(t, strings.HasPrefix(pin1Str, `pin-sha256="`))

	fpFromDER, err := hpkp.ComputeSPKIFingerprintFromDER(raw1)
	require.NoError(t, err)
	assert.Equal(t, fp1, fpFromDER)

	fpFromSPKI := hpkp.ComputeSPKIFingerprintFromSPKI(cert1.RawSubjectPublicKeyInfo)
	assert.Equal(t, fp1, fpFromSPKI)

	// Build Policy with cert1 and backup cert2
	header := "max-age=60000; " + pin1Str + "; " + hpkp.ComputeSPKIPin(cert2)
	policy, err := hpkp.ParseHeader(header)
	require.NoError(t, err)

	// Validate Chain
	t.Run("valid_chain", func(t *testing.T) {
		t.Parallel()

		err := policy.ValidateChain([]*x509.Certificate{cert1})
		assert.NoError(t, err)
	})

	t.Run("valid_raw_certs", func(t *testing.T) {
		t.Parallel()

		err := policy.ValidateRawCerts([][]byte{raw1})
		assert.NoError(t, err)
	})

	t.Run("invalid_chain_mismatch", func(t *testing.T) {
		t.Parallel()

		err := policy.ValidateChain([]*x509.Certificate{cert3})
		assert.ErrorIs(t, err, hpkp.ErrNoMatchingPin)
	})

	t.Run("validate_noting_criteria_success", func(t *testing.T) {
		t.Parallel()

		// cert1 is in the chain, cert2 is backup
		err := policy.ValidateNotingCriteria([]*x509.Certificate{cert1})
		assert.NoError(t, err)
	})

	t.Run("validate_noting_criteria_missing_backup", func(t *testing.T) {
		t.Parallel()

		// Policy with only cert1 (no backup pin)
		singlePinPolicy, err := hpkp.ParseHeader("max-age=60000; " + pin1Str)
		require.NoError(t, err)

		err = singlePinPolicy.ValidateNotingCriteria([]*x509.Certificate{cert1})
		assert.ErrorIs(t, err, hpkp.ErrNoBackupPin)
	})
}

func TestRFC7469_ValidationReport(t *testing.T) {
	t.Parallel()

	cert1, _ := generateTestCert(t, "www.example.com")
	cert2, _ := generateTestCert(t, "ca.example.com")

	fp1 := hpkp.ComputeSPKIPin(cert1)
	fp2 := hpkp.ComputeSPKIPin(cert2)

	policy, err := hpkp.ParseHeader(
		"max-age=2592000; " + fp1 + "; " + fp2 + "; includeSubDomains; report-uri=\"https://report.example.com/pkp\"",
	)
	require.NoError(t, err)

	now := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	report := hpkp.BuildValidationReport(
		"www.example.com",
		443,
		"example.com",
		policy,
		[]*x509.Certificate{cert1},
		[]*x509.Certificate{cert1, cert2},
		now,
	)

	require.NotNil(t, report)
	assert.Equal(t, "www.example.com", report.Hostname)
	assert.Equal(t, 443, report.Port)
	assert.Equal(t, "example.com", report.NotedHostname)
	assert.True(t, report.IncludeSubdomains)
	assert.Len(t, report.ServedCertificateChain, 1)
	assert.Len(t, report.ValidatedCertificateChain, 2)
	assert.Len(t, report.KnownPins, 2)

	data, err := report.JSON()
	require.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, `"hostname": "www.example.com"`)
	assert.Contains(t, jsonStr, `"include-subdomains": true`)
	assert.Contains(t, jsonStr, `"known-pins"`)
	assert.Contains(t, jsonStr, "-----BEGIN CERTIFICATE-----")
}
