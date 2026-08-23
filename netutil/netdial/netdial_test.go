// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package netdial_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"math/big"
	"net"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/netutil/netdial"
)

func TestVerifyCertificatePins(t *testing.T) {
	t.Parallel()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(100),
		Subject:      pkix.Name{Organization: []string{"Aoni Pin Test"}},
		DNSNames:     []string{"api.example.com"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(certDER)
	require.NoError(t, err)

	spkiHash := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	correctPinBase64 := base64.StdEncoding.EncodeToString(spkiHash[:])
	correctPinHex := hex.EncodeToString(spkiHash[:])
	wrongPinBase64 := base64.StdEncoding.EncodeToString(make([]byte, 32))

	rawCerts := [][]byte{certDER}

	t.Run("valid_pin_base64", func(t *testing.T) {
		t.Parallel()

		pins := map[string][]string{"api.example.com": {correctPinBase64}}
		err := netdial.VerifyCertificatePins("api.example.com", pins, rawCerts)
		assert.NoError(t, err)
	})

	t.Run("valid_pin_hex", func(t *testing.T) {
		t.Parallel()

		pins := map[string][]string{"api.example.com": {correctPinHex}}
		err := netdial.VerifyCertificatePins("api.example.com", pins, rawCerts)
		assert.NoError(t, err)
	})

	t.Run("wildcard_domain_pin_match", func(t *testing.T) {
		t.Parallel()

		pins := map[string][]string{"*.example.com": {"sha256/" + correctPinBase64}}
		err := netdial.VerifyCertificatePins("api.example.com", pins, rawCerts)
		assert.NoError(t, err)
	})

	t.Run("rfc7469_pin_sha256_format", func(t *testing.T) {
		t.Parallel()

		pins := map[string][]string{"api.example.com": {`pin-sha256="` + correctPinBase64 + `"`}}
		err := netdial.VerifyCertificatePins("api.example.com", pins, rawCerts)
		assert.NoError(t, err)
	})

	t.Run("pin_mismatch_returns_error", func(t *testing.T) {
		t.Parallel()

		pins := map[string][]string{"api.example.com": {wrongPinBase64}}
		err := netdial.VerifyCertificatePins("api.example.com", pins, rawCerts)
		assert.ErrorIs(t, err, netdial.ErrCertificatePinning)
	})

	t.Run("no_certs_returns_error", func(t *testing.T) {
		t.Parallel()

		pins := map[string][]string{"api.example.com": {correctPinBase64}}
		err := netdial.VerifyCertificatePins("api.example.com", pins, nil)
		assert.ErrorIs(t, err, netdial.ErrNoCertificatesPresented)
	})

	t.Run("invalid_pin_format_returns_error", func(t *testing.T) {
		t.Parallel()

		pins := map[string][]string{"api.example.com": {"invalid-pin-str"}}
		err := netdial.VerifyCertificatePins("api.example.com", pins, rawCerts)
		assert.ErrorIs(t, err, netdial.ErrInvalidPinFormat)
	})
}

func TestDialL4_SSRFGuard(t *testing.T) {
	t.Parallel()

	opts := netdial.DialOptions{
		SSRFGuard: true,
	}

	_, err := netdial.DialL4(t.Context(), "tcp", "127.0.0.1:8080", opts)
	require.Error(t, err)
	assert.ErrorIs(t, err, netdial.ErrSSRFBlocked)
}

func TestDialL4_UnixSocket(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	ln, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err == nil {
			_, _ = conn.Write([]byte("unix_ok"))
			_ = conn.Close()
		}
	}()

	opts := netdial.DialOptions{}
	conn, err := netdial.DialL4(t.Context(), "unix", "unix://"+socketPath, opts)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	buf := make([]byte, 10)
	n, err := conn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "unix_ok", string(buf[:n]))
}

func TestL2AddrAndConn(t *testing.T) {
	t.Parallel()

	mac, err := net.ParseMAC("00:11:22:33:44:55")
	require.NoError(t, err)

	addr := &netdial.L2Addr{HardwareAddr: mac}
	assert.Equal(t, "ethernet", addr.Network())
	assert.Equal(t, "00:11:22:33:44:55", addr.String())

	conn := netdial.NewL2FrameConn(nil, nil, nil)
	require.NotNil(t, conn)
	assert.Equal(t, "ethernet", conn.LocalAddr().Network())
	assert.NoError(t, conn.Close())
}
