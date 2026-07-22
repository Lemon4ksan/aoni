// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license can be found in the LICENSE file.

// Package cert provides dynamic TLS certificate file watching, auto-reloading, and cert utilities.
package cert

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"sync"
	"time"
)

// Watcher periodically polls disk mtime for TLS certificate and key files,
// reloading them dynamically without requiring application restarts.
type Watcher struct {
	mu          sync.RWMutex
	certPath    string
	keyPath     string
	cert        *tls.Certificate
	lastModTime time.Time
	cancel      context.CancelFunc
}

// NewWatcher creates a new [Watcher] that monitors certPath and keyPath.
// Initial certificate loading is performed synchronously. If interval > 0,
// a background watcher goroutine automatically checks for file modifications.
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

// GetCertificate returns the current active [tls.Certificate] for use in tls.Config.
func (w *Watcher) GetCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.cert, nil
}

// GetClientCertificate returns the current active client [tls.Certificate] for mTLS.
func (w *Watcher) GetClientCertificate(_ *tls.CertificateRequestInfo) (*tls.Certificate, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.cert, nil
}

// Close stops the background cert watcher loop.
func (w *Watcher) Close() {
	if w.cancel != nil {
		w.cancel()
	}
}

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

func (w *Watcher) checkAndReload() {
	info, err := os.Stat(w.certPath)
	if err != nil {
		return
	}

	if info.ModTime().After(w.lastModTime) {
		_ = w.reload()
	}
}

func (w *Watcher) reload() error {
	cert, err := tls.LoadX509KeyPair(w.certPath, w.keyPath)
	if err != nil {
		return fmt.Errorf("aoni: cert watcher: failed to load keypair: %w", err)
	}

	info, err := os.Stat(w.certPath)
	if err == nil {
		w.lastModTime = info.ModTime()
	}

	w.mu.Lock()
	w.cert = &cert
	w.mu.Unlock()

	return nil
}
