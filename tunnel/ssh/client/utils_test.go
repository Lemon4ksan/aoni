// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"
	golangssh "golang.org/x/crypto/ssh"

	"github.com/lemon4ksan/aoni/tunnel/ssh/client"
)

func generateTestKeyPair(t *testing.T) ([]byte, golangssh.Signer, golangssh.PublicKey) {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	signer, err := golangssh.NewSignerFromKey(priv)
	require.NoError(t, err)

	pemBlock, err := golangssh.MarshalPrivateKey(priv, "test-key")
	require.NoError(t, err)

	pemBytes := pem.EncodeToMemory(pemBlock)
	_ = pub

	return pemBytes, signer, signer.PublicKey()
}

func generateEncryptedKey(t *testing.T, passphrase string) []byte {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	pemBlock, err := golangssh.MarshalPrivateKeyWithPassphrase(priv, "encrypted-key", []byte(passphrase))
	require.NoError(t, err)

	return pem.EncodeToMemory(pemBlock)
}

func TestParseKey(t *testing.T) {
	t.Parallel()

	t.Run("valid unencrypted key", func(t *testing.T) {
		t.Parallel()

		pemBytes, _, _ := generateTestKeyPair(t)

		signer, err := client.ParseKey(pemBytes, "")
		require.NoError(t, err)
		require.NotNil(t, signer)
	})

	t.Run("valid encrypted key with correct passphrase", func(t *testing.T) {
		t.Parallel()

		pass := "secret123"
		pemBytes := generateEncryptedKey(t, pass)

		signer, err := client.ParseKey(pemBytes, pass)
		require.NoError(t, err)
		require.NotNil(t, signer)
	})

	t.Run("encrypted key with wrong passphrase", func(t *testing.T) {
		t.Parallel()

		pass := "secret123"
		pemBytes := generateEncryptedKey(t, pass)

		signer, err := client.ParseKey(pemBytes, "wrongpass")
		require.Error(t, err)
		assert.ErrorIs(t, err, client.ErrInvalidPrivateKey)
		assert.Nil(t, signer)
	})

	t.Run("invalid pem data", func(t *testing.T) {
		t.Parallel()

		signer, err := client.ParseKey([]byte("not-a-pem-key"), "")
		require.Error(t, err)
		assert.ErrorIs(t, err, client.ErrInvalidPrivateKey)
		assert.Nil(t, signer)
	})
}

func TestParseKeyFile(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	keyPath := filepath.Join(tempDir, "id_ed25519")

	pemBytes, _, _ := generateTestKeyPair(t)
	require.NoError(t, os.WriteFile(keyPath, pemBytes, 0o600))

	t.Run("existing valid key file", func(t *testing.T) {
		t.Parallel()

		signer, err := client.ParseKeyFile(keyPath, "")
		require.NoError(t, err)
		require.NotNil(t, signer)
	})

	t.Run("non existent key file", func(t *testing.T) {
		t.Parallel()

		signer, err := client.ParseKeyFile(filepath.Join(tempDir, "non_existent"), "")
		require.Error(t, err)
		assert.Nil(t, signer)
	})
}

func TestDefaultKnownHostsPath(t *testing.T) {
	t.Parallel()

	path, err := client.DefaultKnownHostsPath()
	require.NoError(t, err)
	assert.NotEmpty(t, path)
	assert.Contains(t, path, filepath.Join(".ssh", "known_hosts"))
}

func TestKnownHostsAndEnsure(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	knownFile := filepath.Join(tempDir, "subdir", "known_hosts")

	t.Run("EnsureKnownHosts creates file and parent dir", func(t *testing.T) {
		cb, err := client.EnsureKnownHosts(knownFile)
		require.NoError(t, err)
		require.NotNil(t, cb)

		info, err := os.Stat(knownFile)
		require.NoError(t, err)
		assert.False(t, info.IsDir())
	})

	t.Run("KnownHosts loads existing file", func(t *testing.T) {
		cb, err := client.KnownHosts(knownFile)
		require.NoError(t, err)
		require.NotNil(t, cb)
	})
}

func TestAddAndCheckKnownHost(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	knownFile := filepath.Join(tempDir, "known_hosts")

	_, _, pubKey := generateTestKeyPair(t)
	_, _, otherPubKey := generateTestKeyPair(t)

	addr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 2222}
	host := "example.com:2222"

	t.Run("check before adding returns host not found", func(t *testing.T) {
		_, err := client.EnsureKnownHosts(knownFile)
		require.NoError(t, err)

		ok, err := client.CheckKnownHost(host, addr, pubKey, knownFile)
		assert.False(t, ok)
		assert.ErrorIs(t, err, client.ErrHostNotFound)
	})

	t.Run("add known host", func(t *testing.T) {
		err := client.AddKnownHost(host, addr, pubKey, knownFile)
		require.NoError(t, err)
	})

	t.Run("check known host matched", func(t *testing.T) {
		ok, err := client.CheckKnownHost(host, addr, pubKey, knownFile)
		assert.True(t, ok)
		assert.NoError(t, err)
	})

	t.Run("check host key mismatch", func(t *testing.T) {
		ok, err := client.CheckKnownHost(host, addr, otherPubKey, knownFile)
		assert.True(t, ok)
		assert.ErrorIs(t, err, client.ErrHostKeyMismatch)
	})
}
