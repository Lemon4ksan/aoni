// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package agent provides SSH agent socket authentication utilities.
package agent

import (
	"errors"
	"fmt"
	"net"
	"os"

	"golang.org/x/crypto/ssh"
	golangagent "golang.org/x/crypto/ssh/agent"
)

// ErrAgentUnavailable is returned when the SSH_AUTH_SOCK environment variable is not configured.
var ErrAgentUnavailable = errors.New("aoni ssh agent: SSH_AUTH_SOCK environment variable is not set")

// HasAgent reports whether an SSH agent socket is available in environment variables.
func HasAgent() bool {
	return SocketPath() != ""
}

// SocketPath returns the SSH_AUTH_SOCK environment variable value.
func SocketPath() string {
	return os.Getenv("SSH_AUTH_SOCK")
}

// Connect connects to the SSH agent unix socket specified by socketPath.
// If socketPath is empty, it uses SocketPath().
func Connect(socketPath string) (golangagent.ExtendedAgent, net.Conn, error) {
	target := socketPath
	if target == "" {
		target = SocketPath()
	}

	if target == "" {
		return nil, nil, ErrAgentUnavailable
	}

	//nolint:gosec
	conn, err := net.Dial("unix", target) //nolint:noctx
	if err != nil {
		return nil, nil, fmt.Errorf("aoni ssh agent: dial unix socket: %w", err)
	}

	return golangagent.NewClient(conn), conn, nil
}

// AuthMethod returns an ssh.AuthMethod that authenticates using an active net.Conn connection to an SSH agent.
func AuthMethod(conn net.Conn) ssh.AuthMethod {
	if conn == nil {
		return nil
	}

	return ssh.PublicKeysCallback(golangagent.NewClient(conn).Signers)
}

// AuthMethodSocket returns an ssh.AuthMethod that lazily dials the specified agent socket path.
func AuthMethodSocket(socketPath string) ssh.AuthMethod {
	if socketPath == "" {
		return nil
	}

	return ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
		//nolint:gosec
		conn, err := net.Dial("unix", socketPath) //nolint:noctx
		if err != nil {
			return nil, nil //nolint:nilerr
		}

		return golangagent.NewClient(conn).Signers()
	})
}

// DefaultAuthMethod returns an ssh.AuthMethod connected to SSH_AUTH_SOCK.
func DefaultAuthMethod() ssh.AuthMethod {
	return AuthMethodSocket(SocketPath())
}
