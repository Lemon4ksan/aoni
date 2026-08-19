// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	golangssh "golang.org/x/crypto/ssh"

	"github.com/lemon4ksan/aoni/tunnel/ssh/ca"
	"github.com/lemon4ksan/aoni/tunnel/ssh/server"
)

func generateTestHostKey(t *testing.T) golangssh.Signer {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	signer, err := golangssh.NewSignerFromKey(priv)
	require.NoError(t, err)

	return signer
}

func generateTestUserKeyPair(t *testing.T) (golangssh.Signer, golangssh.PublicKey) {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	signer, err := golangssh.NewSignerFromKey(priv)
	require.NoError(t, err)

	sshPubKey, err := golangssh.NewPublicKey(pub)
	require.NoError(t, err)

	return signer, sshPubKey
}

func TestServer_BasicExec(t *testing.T) {
	t.Parallel()

	signer := generateTestHostKey(t)

	handler := func(s server.Session) {
		cmd := s.RawCommand()
		if cmd == "echo hello" {
			_, _ = io.WriteString(s, "hello\n")
			_ = s.Exit(0)

			return
		}

		_, _ = io.WriteString(s, "ok\n")
		_ = s.Exit(0)
	}

	srv, err := server.New(
		"127.0.0.1:0",
		handler,
		server.WithHostKeySigner(signer),
		server.WithPasswordAuth(func(ctx server.Context, password string) bool {
			return ctx.User() == "testuser" && password == "secretpass"
		}),
	)
	require.NoError(t, err)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() {
		_ = srv.Serve(listener)
	}()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_ = srv.Shutdown(ctx)
	})

	cfg := &golangssh.ClientConfig{
		User: "testuser",
		Auth: []golangssh.AuthMethod{
			golangssh.Password("secretpass"),
		},
		HostKeyCallback: golangssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	t.Run("successful client connection and command execution", func(t *testing.T) {
		t.Parallel()

		c, err := golangssh.Dial("tcp", listener.Addr().String(), cfg)
		require.NoError(t, err)

		defer c.Close()

		sess, err := c.NewSession()
		require.NoError(t, err)

		defer sess.Close()

		out, err := sess.CombinedOutput("echo hello")
		require.NoError(t, err)
		assert.Equal(t, "hello\n", string(out))
	})

	t.Run("invalid password authentication fails", func(t *testing.T) {
		t.Parallel()

		badCfg := &golangssh.ClientConfig{
			User: "testuser",
			Auth: []golangssh.AuthMethod{
				golangssh.Password("wrongpass"),
			},
			HostKeyCallback: golangssh.InsecureIgnoreHostKey(),
			Timeout:         2 * time.Second,
		}

		c, err := golangssh.Dial("tcp", listener.Addr().String(), badCfg)
		require.Error(t, err)
		assert.Nil(t, c)
	})
}

func TestServer_PublicKeyAndCertificateAuth(t *testing.T) {
	t.Parallel()

	hostSigner := generateTestHostKey(t)
	userSigner, userPubKey := generateTestUserKeyPair(t)

	// Создаем Certificate Authority для тестов
	caObj, _, err := ca.GenerateCA()
	require.NoError(t, err)

	handler := func(s server.Session) {
		_, _ = io.WriteString(s, "auth_ok\n")
		_ = s.Exit(0)
	}

	srv, err := server.New(
		"127.0.0.1:0",
		handler,
		server.WithHostKeySigner(hostSigner),
		server.WithPublicKeyAuth(func(ctx server.Context, key golangssh.PublicKey) bool {
			return bytes.Equal(key.Marshal(), userPubKey.Marshal())
		}),
		server.WithUserCAKeys(caObj.PublicKey()),
	)
	require.NoError(t, err)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() {
		_ = srv.Serve(listener)
	}()

	t.Cleanup(func() { _ = srv.Shutdown(t.Context()) })

	t.Run("public_key_authentication_success", func(t *testing.T) {
		t.Parallel()

		cfg := &golangssh.ClientConfig{
			User:            "alice",
			Auth:            []golangssh.AuthMethod{golangssh.PublicKeys(userSigner)},
			HostKeyCallback: golangssh.InsecureIgnoreHostKey(),
			Timeout:         5 * time.Second,
		}

		c, err := golangssh.Dial("tcp", listener.Addr().String(), cfg)
		require.NoError(t, err)

		defer c.Close()

		sess, err := c.NewSession()
		require.NoError(t, err)

		defer sess.Close()

		out, err := sess.CombinedOutput("test")
		require.NoError(t, err)
		assert.Equal(t, "auth_ok\n", string(out))
	})

	t.Run("ssh_certificate_authentication_success", func(t *testing.T) {
		t.Parallel()

		cert, err := caObj.IssueUserCert(userPubKey, "alice")
		require.NoError(t, err)

		certSigner, err := golangssh.NewCertSigner(cert, userSigner)
		require.NoError(t, err)

		cfg := &golangssh.ClientConfig{
			User:            "alice",
			Auth:            []golangssh.AuthMethod{golangssh.PublicKeys(certSigner)},
			HostKeyCallback: golangssh.InsecureIgnoreHostKey(),
			Timeout:         5 * time.Second,
		}

		c, err := golangssh.Dial("tcp", listener.Addr().String(), cfg)
		require.NoError(t, err)

		defer c.Close()

		sess, err := c.NewSession()
		require.NoError(t, err)

		defer sess.Close()

		out, err := sess.CombinedOutput("test")
		require.NoError(t, err)
		assert.Equal(t, "auth_ok\n", string(out))
	})
}

