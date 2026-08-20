// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package cert provides dynamic TLS certificate watching and hot-reloading capabilities.
package cert

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"time"

	"github.com/lemon4ksan/foundation/generic"
	utls "github.com/refraction-networking/utls"
)

// CompressionAlgorithm specifies a certificate compression algorithm defined in RFC 8879.
type CompressionAlgorithm uint16

const (
	// CertCompressionZlib specifies the zlib certificate compression algorithm.
	CertCompressionZlib CompressionAlgorithm = 1
	// CompressionBrotli specifies the Brotli certificate compression algorithm.
	CompressionBrotli CompressionAlgorithm = 2
	// CompressionZstd specifies the Zstandard certificate compression algorithm.
	CompressionZstd CompressionAlgorithm = 3
)

// ToUTLS maps the compression algorithm to its corresponding uTLS representation.
func (a CompressionAlgorithm) ToUTLS() utls.CertCompressionAlgo {
	switch a {
	case CertCompressionZlib:
		return utls.CertCompressionZlib
	case CompressionZstd:
		return utls.CertCompressionZstd
	default:
		return utls.CertCompressionBrotli
	}
}

// Watcher monitors disk modification timestamps for TLS keypairs, updating certificates dynamically.
type Watcher struct {
	cert        generic.Atomic[tls.Certificate]
	certPath    string
	keyPath     string
	lastModTime time.Time
	cancel      context.CancelFunc
}

// NewWatcher creates a new [Watcher] monitoring certPath and keyPath. Initial loading is synchronous.
func NewWatcher(certPath, keyPath string, interval time.Duration) (*Watcher, error) {
	w := &Watcher{
		certPath: certPath,
		keyPath:  keyPath,
	}

	if err := w.reload(); err != nil {
		return nil, err
	}

	if interval > 0 {
		ctx, cancel := context.WithCancel(context.Background())
		w.cancel = cancel

		go w.watchLoop(ctx, interval)
	}

	return w, nil
}

// GetCertificate returns the active [*tls.Certificate] for server handshake configs.
func (w *Watcher) GetCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	return w.cert.LoadPtr(), nil
}

// GetClientCertificate returns the active client [*tls.Certificate] for mTLS handshakes.
func (w *Watcher) GetClientCertificate(_ *tls.CertificateRequestInfo) (*tls.Certificate, error) {
	return w.cert.LoadPtr(), nil
}

// Close terminates the background certificate watcher loop.
func (w *Watcher) Close() {
	if w.cancel != nil {
		w.cancel()
	}
}

// watchLoop runs periodic timer checking certificate files on disk.
func (w *Watcher) watchLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.checkAndReload()
		}
	}
}

// checkAndReload verifies modification time on disk and triggers reload when changed.
func (w *Watcher) checkAndReload() {
	info, err := os.Stat(w.certPath)
	if err != nil {
		return
	}

	if info.ModTime().After(w.lastModTime) {
		_ = w.reload()
	}
}

// reload reads keypair from disk and atomically updates active certificate reference.
func (w *Watcher) reload() error {
	cert, err := tls.LoadX509KeyPair(w.certPath, w.keyPath)
	if err != nil {
		return fmt.Errorf("aoni: cert watcher: failed to load keypair: %w", err)
	}

	info, err := os.Stat(w.certPath)
	if err == nil {
		w.lastModTime = info.ModTime()
	}

	w.cert.Store(cert)

	return nil
}
