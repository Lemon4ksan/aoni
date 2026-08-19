// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"io"
	"net"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/lemon4ksan/aoni/tunnel/ssh/agent"
)

// Option configures an SSH Client and its underlying ssh.ClientConfig.
type Option func(*Client, *ssh.ClientConfig) error

// CmdOption configures an SSH Cmd instance prior to execution.
type CmdOption func(*Cmd)

// WithDialer routes the underlying SSH connection through a custom ContextDialer transport.
func WithDialer(dialer ContextDialer) Option {
	return func(c *Client, _ *ssh.ClientConfig) error {
		c.Dialer = dialer
		return nil
	}
}

// WithPassword configures password authentication.
func WithPassword(password string) Option {
	return func(_ *Client, config *ssh.ClientConfig) error {
		config.Auth = append(config.Auth, ssh.Password(password))
		return nil
	}
}

// WithKeyboardInteractive configures interactive prompt-response authentication.
func WithKeyboardInteractive(handler func(user, instruction, question string, echo bool) (string, error)) Option {
	return func(_ *Client, config *ssh.ClientConfig) error {
		config.Auth = append(config.Auth, ssh.KeyboardInteractive(
			func(user, instruction string, questions []string, echos []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range questions {
					answer, err := handler(user, instruction, questions[i], echos[i])
					if err != nil {
						return nil, err
					}

					answers[i] = answer
				}

				return answers, nil
			},
		))

		return nil
	}
}

// WithKeyFile configures public key authentication from a private key file path.
func WithKeyFile(keyFile, passphrase string) Option {
	return func(_ *Client, config *ssh.ClientConfig) error {
		signer, err := ParseKeyFile(keyFile, passphrase)
		if err != nil {
			return err
		}

		config.Auth = append(config.Auth, ssh.PublicKeys(signer))

		return nil
	}
}

// WithKey configures public key authentication from raw PEM encoded key bytes.
func WithKey(pemBytes []byte, passphrase string) Option {
	return func(_ *Client, config *ssh.ClientConfig) error {
		signer, err := ParseKey(pemBytes, passphrase)
		if err != nil {
			return err
		}

		config.Auth = append(config.Auth, ssh.PublicKeys(signer))

		return nil
	}
}

// WithAgent configures SSH agent authentication over a connected net.Conn.
func WithAgent(conn net.Conn) Option {
	return func(_ *Client, config *ssh.ClientConfig) error {
		if auth := agent.AuthMethod(conn); auth != nil {
			config.Auth = append(config.Auth, auth)
		}

		return nil
	}
}

// WithAgentSocket configures SSH agent authentication over a Unix socket path.
func WithAgentSocket(socket string) Option {
	return func(_ *Client, config *ssh.ClientConfig) error {
		if auth := agent.AuthMethodSocket(socket); auth != nil {
			config.Auth = append(config.Auth, auth)
		}

		return nil
	}
}

// WithDefaultAgent configures SSH agent authentication using SSH_AUTH_SOCK.
func WithDefaultAgent() Option {
	return func(_ *Client, config *ssh.ClientConfig) error {
		if auth := agent.DefaultAuthMethod(); auth != nil {
			config.Auth = append(config.Auth, auth)
		}

		return nil
	}
}

// WithAuth appends a custom ssh.AuthMethod to authentication options.
func WithAuth(method ssh.AuthMethod) Option {
	return func(_ *Client, config *ssh.ClientConfig) error {
		config.Auth = append(config.Auth, method)
		return nil
	}
}

// WithSigner appends public key authentication from an ssh.Signer.
func WithSigner(signer ssh.Signer) Option {
	return func(_ *Client, config *ssh.ClientConfig) error {
		config.Auth = append(config.Auth, ssh.PublicKeys(signer))
		return nil
	}
}

// WithPort sets the target SSH port.
func WithPort(port uint) Option {
	return func(c *Client, _ *ssh.ClientConfig) error {
		c.Port = port
		return nil
	}
}