func TestServer_SessionEnvironAndPty(t *testing.T) {
	t.Parallel()

	hostSigner := generateTestHostKey(t)

	handler := func(s server.Session) {
		pty, hasPty := s.Pty()
		if hasPty {
			_, _ = io.WriteString(s, "pty:"+pty.Term+"\n")
		}

		for _, env := range s.Environ() {
			_, _ = io.WriteString(s, env+"\n")
		}

		_ = s.Exit(0)
	}

	srv, err := server.New(
		"127.0.0.1:0",
		handler,
		server.WithHostKeySigner(hostSigner),
		server.WithPasswordAuth(func(_ server.Context, _ string) bool { return true }),
	)
	require.NoError(t, err)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() {
		_ = srv.Serve(listener)
	}()

	t.Cleanup(func() { _ = srv.Shutdown(t.Context()) })

	cfg := &golangssh.ClientConfig{
		User:            "test",
		Auth:            []golangssh.AuthMethod{golangssh.Password("pass")},
		HostKeyCallback: golangssh.InsecureIgnoreHostKey(),
	}

	c, err := golangssh.Dial("tcp", listener.Addr().String(), cfg)
	require.NoError(t, err)

	defer c.Close()

	sess, err := c.NewSession()
	require.NoError(t, err)

	defer sess.Close()

	modes := golangssh.TerminalModes{golangssh.ECHO: 0}
	require.NoError(t, sess.RequestPty("xterm-256color", 80, 24, modes))
	require.NoError(t, sess.Setenv("APP_ENV", "testing"))

	out, err := sess.CombinedOutput("run")
	require.NoError(t, err)

	outStr := string(out)
	assert.Contains(t, outStr, "pty:xterm-256color")
	assert.Contains(t, outStr, "APP_ENV=testing")
}

func TestServer_Subsystems(t *testing.T) {
	t.Parallel()

	hostSigner := generateTestHostKey(t)

	srv, err := server.New(
		"127.0.0.1:0",
		nil,
		server.WithHostKeySigner(hostSigner),
		server.WithPasswordAuth(func(_ server.Context, _ string) bool { return true }),
		server.WithSubsystem("custom-subsys", func(s server.Session) {
			_, _ = io.WriteString(s, "subsystem_active\n")
			_ = s.Exit(0)
		}),
	)
	require.NoError(t, err)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() {
		_ = srv.Serve(listener)
	}()

	t.Cleanup(func() { _ = srv.Shutdown(t.Context()) })

	cfg := &golangssh.ClientConfig{
		User:            "test",
		Auth:            []golangssh.AuthMethod{golangssh.Password("pass")},
		HostKeyCallback: golangssh.InsecureIgnoreHostKey(),
	}

	c, err := golangssh.Dial("tcp", listener.Addr().String(), cfg)
	require.NoError(t, err)

	defer c.Close()

	sess, err := c.NewSession()
	require.NoError(t, err)

	defer sess.Close()

	outPipe, err := sess.StdoutPipe()
	require.NoError(t, err)

	require.NoError(t, sess.RequestSubsystem("custom-subsys"))

	out, err := io.ReadAll(outPipe)
	require.NoError(t, err)
	assert.Equal(t, "subsystem_active\n", string(out))
}
