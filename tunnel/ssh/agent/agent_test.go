// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package agent_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	golangagent "golang.org/x/crypto/ssh/agent"

	"github.com/lemon4ksan/aoni/tunnel/ssh/agent"
)

func startMockAgentServer(t *testing.T) string {
	t.Helper()

	socketPath := filepath.Join(t.TempDir(), "agent.sock")
	ln, err := net.Listen("unix", socketPath)
	require.NoError(t, err)

	keyring := golangagent.NewKeyring()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	_ = pub

	err = keyring.Add(golangagent.AddedKey{
		PrivateKey: priv,
		Comment:    "test-agent-key",
	})
	require.NoError(t, err)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}

			go golangagent.ServeAgent(keyring, conn)
		}
	}()

	t.Cleanup(func() {
		_ = ln.Close()
	})

	return socketPath
}

func TestAgent_EnvironmentAndUnset(t *testing.T) {
	t.Run("HasAgent and SocketPath", func(t *testing.T) {
		t.Setenv("SSH_AUTH_SOCK", "/tmp/mock-agent.sock")
		assert.True(t, agent.HasAgent())
		assert.Equal(t, "/tmp/mock-agent.sock", agent.SocketPath())

		t.Setenv("SSH_AUTH_SOCK", "")
		assert.False(t, agent.HasAgent())
		assert.Empty(t, agent.SocketPath())
	})

	t.Run("Connect with empty socket returns ErrAgentUnavailable", func(t *testing.T) {
		t.Setenv("SSH_AUTH_SOCK", "")

		ag, conn, err := agent.Connect("")
		require.ErrorIs(t, err, agent.ErrAgentUnavailable)
		assert.Nil(t, ag)
		assert.Nil(t, conn)
	})

	t.Run("AuthMethod nil conn", func(t *testing.T) {
		method := agent.AuthMethod(nil)
		assert.Nil(t, method)
	})

	t.Run("AuthMethodSocket empty", func(t *testing.T) {
		method := agent.AuthMethodSocket("")
		assert.Nil(t, method)
	})

	t.Run("DefaultAuthMethod", func(t *testing.T) {
		t.Setenv("SSH_AUTH_SOCK", "/tmp/non-existent.sock")

		method := agent.DefaultAuthMethod()
		assert.NotNil(t, method)
	})
}

func TestAgent_LiveUnixSocket(t *testing.T) {
	socketPath := startMockAgentServer(t)

	t.Run("successful connection to unix agent socket", func(t *testing.T) {
		ag, conn, err := agent.Connect(socketPath)
		require.NoError(t, err)
		require.NotNil(t, ag)
		require.NotNil(t, conn)

		t.Cleanup(func() { _ = conn.Close() })

		// Проверяем, что агент возвращает зарегистрированный ключ
		signers, err := ag.Signers()
		require.NoError(t, err)
		assert.Len(t, signers, 1)
	})

	t.Run("dial non-existent unix socket returns error", func(t *testing.T) {
		nonExistent := filepath.Join(t.TempDir(), "missing.sock")
		ag, conn, err := agent.Connect(nonExistent)
		require.Error(t, err)
		assert.Nil(t, ag)
		assert.Nil(t, conn)
	})

	t.Run("AuthMethod with live connection", func(t *testing.T) {
		_, conn, err := agent.Connect(socketPath)
		require.NoError(t, err)

		t.Cleanup(func() { _ = conn.Close() })

		authMethod := agent.AuthMethod(conn)
		require.NotNil(t, authMethod)
	})

	t.Run("AuthMethodSocket with live socket path", func(t *testing.T) {
		authMethod := agent.AuthMethodSocket(socketPath)
		require.NotNil(t, authMethod)
	})
}
