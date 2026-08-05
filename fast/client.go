// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package fast provides high-performance fasthttp engine adapters for [aoni.Request] and [aoni.Response].
package fast

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/url"

	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/pipeline"
)

// HTTPDoer executes an HTTP request transaction.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client executes ultra-high-performance HTTP requests over fasthttp,
// seamlessly multiplexing native H1 (fasthttp), native H2 (h2engine), and native H3 (h3engine).
type Client struct {
	engine         *fasthttp.Client
	pipelineEngine *pipeline.Pipeline
	dialer         *fastDialer
	defaultDial    func(string) (net.Conn, error)
	config         aoni.Config

	protocolState protocolState
}

// NewClient creates a new multiprotocol Client configured with fasthttp, uTLS,
// native HTTP/2 framing, and native HTTP/3 QUIC support.
func NewClient(opts ...aoni.ClientOption) *Client {
	c := &Client{
		engine: defaultFasthttpClient(),
		config: aoni.Config{
			Defaults: aoni.ClientDefaults{
				Headers: make(http.Header),
			},
		},
	}

	for _, opt := range opts {
		if opt != nil {
			opt(&c.config)
		}
	}

	c.applyEngineConfig()
	c.applyCustomDialer()

	c.pipelineEngine = pipeline.NewPipeline(
		c.config.Defaults.ToPipelineDefaults(),
		c.config.Fingerprint.ToPipelineFingerprint(),
	)

	return c
}

// With produces a deep-copied [Client] with the provided functional options applied.
func (c *Client) With(opts ...aoni.ClientOption) *Client {
	clonedEngine := cloneFasthttpClient(c.engine)

	c2 := &Client{
		engine:      clonedEngine,
		defaultDial: c.defaultDial,
		config:      c.config.Clone(),
	}

	for _, opt := range opts {
		if opt != nil {
			opt(&c2.config)
		}
	}

	c2.applyEngineConfig()

	if !isCustomDialerSet(c.engine, c.defaultDial) {
		c2.applyCustomDialer()
	}

	c2.pipelineEngine = pipeline.NewPipeline(
		c2.config.Defaults.ToPipelineDefaults(),
		c2.config.Fingerprint.ToPipelineFingerprint(),
	)

	return c2
}

// Config returns a copy of active client configurations.
func (c *Client) Config() aoni.Config {
	return c.config
}

// Engine returns the underlying [*fasthttp.Client] engine instance.
func (c *Client) Engine() *fasthttp.Client {
	return c.engine
}

// Request executes an HTTP request across HTTP/1.1, native HTTP/2, or native HTTP/3.
func (c *Client) Request(
	ctx context.Context,
	method, path string,
	mods ...aoni.RequestModifier,
) (aoni.Response, error) {
	if handler := c.resolveProtocolHandler(path); handler != nil {
		stdReq, err := http.NewRequestWithContext(ctx, method, path, nil)
		if err != nil {
			return nil, err
		}

		resp, err := handler.RoundTrip(stdReq) //nolint:bodyclose
		if err != nil {
			return nil, err
		}

		return aoni.NewStdResponse(resp), nil
	}

	fastReq := fasthttp.AcquireRequest()
	fastResp := fasthttp.AcquireResponse()

	reqAdapter := NewRequest(fastReq)
	defer reqAdapter.Release()

	reqAdapter.SetContext(ctx)
	reqAdapter.SetMethod(method)

	if err := c.resolveTargetURL(reqAdapter, path); err != nil {
		fasthttp.ReleaseRequest(fastReq)
		fasthttp.ReleaseResponse(fastResp)

		return nil, err
	}

	reqCfg := aoni.GetOrInitRequestConfig(ctx)
	if reqCfg.TargetHost == "" {
		if u, err := url.Parse(reqAdapter.URL()); err == nil && u.Hostname() != "" {
			reqCfg.TargetHost = u.Hostname()
		} else if hostStr := string(fastReq.URI().Host()); hostStr != "" {
			h, _, _ := net.SplitHostPort(hostStr)
			if h == "" {
				h = hostStr
			}

			reqCfg.TargetHost = h
		}
	}

	c.applyDefaultHeaders(reqAdapter)
	c.applyModifiers(reqAdapter, mods)

	reqCtx := reqAdapter.Context()

	stdResp, err := c.pipelineEngine.Execute(
		reqCtx,
		reqAdapter,
		c.HTTP(),
		c.config.Defaults.Pipeline,
	) //nolint:bodyclose
	if err != nil {
		fasthttp.ReleaseRequest(fastReq)
		fasthttp.ReleaseResponse(fastResp)

		return nil, err
	}

	return aoni.NewStdResponse(stdResp), nil
}

// Do executes a prepared [aoni.Request] contract, routing through the target native protocol engine (H1, H2, or H3).
func (c *Client) Do(req aoni.Request) (aoni.Response, error) {
	if req == nil {
		req = NewRequest(nil)
	}

	ctx := req.Context()

	stdResp, err := c.pipelineEngine.Execute(ctx, req, c.HTTP(), c.config.Defaults.Pipeline) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	return aoni.NewStdResponse(stdResp), nil
}

// HTTP returns an [aoni.HTTPDoer] executing requests via fasthttp, H2, or H3.
func (c *Client) HTTP() aoni.HTTPDoer {
	return aoni.HTTPDoerFunc(func(req *http.Request) (*http.Response, error) {
		fastReq := fasthttp.AcquireRequest()
		fastResp := fasthttp.AcquireResponse()

		fastReq.Header.SetMethod(req.Method)
		fastReq.SetRequestURI(req.URL.String())

		for k, vv := range req.Header {
			for _, v := range vv {
				fastReq.Header.Add(k, v)
			}
		}

		if req.Body != nil && req.Body != http.NoBody {
			fastReq.SetBodyStream(req.Body, int(req.ContentLength))
		}

		ctx := req.Context()
		c.applyCookies(ctx, fastReq)
		extractUserInfoAndSetAuth(fastReq)

		_, err, autoReleased := c.dispatchSingleRequest(ctx, fastReq, fastResp)
		if err != nil {
			if !autoReleased {
				fasthttp.ReleaseRequest(fastReq)
				fasthttp.ReleaseResponse(fastResp)
			}

			return nil, err
		}

		c.captureCookies(ctx, fastReq, fastResp)

		bodyRC := &fastBodyReadCloser{
			Reader:   bytes.NewReader(fastResp.Body()),
			fastReq:  fastReq,
			fastResp: fastResp,
		}

		httpResp := &http.Response{
			StatusCode:    fastResp.StatusCode(),
			Status:        http.StatusText(fastResp.StatusCode()),
			Header:        make(http.Header),
			Body:          bodyRC,
			ContentLength: int64(len(fastResp.Body())),
			Request:       req,
		}

		fastResp.Header.All()(func(k, v []byte) bool {
			httpResp.Header.Add(string(k), string(v))
			return true
		})

		return httpResp, nil
	})
}

var (
	_ aoni.RequestDoer = (*Client)(nil)
	_ aoni.WSDialer    = (*Client)(nil)
)