// WithTimeout sets the SSH handshake connection timeout.
func WithTimeout(d time.Duration) Option {
	return func(_ *Client, config *ssh.ClientConfig) error {
		config.Timeout = d
		return nil
	}
}

// WithWindowSize sets the SSH channel initial window size in bytes.
func WithWindowSize(size uint32) Option {
	return func(c *Client, _ *ssh.ClientConfig) error {
		c.WindowSize = size
		return nil
	}
}

// WithMaxPacketSize sets the maximum SSH packet size in bytes.
func WithMaxPacketSize(size uint32) Option {
	return func(c *Client, _ *ssh.ClientConfig) error {
		c.MaxPacketSize = size
		return nil
	}
}

// WithHighPerformanceDefaults configures AEAD ciphers, fast KEX curves, and large channel window scaling.
func WithHighPerformanceDefaults() Option {
	return func(c *Client, config *ssh.ClientConfig) error {
		c.WindowSize = 16 * 1024 * 1024
		c.MaxPacketSize = 64 * 1024

		config.Ciphers = []string{
			"aes128-gcm@openssh.com",
			"chacha20-poly1305@openssh.com",
			"aes256-gcm@openssh.com",
		}
		config.KeyExchanges = []string{
			"curve25519-sha256",
			"curve25519-sha256@libssh.org",
			"ecdh-sha2-nistp256",
		}

		return nil
	}
}

// WithKnownHosts configures host key verification using path.
func WithKnownHosts(path string) Option {
	return func(_ *Client, config *ssh.ClientConfig) error {
		cb, err := KnownHosts(path)
		if err != nil {
			return err
		}

		config.HostKeyCallback = cb

		return nil
	}
}

// WithEnsureKnownHosts configures host key verification using path, creating the file if missing.
func WithEnsureKnownHosts(path string) Option {
	return func(_ *Client, config *ssh.ClientConfig) error {
		cb, err := EnsureKnownHosts(path)
		if err != nil {
			return err
		}

		config.HostKeyCallback = cb

		return nil
	}
}

// WithFingerprint verifies host key against an explicit SHA256 fingerprint string without requiring known_hosts.
func WithFingerprint(fingerprint string) Option {
	return func(_ *Client, config *ssh.ClientConfig) error {
		config.HostKeyCallback = func(_ string, _ net.Addr, publicKey ssh.PublicKey) error {
			if ssh.FingerprintSHA256(publicKey) != fingerprint {
				return ErrFingerprintMismatch
			}

			return nil
		}

		return nil
	}
}

// WithLegacyCiphers enables legacy ciphers and KEX algorithms.
func WithLegacyCiphers() Option {
	return func(_ *Client, config *ssh.ClientConfig) error {
		config.Ciphers = append(config.Ciphers,
			"aes128-cbc", "aes192-cbc", "aes256-cbc", "3des-cbc",
		)
		config.KeyExchanges = append(config.KeyExchanges,
			"diffie-hellman-group-exchange-sha1",
			"diffie-hellman-group-exchange-sha256",
		)

		return nil
	}
}

// WithCiphers overrides or appends accepted SSH ciphers.
func WithCiphers(ciphers []string) Option {
	return func(_ *Client, config *ssh.ClientConfig) error {
		config.Ciphers = append(config.Ciphers, ciphers...)
		return nil
	}
}

// WithKeyExchanges overrides or appends accepted Key Exchange algorithms.
func WithKeyExchanges(kex []string) Option {
	return func(_ *Client, config *ssh.ClientConfig) error {
		config.KeyExchanges = append(config.KeyExchanges, kex...)
		return nil
	}
}

// WithRequestPty enables pseudo-terminal (PTY) allocation on sessions created by this client.
func WithRequestPty(enabled bool) Option {
	return func(c *Client, _ *ssh.ClientConfig) error {
		c.RequestPty = enabled
		return nil
	}
}

// WithPtyTerminal configures pseudo-terminal dimensions and terminal type.
func WithPtyTerminal(term string, width, height int) Option {
	return func(c *Client, _ *ssh.ClientConfig) error {
		c.RequestPty = true
		c.PtyTerm = term
		c.PtyWidth = width
		c.PtyHeight = height

		return nil
	}
}

