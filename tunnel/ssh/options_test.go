// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ssh_test

import (
	"github.com/lemon4ksan/aoni/tunnel/ssh"

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
)

func TestOptions(t *testing.T) {
	t.Run("WithPort", func(t *testing.T) {
		c := &ssh.Client{}
		cfg := &golangssh.ClientConfig{}

		opt := ssh.WithPort(2222)
		err := opt(c, cfg)
		require.NoError(t, err)
		assert.Equal(t, uint(2222), c.Port)
	})

	t.Run("WithPassword", func(t *testing.T) {
		c := &ssh.Client{}
		cfg := &golangssh.ClientConfig{}

		opt := ssh.WithPassword("supersecret")
		err := opt(c, cfg)
		require.NoError(t, err)
		assert.Len(t, cfg.Auth, 1)
	})

	t.Run("WithKeyboardInteractive", func(t *testing.T) {
		c := &ssh.Client{}
		cfg := &golangssh.ClientConfig{}

		handler := func(_, _, _ string, _ bool) (string, error) {
			return "ans", nil
		}

		opt := ssh.WithKeyboardInteractive(handler)
		err := opt(c, cfg)
		require.NoError(t, err)
		assert.Len(t, cfg.Auth, 1)
	})

	t.Run("WithKey and WithSigner", func(t *testing.T) {
		pemBytes, signer, _ := generateTestKeyPair(t)

		t.Run("WithKey valid", func(t *testing.T) {
			c := &ssh.Client{}
			cfg := &golangssh.ClientConfig{}

			err := ssh.WithKey(pemBytes, "")(c, cfg)
			require.NoError(t, err)
			assert.Len(t, cfg.Auth, 1)
		})

		t.Run("WithKey invalid", func(t *testing.T) {
			c := &ssh.Client{}
			cfg := &golangssh.ClientConfig{}

			err := ssh.WithKey([]byte("bad pem"), "")(c, cfg)
			assert.Error(t, err)
		})

		t.Run("WithSigner", func(t *testing.T) {
			c := &ssh.Client{}
			cfg := &golangssh.ClientConfig{}

			err := ssh.WithSigner(signer)(c, cfg)
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

		c := &ssh.Client{}
		cfg := &golangssh.ClientConfig{}

		err = ssh.WithCertSigner(certSigner)(c, cfg)
		require.NoError(t, err)
		assert.Len(t, cfg.Auth, 1)
	})

	t.Run("WithKeyFile", func(t *testing.T) {
		tempDir := t.TempDir()
		keyFile := filepath.Join(tempDir, "id_ed25519")

		pemBytes, _, _ := generateTestKeyPair(t)
		require.NoError(t, os.WriteFile(keyFile, pemBytes, 0o600))

		c := &ssh.Client{}
		cfg := &golangssh.ClientConfig{}

		err := ssh.WithKeyFile(keyFile, "")(c, cfg)
		require.NoError(t, err)
		assert.Len(t, cfg.Auth, 1)

		err = ssh.WithKeyFile(filepath.Join(tempDir, "missing"), "")(c, cfg)
		assert.Error(t, err)
	})

	t.Run("WithAgent variants", func(t *testing.T) {
		t.Run("WithAgent nil conn", func(t *testing.T) {
			c := &ssh.Client{}
			cfg := &golangssh.ClientConfig{}

			err := ssh.WithAgent(nil)(c, cfg)
			require.NoError(t, err)
			assert.Empty(t, cfg.Auth)
		})

		t.Run("WithAgentSocket empty", func(t *testing.T) {
			c := &ssh.Client{}
			cfg := &golangssh.ClientConfig{}

			err := ssh.WithAgentSocket("")(c, cfg)
			require.NoError(t, err)
			assert.Empty(t, cfg.Auth)
		})

		t.Run("WithDefaultAgent", func(t *testing.T) {
			c := &ssh.Client{}
			cfg := &golangssh.ClientConfig{}

			t.Setenv("SSH_AUTH_SOCK", "")

			err := ssh.WithDefaultAgent()(c, cfg)
			require.NoError(t, err)
		})
	})

	t.Run("WithTimeout", func(t *testing.T) {
		c := &ssh.Client{}
		cfg := &golangssh.ClientConfig{}

		err := ssh.WithTimeout(10*time.Second)(c, cfg)
		require.NoError(t, err)
		assert.Equal(t, 10*time.Second, cfg.Timeout)
	})

	t.Run("WithWindowSize and WithMaxPacketSize", func(t *testing.T) {
		c := &ssh.Client{}
		cfg := &golangssh.ClientConfig{}

		require.NoError(t, ssh.WithWindowSize(8192)(c, cfg))
		assert.Equal(t, uint32(8192), c.WindowSize)

		require.NoError(t, ssh.WithMaxPacketSize(4096)(c, cfg))
		assert.Equal(t, uint32(4096), c.MaxPacketSize)
	})

	t.Run("WithHighPerformanceDefaults", func(t *testing.T) {
		c := &ssh.Client{}
		cfg := &golangssh.ClientConfig{}

		err := ssh.WithHighPerformanceDefaults()(c, cfg)
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

		c := &ssh.Client{}
		cfg := &golangssh.ClientConfig{}

		err := ssh.WithKnownHosts(knownFile)(c, cfg)
		require.NoError(t, err)
		assert.NotNil(t, cfg.HostKeyCallback)

		ensureFile := filepath.Join(tempDir, "new_dir", "known_hosts")
		err = ssh.WithEnsureKnownHosts(ensureFile)(c, cfg)
		require.NoError(t, err)
		assert.NotNil(t, cfg.HostKeyCallback)
	})

	t.Run("WithFingerprint", func(t *testing.T) {
		_, _, pubKey := generateTestKeyPair(t)
		fp := golangssh.FingerprintSHA256(pubKey)

		c := &ssh.Client{}
		cfg := &golangssh.ClientConfig{}

		err := ssh.WithFingerprint(fp)(c, cfg)
		require.NoError(t, err)
		require.NotNil(t, cfg.HostKeyCallback)

		dummyAddr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}
		require.NoError(t, cfg.HostKeyCallback("127.0.0.1", dummyAddr, pubKey))

		_, _, otherPubKey := generateTestKeyPair(t)
		err = cfg.HostKeyCallback("127.0.0.1", dummyAddr, otherPubKey)
		assert.ErrorIs(t, err, ssh.ErrFingerprintMismatch)
	})

	t.Run("WithCiphers, WithKeyExchanges, WithLegacyCiphers", func(t *testing.T) {
		c := &ssh.Client{}
		cfg := &golangssh.ClientConfig{}

		require.NoError(t, ssh.WithCiphers([]string{"my-cipher"})(c, cfg))
		assert.Contains(t, cfg.Ciphers, "my-cipher")

		require.NoError(t, ssh.WithKeyExchanges([]string{"my-kex"})(c, cfg))
		assert.Contains(t, cfg.KeyExchanges, "my-kex")

		require.NoError(t, ssh.WithLegacyCiphers()(c, cfg))
		assert.Contains(t, cfg.Ciphers, "aes128-cbc")
	})

	t.Run("WithRequestPty and WithPtyTerminal", func(t *testing.T) {
		c := &ssh.Client{}
		cfg := &golangssh.ClientConfig{}

		require.NoError(t, ssh.WithRequestPty(true)(c, cfg))
		assert.True(t, c.RequestPty)

		require.NoError(t, ssh.WithPtyTerminal("vt100", 120, 50)(c, cfg))
		assert.True(t, c.RequestPty)
		assert.Equal(t, "vt100", c.PtyTerm)
		assert.Equal(t, 120, c.PtyWidth)
		assert.Equal(t, 50, c.PtyHeight)
	})

	t.Run(
		"WithInsecureIgnoreHostKey, WithHostKeyCallback, WithHostKeyAlgorithms, WithBannerCallback",
		func(t *testing.T) {
			c := &ssh.Client{}
			cfg := &golangssh.ClientConfig{}

			require.NoError(t, ssh.WithInsecureIgnoreHostKey()(c, cfg))
			assert.NotNil(t, cfg.HostKeyCallback)

			cb := func(_ string, _ net.Addr, _ golangssh.PublicKey) error { return nil }
			require.NoError(t, ssh.WithHostKeyCallback(cb)(c, cfg))

			require.NoError(t, ssh.WithHostKeyAlgorithms([]string{"ssh-ed25519"})(c, cfg))
			assert.Equal(t, []string{"ssh-ed25519"}, cfg.HostKeyAlgorithms)

			bannerCb := func(_ string) error { return nil }
			require.NoError(t, ssh.WithBannerCallback(bannerCb)(c, cfg))
			assert.NotNil(t, cfg.BannerCallback)
		},
	)

	t.Run("WithProxy and WithJump", func(t *testing.T) {
		c := &ssh.Client{}
		cfg := &golangssh.ClientConfig{}

		require.NoError(t, ssh.WithProxy("socks5://127.0.0.1:1080")(c, cfg))
		assert.Equal(t, "socks5://127.0.0.1:1080", c.ProxyURL)

		jumpClient := &ssh.Client{}
		require.NoError(t, ssh.WithJump(jumpClient)(c, cfg))
		assert.Equal(t, jumpClient, c.Jump)
	})

	t.Run("WithConfig", func(t *testing.T) {
		c := &ssh.Client{}
		cfg := &golangssh.ClientConfig{}

		opt := ssh.WithConfig(func(c *golangssh.ClientConfig) error {
			c.User = "custom-user"
			return nil
		})
		require.NoError(t, opt(c, cfg))
		assert.Equal(t, "custom-user", cfg.User)
	})
}

func TestCmdOptions(t *testing.T) {
	t.Parallel()

	cmd := &ssh.Cmd{
		Session: &golangssh.Session{},
	}

	ssh.WithPath("/usr/bin/bash")(cmd)
	assert.Equal(t, "/usr/bin/bash", cmd.Path)

	bufOut := &bytes.Buffer{}
	ssh.WithStdout(bufOut)(cmd)
	assert.Equal(t, bufOut, cmd.Stdout)

	bufErr := &bytes.Buffer{}
	ssh.WithStderr(bufErr)(cmd)
	assert.Equal(t, bufErr, cmd.Stderr)

	bufIn := bytes.NewReader([]byte("input"))
	ssh.WithStdin(bufIn)(cmd)
	assert.Equal(t, bufIn, cmd.Stdin)

	ssh.WithEnv([]string{"FOO=bar"})(cmd)
	assert.Equal(t, []string{"FOO=bar"}, cmd.Env)
}
