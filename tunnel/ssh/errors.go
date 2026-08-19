// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ssh

import (
	"github.com/lemon4ksan/aoni/tunnel/ssh/agent"
	"github.com/lemon4ksan/aoni/tunnel/ssh/client"
	"github.com/lemon4ksan/aoni/tunnel/ssh/server"
	"github.com/lemon4ksan/aoni/tunnel/ssh/sftp"
)

var (
	// ErrSSHDialFailed is returned when connecting or authenticating with an SSH server fails.
	ErrSSHDialFailed = client.ErrSSHDialFailed

	// ErrSSHClosed is returned when attempting an operation on an inactive SSH client.
	ErrSSHClosed = client.ErrSSHClosed

	// ErrInvalidPrivateKey is returned when parsing an invalid or encrypted PEM private key fails.
	ErrInvalidPrivateKey = client.ErrInvalidPrivateKey

	// ErrHostKeyMismatch is returned when a host key does not match the entry in known_hosts.
	ErrHostKeyMismatch = client.ErrHostKeyMismatch

	// ErrHostNotFound is returned when a host key is missing from the known_hosts file.
	ErrHostNotFound = client.ErrHostNotFound

	// ErrCommandInitFailed is returned when environment variables or session options fail to apply.
	ErrCommandInitFailed = client.ErrCommandInitFailed

	// ErrProxyAndJumpConflict is returned when attempting to set both a SOCKS5 proxy and an SSH jump host on the same client.
	ErrProxyAndJumpConflict = client.ErrProxyAndJumpConflict

	// ErrAgentUnavailable is returned when the SSH_AUTH_SOCK environment variable is not configured.
	ErrAgentUnavailable = agent.ErrAgentUnavailable

	// ErrFingerprintMismatch is returned when the remote host key fingerprint does not match the expected SHA256 pin.
	ErrFingerprintMismatch = client.ErrFingerprintMismatch

	// ErrInvalidTargetFile is returned when a target SCP filename contains control characters (\r, \n, \x00).
	ErrInvalidTargetFile = sftp.ErrInvalidTargetFile

	// ErrPtyRequestFailed is returned when the server rejects a pseudo-terminal (PTY) allocation request.
	ErrPtyRequestFailed = client.ErrPtyRequestFailed

	// ErrInvalidChunkSize is returned when parallel SFTP transfer chunkSize is less than or equal to zero.
	ErrInvalidChunkSize = sftp.ErrInvalidChunkSize

	// ErrParallelTransferFailed is returned when one or more chunks fail during parallel SFTP transfer.
	ErrParallelTransferFailed = sftp.ErrParallelTransferFailed

	// ErrServerClosed is returned when operations are performed on a closed SSH server.
	ErrServerClosed = server.ErrServerClosed
)