// WithInsecureIgnoreHostKey disables host key verification.
func WithInsecureIgnoreHostKey() Option {
	return func(_ *Client, config *ssh.ClientConfig) error {
		config.HostKeyCallback = ssh.InsecureIgnoreHostKey() //nolint:gosec
		return nil
	}
}

// WithHostKeyCallback sets a custom host key callback function.
func WithHostKeyCallback(cb ssh.HostKeyCallback) Option {
	return func(_ *Client, config *ssh.ClientConfig) error {
		config.HostKeyCallback = cb
		return nil
	}
}

// WithHostKeyAlgorithms sets custom accepted host key algorithms.
func WithHostKeyAlgorithms(algorithms []string) Option {
	return func(_ *Client, config *ssh.ClientConfig) error {
		config.HostKeyAlgorithms = algorithms
		return nil
	}
}

// WithBannerCallback sets a banner message callback function.
func WithBannerCallback(cb ssh.BannerCallback) Option {
	return func(_ *Client, config *ssh.ClientConfig) error {
		config.BannerCallback = cb
		return nil
	}
}

// WithProxy configures a SOCKS5 proxy URL for routing the SSH connection.
func WithProxy(socks5URL string) Option {
	return func(c *Client, _ *ssh.ClientConfig) error {
		c.ProxyURL = socks5URL
		return nil
	}
}

// WithJump configures a pre-connected jump client to tunnel through.
func WithJump(jumpClient *Client) Option {
	return func(c *Client, _ *ssh.ClientConfig) error {
		c.Jump = jumpClient
		return nil
	}
}

// WithConfig applies a custom modification callback to the underlying ssh.ClientConfig.
func WithConfig(fn func(*ssh.ClientConfig) error) Option {
	return func(_ *Client, config *ssh.ClientConfig) error {
		return fn(config)
	}
}

// WithPath sets the command executable path for Cmd.
func WithPath(path string) CmdOption {
	return func(c *Cmd) {
		c.Path = path
	}
}

// WithStdout assigns a writer for remote command stdout.
func WithStdout(w io.Writer) CmdOption {
	return func(c *Cmd) {
		c.Stdout = w
	}
}

// WithStderr assigns a writer for remote command stderr.
func WithStderr(w io.Writer) CmdOption {
	return func(c *Cmd) {
		c.Stderr = w
	}
}

// WithStdin assigns a reader for remote command stdin.
func WithStdin(r io.Reader) CmdOption {
	return func(c *Cmd) {
		c.Stdin = r
	}
}

// WithEnv sets environment variables for the remote command session.
func WithEnv(env []string) CmdOption {
	return func(c *Cmd) {
		c.Env = env
	}
}

// WithCertFile configures public key authentication using an OpenSSH certificate and private key file.
func WithCertFile(certFile, keyFile, passphrase string) Option {
	return func(_ *Client, config *ssh.ClientConfig) error {
		signer, err := ParseCertKeyFile(certFile, keyFile, passphrase)
		if err != nil {
			return err
		}

		config.Auth = append(config.Auth, ssh.PublicKeys(signer))

		return nil
	}
}

// WithCert configures public key authentication using raw OpenSSH certificate and private key PEM bytes.
func WithCert(certBytes, keyBytes []byte, passphrase string) Option {
	return func(_ *Client, config *ssh.ClientConfig) error {
		signer, err := ParseCertKey(certBytes, keyBytes, passphrase)
		if err != nil {
			return err
		}

		config.Auth = append(config.Auth, ssh.PublicKeys(signer))

		return nil
	}
}

// WithCertSigner appends authentication using an existing certificate-backed ssh.Signer.
func WithCertSigner(signer ssh.Signer) Option {
	return func(_ *Client, config *ssh.ClientConfig) error {
		config.Auth = append(config.Auth, ssh.PublicKeys(signer))
		return nil
	}
}
