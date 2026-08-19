// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ca

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/lemon4ksan/aoni/tunnel/ssh/client"
)

// CA represents an enterprise-grade SSH Certificate Authority capable of signing
// short-lived OpenSSH User and Host certificates.
type CA struct {
	Signer ssh.Signer
	serial atomic.Uint64
}

// NewCA instantiates a [CA] wrapping an existing ssh.Signer.
func NewCA(signer ssh.Signer) *CA {
	ca := &CA{Signer: signer}
	ca.serial.Store(uint64(time.Now().UnixNano()))
	return ca
}

// NewCAFromPEM parses a CA private key from PEM bytes.
func NewCAFromPEM(keyPEM []byte, passphrase string) (*CA, error) {
	signer, err := client.ParseKey(keyPEM, passphrase)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidCAKey, err)
	}

	return NewCA(signer), nil
}

// NewCAFromFile loads a CA private key from a disk file path.
func NewCAFromFile(path, passphrase string) (*CA, error) {
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return NewCAFromPEM(pemBytes, passphrase)
}

// GenerateCA creates a new in-memory ED25519 Certificate Authority keypair.
func GenerateCA() (*CA, []byte, error) {
	_, pk, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("aoni ca: generate ed25519 key failed: %w", err)
	}

	pemBlock, err := ssh.MarshalPrivateKey(pk, "")
	if err != nil {
		return nil, nil, fmt.Errorf("aoni ca: marshal private key failed: %w", err)
	}

	pemBytes := pem.EncodeToMemory(pemBlock)

	signer, err := ssh.ParsePrivateKey(pemBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrInvalidCAKey, err)
	}

	return NewCA(signer), pemBytes, nil
}

// PublicKey returns the CA's public key used for server/client verification.
func (c *CA) PublicKey() ssh.PublicKey {
	return c.Signer.PublicKey()
}

// AuthorizedKey returns the CA's public key formatted as an OpenSSH authorized_key payload.
func (c *CA) AuthorizedKey() []byte {
	return ssh.MarshalAuthorizedKey(c.PublicKey())
}

// IssueUserCert signs userKey, generating a short-lived OpenSSH User Certificate.
func (c *CA) IssueUserCert(userKey ssh.PublicKey, username string, opts ...IssueOption) (*ssh.Certificate, error) {
	if userKey == nil {
		return nil, ErrInvalidPublicKey
	}

	cfg := DefaultIssueConfig()
	if username != "" {
		cfg.Principals = []string{username}
	}

	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	now := uint64(time.Now().Unix())

	serial := cfg.Serial
	if serial == 0 {
		serial = c.serial.Add(1)
	}

	keyID := cfg.KeyID
	if keyID == "" {
		keyID = fmt.Sprintf("aoni-user-cert-%s-%d", username, now)
	}

	cert := &ssh.Certificate{
		Key:             userKey,
		Serial:          serial,
		CertType:        ssh.UserCert,
		KeyId:           keyID,
		ValidPrincipals: cfg.Principals,
		ValidAfter:      now - 60,
		ValidBefore:     now + uint64(cfg.TTL.Seconds()),
		Permissions: ssh.Permissions{
			CriticalOptions: cfg.CriticalOptions,
			Extensions:      cfg.Extensions,
		},
	}

	if err := cert.SignCert(rand.Reader, c.Signer); err != nil {
		return nil, fmt.Errorf("aoni ca: sign user cert failed: %w", err)
	}

	return cert, nil
}

// IssueHostCert signs hostKey, generating an OpenSSH Host Certificate for SSH servers.
func (c *CA) IssueHostCert(hostKey ssh.PublicKey, hostname string, opts ...IssueOption) (*ssh.Certificate, error) {
	if hostKey == nil {
		return nil, ErrInvalidPublicKey
	}

	cfg := DefaultIssueConfig()
	cfg.TTL = 365 * 24 * time.Hour
	cfg.Extensions = nil

	if hostname != "" {
		cfg.Principals = []string{hostname}
	}

	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	now := uint64(time.Now().Unix())

	serial := cfg.Serial
	if serial == 0 {
		serial = c.serial.Add(1)
	}

	keyID := cfg.KeyID
	if keyID == "" {
		keyID = fmt.Sprintf("aoni-host-cert-%s-%d", hostname, now)
	}

	cert := &ssh.Certificate{
		Key:             hostKey,
		Serial:          serial,
		CertType:        ssh.HostCert,
		KeyId:           keyID,
		ValidPrincipals: cfg.Principals,
		ValidAfter:      now - 60,
		ValidBefore:     now + uint64(cfg.TTL.Seconds()),
	}

	if err := cert.SignCert(rand.Reader, c.Signer); err != nil {
		return nil, fmt.Errorf("aoni ca: sign host cert failed: %w", err)
	}

	return cert, nil
}

// SaveCAKeys saves the CA private and public keys to disk.
func (c *CA) SaveCAKeys(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	pubPath := filepath.Join(dir, "ca_key.pub")

	return os.WriteFile(pubPath, c.AuthorizedKey(), 0o644)
}
