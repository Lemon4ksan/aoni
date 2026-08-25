// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client_test

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"
	golangssh "golang.org/x/crypto/ssh"

	"github.com/lemon4ksan/aoni/tunnel/ssh/ca"
	"github.com/lemon4ksan/aoni/tunnel/ssh/client"
)

func TestOptions(t *testing.T) {
	t.Run("WithPort", func(t *testing.T) {
		c := &client.Client{}
		cfg := &golangssh.ClientConfig{}

		opt := client.WithPort(2222)
		err := opt(c, cfg)
		require.NoError(t, err)
		assert.Equal(t, uint(2222), c.Port)
	})

	t.Run("WithPassword", func(t *testing.T) {
		c := &client.Client{}
		cfg := &golangssh.ClientConfig{}

		opt := client.WithPassword("supersecret")
		err := opt(c, cfg)
		require.NoError(t, err)
		assert.Len(t, cfg.Auth, 1)
	})

	t.Run("WithKeyboardInteractive", func(t *testing.T) {
		c := &client.Client{}
		cfg := &golangssh.ClientConfig{}

		handler := func(_, _, _ string, _ bool) (string, error) {
			return "ans", nil
		}

		opt := client.WithKeyboardInteractive(handler)
		err := opt(c, cfg)
		require.NoError(t, err)
		assert.Len(t, cfg.Auth, 1)
	})

	t.Run("WithKey and WithSigner", func(t *testing.T) {
		pemBytes, signer, _ := generateTestKeyPair(t)

		t.Run("WithKey valid", func(t *testing.T) {
			c := &client.Client{}
			cfg := &golangssh.ClientConfig{}

			err := client.WithKey(pemBytes, "")(c, cfg)
			require.NoError(t, err)
			assert.Len(t, cfg.Auth, 1)
		})

		t.Run("WithKey invalid", func(t *testing.T) {
			c := &client.Client{}
			cfg := &golangssh.ClientConfig{}

			err := client.WithKey([]byte("bad pem"), "")(c, cfg)
			assert.Error(t, err)
		})

		t.Run("WithSigner", func(t *testing.T) {
			c := &client.Client{}
			cfg := &golangssh.ClientConfig{}

			err := client.WithSigner(signer)(c, cfg)
			require.NoError(t, err)
			assert.Len(t, cfg.Auth, 1)
		})
	})

	t.Run("WithCertSigner", func(t *testing.T) {
		caObj, _, err := ca.GenerateCA()
		require.NoError(t, err)

		_, userSigner, userPub := generateTestKeyPair(t)
		userCert, err := caObj.IssueUserCert(userPub, "alice")
		require.NoError(t, err)

		certSigner, err := golangssh.NewCertSigner(userCert, userSigner)
		require.NoError(t, err)

		c := &client.Client{}
		cfg := &golangssh.ClientConfig{}

		err = client.WithCertSigner(certSigner)(c, cfg)
		require.NoError(t, err)
		assert.Len(t, cfg.Auth, 1)
	})

	t.Run("WithKeyFile", func(t *testing.T) {
		tempDir := t.TempDir()
		keyFile := filepath.Join(tempDir, "id_ed25519")

		pemBytes, _, _ := generateTestKeyPair(t)
		require.NoError(t, os.WriteFile(keyFile, pemBytes, 0o600))

		c := &client.Client{}
		cfg := &golangssh.ClientConfig{}

		err := client.WithKeyFile(keyFile, "")(c, cfg)
		require.NoError(t, err)
		assert.Len(t, cfg.Auth, 1)

		err = client.WithKeyFile(filepath.Join(tempDir, "missing"), "")(c, cfg)
		assert.Error(t, err)
	})

	t.Run("WithAgent variants", func(t *testing.T) {
		t.Run("WithAgent nil conn", func(t *testing.T) {
			c := &client.Client{}
			cfg := &golangssh.ClientConfig{}

			err := client.WithAgent(nil)(c, cfg)
			require.NoError(t, err)
			assert.Empty(t, cfg.Auth)
		})

		t.Run("WithAgentSocket empty", func(t *testing.T) {
			c := &client.Client{}
			cfg := &golangssh.ClientConfig{}

			err := client.WithAgentSocket("")(c, cfg)
			require.NoError(t, err)
			assert.Empty(t, cfg.Auth)
		})

		t.Run("WithDefaultAgent", func(t *testing.T) {
			c := &client.Client{}
			cfg := &golangssh.ClientConfig{}

			t.Setenv("SSH_AUTH_SOCK", "")

			err := client.WithDefaultAgent()(c, cfg)
			require.NoError(t, err)
		})
	})

	t.Run("WithTimeout", func(t *testing.T) {
		c := &client.Client{}
		cfg := &golangssh.ClientConfig{}

		err := client.WithTimeout(10*time.Second)(c, cfg)
		require.NoError(t, err)
		assert.Equal(t, 10*time.Second, cfg.Timeout)
	})

	t.Run("WithWindowSize and WithMaxPacketSize", func(t *testing.T) {
		c := &client.Client{}
		cfg := &golangssh.ClientConfig{}

		require.NoError(t, client.WithWindowSize(8192)(c, cfg))
		assert.Equal(t, uint32(8192), c.WindowSize)

		require.NoError(t, client.WithMaxPacketSize(4096)(c, cfg))
		assert.Equal(t, uint32(4096), c.MaxPacketSize)
	})

	t.Run("WithHighPerformanceDefaults", func(t *testing.T) {
		c := &client.Client{}
		cfg := &golangssh.ClientConfig{}

		err := client.WithHighPerformanceDefaults()(c, cfg)
		require.NoError(t, err)
		assert.Equal(t, uint32(16*1024*1024), c.WindowSize)
		assert.Equal(t, uint32(64*1024), c.MaxPacketSize)
		assert.NotEmpty(t, cfg.Ciphers)
		assert.NotEmpty(t, cfg.KeyExchanges)
	})

	t.Run("WithKnownHosts and WithEnsureKnownHosts", func(t *testing.T) {
		tempDir := t.TempDir()
		knownFile := filepath.Join(tempDir, "known_hosts")
		require.NoError(t, os.WriteFile(knownFile, []byte(""), 0o600))

		c := &client.Client{}
		cfg := &golangssh.ClientConfig{}

		err := client.WithKnownHosts(knownFile)(c, cfg)
		require.NoError(t, err)
		assert.NotNil(t, cfg.HostKeyCallback)

		ensureFile := filepath.Join(tempDir, "new_dir", "known_hosts")
		err = client.WithEnsureKnownHosts(ensureFile)(c, cfg)
		require.NoError(t, err)
		assert.NotNil(t, cfg.HostKeyCallback)
	})

	t.Run("WithFingerprint", func(t *testing.T) {
		_, _, pubKey := generateTestKeyPair(t)
		fp := golangssh.FingerprintSHA256(pubKey)

		c := &client.Client{}
		cfg := &golangssh.ClientConfig{}

		err := client.WithFingerprint(fp)(c, cfg)
		require.NoError(t, err)
		require.NotNil(t, cfg.HostKeyCallback)

		dummyAddr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}
		require.NoError(t, cfg.HostKeyCallback("127.0.0.1", dummyAddr, pubKey))

		_, _, otherPubKey := generateTestKeyPair(t)
		err = cfg.HostKeyCallback("127.0.0.1", dummyAddr, otherPubKey)
		assert.ErrorIs(t, err, client.ErrFingerprintMismatch)
	})

	t.Run("WithCiphers, WithKeyExchanges, WithLegacyCiphers", func(t *testing.T) {
		c := &client.Client{}
		cfg := &golangssh.ClientConfig{}

		require.NoError(t, client.WithCiphers([]string{"my-cipher"})(c, cfg))
		assert.Contains(t, cfg.Ciphers, "my-cipher")

		require.NoError(t, client.WithKeyExchanges([]string{"my-kex"})(c, cfg))
		assert.Contains(t, cfg.KeyExchanges, "my-kex")

		require.NoError(t, client.WithLegacyCiphers()(c, cfg))
		assert.Contains(t, cfg.Ciphers, "aes128-cbc")
	})

	t.Run("WithRequestPty and WithPtyTerminal", func(t *testing.T) {
		c := &client.Client{}
		cfg := &golangssh.ClientConfig{}

		require.NoError(t, client.WithRequestPty(true)(c, cfg))
		assert.True(t, c.RequestPty)

		require.NoError(t, client.WithPtyTerminal("vt100", 120, 50)(c, cfg))
		assert.True(t, c.RequestPty)
		assert.Equal(t, "vt100", c.PtyTerm)
		assert.Equal(t, 120, c.PtyWidth)
		assert.Equal(t, 50, c.PtyHeight)
	})

	t.Run(
		"WithInsecureIgnoreHostKey, WithHostKeyCallback, WithHostKeyAlgorithms, WithBannerCallback",
		func(t *testing.T) {
			c := &client.Client{}
			cfg := &golangssh.ClientConfig{}

			require.NoError(t, client.WithInsecureIgnoreHostKey()(c, cfg))
			assert.NotNil(t, cfg.HostKeyCallback)

			cb := func(_ string, _ net.Addr, _ golangssh.PublicKey) error { return nil }
			require.NoError(t, client.WithHostKeyCallback(cb)(c, cfg))

			require.NoError(t, client.WithHostKeyAlgorithms([]string{"ssh-ed25519"})(c, cfg))
			assert.Equal(t, []string{"ssh-ed25519"}, cfg.HostKeyAlgorithms)

			bannerCb := func(_ string) error { return nil }
			require.NoError(t, client.WithBannerCallback(bannerCb)(c, cfg))
			assert.NotNil(t, cfg.BannerCallback)
		},
	)

	t.Run("WithProxy and WithJump", func(t *testing.T) {
		c := &client.Client{}
		cfg := &golangssh.ClientConfig{}

		require.NoError(t, client.WithProxy("socks5://127.0.0.1:1080")(c, cfg))
		assert.Equal(t, "socks5://127.0.0.1:1080", c.ProxyURL)

		jumpClient := &client.Client{}
		require.NoError(t, client.WithJump(jumpClient)(c, cfg))
		assert.Equal(t, jumpClient, c.Jump)
	})

	t.Run("WithConfig", func(t *testing.T) {
		c := &client.Client{}
		cfg := &golangssh.ClientConfig{}

		opt := client.WithConfig(func(c *golangssh.ClientConfig) error {
			c.User = "custom-user"
			return nil
		})
		require.NoError(t, opt(c, cfg))
		assert.Equal(t, "custom-user", cfg.User)
	})
}

func TestCmdOptions(t *testing.T) {
	t.Parallel()

	cmd := &client.Cmd{
		Session: &golangssh.Session{},
	}

	client.WithPath("/usr/bin/bash")(cmd)
	assert.Equal(t, "/usr/bin/bash", cmd.Path)

	bufOut := &bytes.Buffer{}
	client.WithStdout(bufOut)(cmd)
	assert.Equal(t, bufOut, cmd.Stdout)

	bufErr := &bytes.Buffer{}
	client.WithStderr(bufErr)(cmd)
	assert.Equal(t, bufErr, cmd.Stderr)

	bufIn := bytes.NewReader([]byte("input"))
	client.WithStdin(bufIn)(cmd)
	assert.Equal(t, bufIn, cmd.Stdin)

	client.WithEnv([]string{"FOO=bar"})(cmd)
	assert.Equal(t, []string{"FOO=bar"}, cmd.Env)
}
