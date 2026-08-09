// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/http2"

	"github.com/lemon4ksan/aoni/internal/experimental"
)

// Engine is the central runtime execution engine orchestrator.
type Engine struct {
	Prepared   PreparedConfig
	Janitor    *Janitor
	Dispatcher *Dispatcher
	AltSvc     *AltSvcCache
	Referer    *RefererAutomaton
	BufferPool *BufferPool
	Features   experimental.Features
}

// NewEngine constructs a central execution [Engine].
func NewEngine(
	baseURL *url.URL,
	staticHeaders http.Header,
	evictor IdleEvictor,
	evictInterval time.Duration,
	maxInflight int,
) *Engine {
	altSvc := NewAltSvcCache()

	return &Engine{
		Prepared:   NewPreparedConfig(baseURL, staticHeaders),
		Janitor:    NewJanitor(evictor, evictInterval, maxInflight),
		Dispatcher: NewDispatcher(altSvc),
		AltSvc:     altSvc,
		Referer:    NewRefererAutomaton(PolicyStrictOriginWhenCrossOrigin),
		BufferPool: GlobalBufferPool,
		Features:   experimental.InspectFeatures(),
	}
}

// NewCycle starts a transaction execution cycle tied to this engine.
func (e *Engine) NewCycle(
	parentCtx context.Context,
	timeout time.Duration,
	maxAttempts, maxRedirects int,
) (*ExecutionCycle, context.Context) {
	return NewExecutionCycle(parentCtx, timeout, maxAttempts, maxRedirects)
}

// Close shuts down background janitors and releases resources.
func (e *Engine) Close() {
	if e == nil {
		return
	}

	if e.Janitor != nil {
		e.Janitor.Stop()
	}
}

type ConnectionPoolConfigDTO struct {
	MaxIdleConns          int
	MaxIdleConnsPerHost   int
	MaxConnsPerHost       int
	IdleConnTimeout       time.Duration
	ResponseHeaderTimeout time.Duration
	ReadBufferSize        int
	WriteBufferSize       int
}

type HTTP2ConfigDTO struct {
	ReadIdleTimeout time.Duration
	PingTimeout     time.Duration
	AllowHTTP       bool
}

// ApplyTransportOverrides applies pool limits, TLS skip verification, and HTTP/2 timeouts to an http.Transport.
func ApplyTransportOverrides(tr *http.Transport, insecure bool, pool *ConnectionPoolConfigDTO, h2Cfg *HTTP2ConfigDTO) {
	if tr == nil {
		return
	}

	if insecure {
		if tr.TLSClientConfig == nil {
			tr.TLSClientConfig = &tls.Config{}
		}

		tr.TLSClientConfig.InsecureSkipVerify = true
	}

	if pool != nil {
		if pool.MaxIdleConns > 0 {
			tr.MaxIdleConns = pool.MaxIdleConns
		}

		if pool.MaxIdleConnsPerHost > 0 {
			tr.MaxIdleConnsPerHost = pool.MaxIdleConnsPerHost
		}

		if pool.MaxConnsPerHost > 0 {
			tr.MaxConnsPerHost = pool.MaxConnsPerHost
		}

		if pool.IdleConnTimeout > 0 {
			tr.IdleConnTimeout = pool.IdleConnTimeout
		}

		if pool.ResponseHeaderTimeout > 0 {
			tr.ResponseHeaderTimeout = pool.ResponseHeaderTimeout
		}

		if pool.ReadBufferSize > 0 {
			tr.ReadBufferSize = pool.ReadBufferSize
		}

		if pool.WriteBufferSize > 0 {
			tr.WriteBufferSize = pool.WriteBufferSize
		}
	}

	if h2Cfg != nil {
		if t2, err := http2.ConfigureTransports(tr); err == nil && t2 != nil {
			t2.ReadIdleTimeout = h2Cfg.ReadIdleTimeout
			t2.PingTimeout = h2Cfg.PingTimeout
			t2.AllowHTTP = h2Cfg.AllowHTTP
		}
	}
}

// TLSConfigWithOverride clones base and applies per-request TLS settings.
func TLSConfigWithOverride(base *tls.Config, insecure bool) *tls.Config {
	if !insecure {
		return base
	}

	var cloned *tls.Config
	if base != nil {
		cloned = base.Clone()
	} else {
		cloned = &tls.Config{}
	}

	cloned.InsecureSkipVerify = true

	return cloned
}
