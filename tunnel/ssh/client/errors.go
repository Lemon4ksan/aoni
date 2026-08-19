// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import "errors"

var (
	// ErrSSHDialFailed is returned when connecting or authenticating with an SSH server fails.
	ErrSSHDialFailed = errors.New("aoni ssh client: connection or authentication failed")

	// ErrSSHClosed is returned when attempting an operation on an inactive SSH client.
	ErrSSHClosed = errors.New("aoni ssh client: client session is closed")

	// ErrInvalidPrivateKey is returned when parsing an invalid or encrypted PEM private key fails.
	ErrInvalidPrivateKey = errors.New("aoni ssh client: invalid or encrypted private key")

	// ErrInvalidCertificate is returned when parsing or validating an OpenSSH certificate fails.
	ErrInvalidCertificate = errors.New("aoni ssh client: invalid or corrupted ssh certificate")

	// ErrHostKeyMismatch is returned when a host key does not match the entry in known_hosts.
	ErrHostKeyMismatch = errors.New("aoni ssh client: host key mismatch detected")

	// ErrHostNotFound is returned when a host key is missing from the known_hosts file.
	ErrHostNotFound = errors.New("aoni ssh client: host not found in known_hosts")

	// ErrCommandInitFailed is returned when environment variables or session options fail to apply.
	ErrCommandInitFailed = errors.New("aoni ssh client: command initialization failed")

	// ErrProxyAndJumpConflict is returned when attempting to set both a SOCKS5 proxy and an SSH jump host on the same client.
	ErrProxyAndJumpConflict = errors.New("aoni ssh client: cannot combine proxy and jump host on the same client level")

	// ErrFingerprintMismatch is returned when the remote host key fingerprint does not match the expected SHA256 pin.
	ErrFingerprintMismatch = errors.New("aoni ssh client: host key fingerprint mismatch")

	// ErrPtyRequestFailed is returned when the server rejects a pseudo-terminal (PTY) allocation request.
	ErrPtyRequestFailed = errors.New("aoni ssh client: failed to request pseudo-terminal")
)
