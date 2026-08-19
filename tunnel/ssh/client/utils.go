// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type knownHostsCacheEntry struct {
	lastStat time.Time
	modTime  time.Time
	callback ssh.HostKeyCallback
}

const knownHostsStatInterval = 1 * time.Second

var (
	knownHostsMu    sync.RWMutex
	knownHostsCache = make(map[string]knownHostsCacheEntry)
)

// ParseKeyFile loads and parses an ssh.Signer from a private key file path.
func ParseKeyFile(prvFile, passphrase string) (ssh.Signer, error) {
	privateKey, err := os.ReadFile(prvFile)
	if err != nil {
		return nil, err
	}

	return ParseKey(privateKey, passphrase)
}

// ParseKey parses an ssh.Signer from raw PEM encoded private key bytes.
func ParseKey(privateKey []byte, passphrase string) (ssh.Signer, error) {
	if passphrase != "" {
		signer, err := ssh.ParsePrivateKeyWithPassphrase(privateKey, []byte(passphrase))
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidPrivateKey, err)
		}

		return signer, nil
	}

	signer, err := ssh.ParsePrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidPrivateKey, err)
	}

	return signer, nil
}

// DefaultKnownHosts returns a host key callback using the default user known_hosts file path.
func DefaultKnownHosts() (ssh.HostKeyCallback, error) {
	path, err := DefaultKnownHostsPath()
	if err != nil {
		return nil, err
	}

	return EnsureKnownHosts(path)
}

// KnownHosts returns a thread-safe, cached host key callback targeting an existing known_hosts file path.
// Debounces os.Stat calls using a 1-second interval to eliminate disk I/O syscall overhead.
func KnownHosts(file string) (ssh.HostKeyCallback, error) {
	now := time.Now()

	knownHostsMu.RLock()

	entry, exists := knownHostsCache[file]

	knownHostsMu.RUnlock()

	if exists && now.Sub(entry.lastStat) < knownHostsStatInterval {
		return entry.callback, nil
	}

	info, err := os.Stat(file)
	if err != nil {
		if exists {
			return entry.callback, nil
		}

		return nil, err
	}

	if exists && !info.ModTime().After(entry.modTime) {
		knownHostsMu.Lock()
		entry.lastStat = now
		knownHostsCache[file] = entry
		knownHostsMu.Unlock()

		return entry.callback, nil
	}

	cb, err := knownhosts.New(file)
	if err != nil {
		return nil, err
	}

	knownHostsMu.Lock()
	knownHostsCache[file] = knownHostsCacheEntry{
		lastStat: now,
		modTime:  info.ModTime(),
		callback: cb,
	}
	knownHostsMu.Unlock()

	return cb, nil
}

// EnsureKnownHosts returns a host key callback, creating the file and parent directory if missing.
func EnsureKnownHosts(file string) (ssh.HostKeyCallback, error) {
	if _, err := os.Stat(file); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			return nil, err
		}

		f, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return nil, err
		}

		_ = f.Close()
	}

	return KnownHosts(file)
}

// CheckKnownHost verifies if host exists in knownFile using cached host key callbacks.
// Returns (true, nil) if matched, (true, ErrHostKeyMismatch) on key mismatch, or (false, ErrHostNotFound) if absent.
func CheckKnownHost(host string, remote net.Addr, key ssh.PublicKey, knownFile string) (bool, error) {
	targetFile := knownFile
	if targetFile == "" {
		path, err := DefaultKnownHostsPath()
		if err != nil {
			return false, err
		}

		targetFile = path
	}

	callback, err := KnownHosts(targetFile)
	if err != nil {
		return false, err
	}

	err = callback(host, remote, key)
	if err == nil {
		return true, nil
	}

	var keyErr *knownhosts.KeyError
	if errors.As(err, &keyErr) {
		if len(keyErr.Want) > 0 {
			return true, fmt.Errorf("%w: %w", ErrHostKeyMismatch, keyErr)
		}

		return false, ErrHostNotFound
	}

	return false, err
}

// AddKnownHost appends a host key entry to knownFile and invalidates the internal callback cache.
func AddKnownHost(host string, remote net.Addr, key ssh.PublicKey, knownFile string) error {
	targetFile := knownFile
	if targetFile == "" {
		path, err := DefaultKnownHostsPath()
		if err != nil {
			return err
		}

		targetFile = path
	}

	f, err := os.OpenFile(targetFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	remoteNormalized := knownhosts.Normalize(remote.String())
	hostNormalized := knownhosts.Normalize(host)
	addresses := make([]string, 0, 2)
	addresses = append(addresses, remoteNormalized)

	if hostNormalized != remoteNormalized {
		addresses = append(addresses, hostNormalized)
	}

	_, err = f.WriteString(knownhosts.Line(addresses, key) + "\n")
	if err != nil {
		return err
	}

	knownHostsMu.Lock()
	delete(knownHostsCache, targetFile)
	knownHostsMu.Unlock()

	return nil
}

// DefaultKnownHostsPath returns the standard ~/.ssh/known_hosts path.
func DefaultKnownHostsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, ".ssh", "known_hosts"), nil
}

// ParseCertKey parses a private key and an OpenSSH certificate payload,
// returning a cert-backed ssh.Signer ready for SSH client authentication.
func ParseCertKey(certBytes, keyBytes []byte, passphrase string) (ssh.Signer, error) {
	signer, err := ParseKey(keyBytes, passphrase)
	if err != nil {
		return nil, err
	}

	pubKey, _, _, _, err := ssh.ParseAuthorizedKey(certBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: parse cert authorized key: %w", ErrInvalidCertificate, err)
	}

	cert, ok := pubKey.(*ssh.Certificate)
	if !ok {
		return nil, ErrInvalidCertificate
	}

	certSigner, err := ssh.NewCertSigner(cert, signer)
	if err != nil {
		return nil, fmt.Errorf("%w: create cert signer: %w", ErrInvalidCertificate, err)
	}

	return certSigner, nil
}

// ParseCertKeyFile loads and parses a cert-backed ssh.Signer from file paths.
func ParseCertKeyFile(certFile, keyFile, passphrase string) (ssh.Signer, error) {
	certBytes, err := os.ReadFile(certFile)
	if err != nil {
		return nil, err
	}

	keyBytes, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, err
	}

	return ParseCertKey(certBytes, keyBytes, passphrase)
}
