// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cert_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/netutil/cert"
)

func generateTestKeyPair(t *testing.T, dir, org string) (certPath, keyPath string) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			Organization: []string{org},
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	require.NoError(t, err)

	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")

	certOut, err := os.Create(certPath)
	require.NoError(t, err)

	_ = pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	_ = certOut.Close()

	keyOut, err := os.Create(keyPath)
	require.NoError(t, err)

	privBytes, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)

	_ = pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: privBytes})
	_ = keyOut.Close()

	return certPath, keyPath
}

func TestCertWatcher(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	certPath, keyPath := generateTestKeyPair(t, dir, "Aoni Test Org")

	watcher, err := cert.NewWatcher(certPath, keyPath, 50*time.Millisecond)
	require.NoError(t, err)
	t.Cleanup(watcher.Close)

	tlsCert, err := watcher.GetCertificate(nil)
	require.NoError(t, err)
	require.NotNil(t, tlsCert)

	clientCert, err := watcher.GetClientCertificate(nil)
	require.NoError(t, err)
	require.NotNil(t, clientCert)

	// Verify certificate organization
	parsed, err := x509.ParseCertificate(tlsCert.Certificate[0])
	require.NoError(t, err)
	assert.Equal(t, "Aoni Test Org", parsed.Subject.Organization[0])
}

func TestCertWatcher_HotReload(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	certPath, keyPath := generateTestKeyPair(t, dir, "Initial Org")

	watcher, err := cert.NewWatcher(certPath, keyPath, 20*time.Millisecond)
	require.NoError(t, err)
	t.Cleanup(watcher.Close)

	initialCert, err := watcher.GetCertificate(nil)
	require.NoError(t, err)

	parsedInitial, err := x509.ParseCertificate(initialCert.Certificate[0])
	require.NoError(t, err)
	assert.Equal(t, "Initial Org", parsedInitial.Subject.Organization[0])

	// Ensure modification time changes on disk
	time.Sleep(100 * time.Millisecond)

	// Regenerate certificate with new Organization
	generateTestKeyPair(t, dir, "Updated Org")

	// Wait for watcher ticker to trigger reload
	assert.Eventually(t, func() bool {
		currentCert, err := watcher.GetCertificate(nil)
		if err != nil || currentCert == nil {
			return false
		}

		parsedCurrent, err := x509.ParseCertificate(currentCert.Certificate[0])
		if err != nil {
			return false
		}

		return parsedCurrent.Subject.Organization[0] == "Updated Org"
	}, 1*time.Second, 10*time.Millisecond)
}

func TestCertWatcher_InvalidFiles(t *testing.T) {
	t.Parallel()

	_, err := cert.NewWatcher("/nonexistent/cert.pem", "/nonexistent/key.pem", 50*time.Millisecond)
	require.Error(t, err)
}
