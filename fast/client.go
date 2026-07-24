// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/valyala/fasthttp"
	"golang.org/x/net/http2"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fingerprint/h2"
	"github.com/lemon4ksan/aoni/internal/bytesconv"
)

// Client executes ultra-high-performance HTTP requests over fasthttp, seamlessly multiplexing H1, H2, and H3 (quic-go).
type Client struct {
	engine      *fasthttp.Client
	h2Transport http.RoundTripper
	h3Transport http.RoundTripper
	config      aoni.Config
	h2Once      sync.Once
	h3Once      sync.Once
}

// Option is an alias for [aoni.ClientOption].
type Option = aoni.ClientOption

// NewClient creates a new multiprotocol [Client] configured with fasthttp, uTLS, HTTP/2 framing, and HTTP/3 QUIC support.
func NewClient(opts ...aoni.ClientOption) *Client {
	c := &Client{
		engine: &fasthttp.Client{
			ReadTimeout:         15 * time.Second,
			WriteTimeout:        15 * time.Second,
			MaxConnsPerHost:     512,
			MaxIdleConnDuration: 90 * time.Second,
		},
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

	return c
}

// Config returns a copy of active client configurations.
func (c *Client) Config() aoni.Config {
	return c.config
}

// Engine returns the underlying [*fasthttp.Client] engine instance.
func (c *Client) Engine() *fasthttp.Client {
	return c.engine
}

// Request executes an HTTP request, seamlessly routing execution across HTTP/1.1 (fasthttp), HTTP/2, or HTTP/3 (quic-go).
//
// Postconditions:
//   - When executing over HTTP/1.1, the returned [aoni.Response] MUST be closed via [aoni.Response.Close] to return objects to [sync.Pool].
func (c *Client) Request(
	ctx context.Context,
	method, path string,
	mods ...aoni.RequestModifier,
) (aoni.Response, error) {
	fastReq := fasthttp.AcquireRequest()
	fastResp := fasthttp.AcquireResponse()

	reqAdapter := NewRequest(fastReq)
	reqAdapter.SetContext(ctx)
	reqAdapter.SetMethod(method)

	if err := c.resolveTargetURL(reqAdapter, path); err != nil {
		fasthttp.ReleaseRequest(fastReq)
		fasthttp.ReleaseResponse(fastResp)

		return nil, err
	}

	c.applyDefaultHeaders(reqAdapter)
	c.applyModifiers(reqAdapter, mods)

	alpnMode := resolveALPNMode(ctx, &c.config)
	switch alpnMode {
	case aoni.AlpnH3:
		fasthttp.ReleaseRequest(fastReq)
		fasthttp.ReleaseResponse(fastResp)

		return c.executeStdTransport(ctx, reqAdapter, c.getH3Transport())

	case aoni.AlpnH2:
		fasthttp.ReleaseRequest(fastReq)
		fasthttp.ReleaseResponse(fastResp)

		return c.executeStdTransport(ctx, reqAdapter, c.getH2Transport())

	default:
		if err := c.executeFastHTTP(ctx, fastReq, fastResp); err != nil {
			fasthttp.ReleaseRequest(fastReq)
			fasthttp.ReleaseResponse(fastResp)

			return nil, err
		}

		return NewPooledResponse(fastReq, fastResp), nil
	}
}

// Do executes a prepared [aoni.Request] contract, routing through the target protocol engine (H1, H2, or H3 over quic-go).
func (c *Client) Do(req aoni.Request) (aoni.Response, error) {
	if req == nil {
		req = NewRequest(nil)
	}

	ctx := req.Context()
	alpnMode := resolveALPNMode(ctx, &c.config)

	switch alpnMode {
	case aoni.AlpnH3:
		return c.executeStdTransport(ctx, req, c.getH3Transport())
	case aoni.AlpnH2:
		return c.executeStdTransport(ctx, req, c.getH2Transport())
	}

	fastReq, ok := req.EngineRequest().(*fasthttp.Request)
	if !ok {
		fastReq = fasthttp.AcquireRequest()
		defer fasthttp.ReleaseRequest(fastReq)

		fastReq.Header.SetMethod(req.Method())
		fastReq.SetRequestURI(req.URL())

		if body := req.BodyBytes(); len(body) > 0 {
			fastReq.SetBody(body)
		}
	}

	fastResp := fasthttp.AcquireResponse()
	if err := c.engine.Do(fastReq, fastResp); err != nil {
		fasthttp.ReleaseResponse(fastResp)
		return nil, err
	}

	return NewResponse(fastResp), nil
}

func (c *Client) getH3Transport() http.RoundTripper {
	c.h3Once.Do(func() {
		quicCfg := &quic.Config{
			EnableDatagrams: true,
		}

		if h3s := c.config.Fingerprint.H3Settings; h3s != nil {
			quicCfg.InitialStreamReceiveWindow = h3s.InitialStreamReceiveWindow
			quicCfg.MaxStreamReceiveWindow = h3s.MaxStreamReceiveWindow
			quicCfg.InitialConnectionReceiveWindow = h3s.InitialConnectionReceiveWindow
			quicCfg.MaxConnectionReceiveWindow = h3s.MaxConnectionReceiveWindow
			quicCfg.MaxIncomingStreams = h3s.MaxIncomingStreams
			quicCfg.MaxIncomingUniStreams = h3s.MaxIncomingUniStreams
			quicCfg.EnableDatagrams = h3s.EnableDatagrams
		}

		tlsCfg := &tls.Config{
			NextProtos:         []string{aoni.AlpnH3},
			InsecureSkipVerify: c.config.Engine.InsecureSkipVerify,
		}

		if spec := c.config.Fingerprint.TLSQUICClientHelloSpec; spec != nil && len(spec.CipherSuites) > 0 {
			tlsCfg.CipherSuites = spec.CipherSuites
		}

		c.h3Transport = &http3.Transport{
			TLSClientConfig: tlsCfg,
			QUICConfig:      quicCfg,
		}
	})

	return c.h3Transport
}

func (c *Client) getH2Transport() http.RoundTripper {
	c.h2Once.Do(func() {
		baseTr := &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				dialer := newFastDialer(&c.config)
				return dialer.DialH2(ctx, addr)
			},
			ForceAttemptHTTP2: true,
		}

		if h2Settings := c.config.Fingerprint.H2Settings; h2Settings != nil {
			c.h2Transport = h2.NewFramedTransport(baseTr, *h2Settings, c.config.Fingerprint.HeaderOrder...)
			return
		}

		t2, err := http2.ConfigureTransports(baseTr)
		if err == nil && t2 != nil {
			t2.ReadIdleTimeout = 15 * time.Second
		}

		c.h2Transport = baseTr
	})

	return c.h2Transport
}

func (c *Client) executeStdTransport(
	ctx context.Context,
	req aoni.Request,
	tr http.RoundTripper,
) (aoni.Response, error) {
	httpReq := req.HTTPRequest()
	if httpReq == nil {
		var err error
		httpReq, err = http.NewRequestWithContext(ctx, req.Method(), req.URL(), req.BodyStream())
		if err != nil {
			return nil, err
		}

		if fastReq, ok := req.EngineRequest().(*fasthttp.Request); ok && fastReq != nil {
			fastReq.Header.All()(func(k, v []byte) bool {
				httpReq.Header.Add(string(k), string(v))
				return true
			})
		}
	}

	resp, err := tr.RoundTrip(httpReq)
	if err != nil {
		return nil, err
	}

	return aoni.NewStdResponse(resp), nil
}

func resolveALPNMode(ctx context.Context, cfg *aoni.Config) string {
	reqCfg := aoni.GetRequestConfig(ctx)
	if reqCfg != nil && len(reqCfg.ALPNOverride) > 0 {
		first := reqCfg.ALPNOverride[0]
		if first == aoni.AlpnH3 || first == aoni.AlpnH2 {
			return first
		}
	}

	if len(cfg.Fingerprint.HeaderOrder) > 0 && slices.Contains(cfg.Fingerprint.HeaderOrder, ":method") {
		return aoni.AlpnH2
	}

	return aoni.AlpnHTTP
}

func (c *Client) applyEngineConfig() {
	if c.config.Engine.Timeout > 0 {
		c.engine.ReadTimeout = c.config.Engine.Timeout
		c.engine.WriteTimeout = c.config.Engine.Timeout
	}

	if c.config.Engine.InsecureSkipVerify {
		c.engine.TLSConfig = nil
	}

	c.engine.DisableHeaderNamesNormalizing = true
}

func (c *Client) applyCustomDialer() {
	dialer := newFastDialer(&c.config)
	c.engine.Dial = dialer.Dial
	c.engine.DialDualStack = true
}

func (c *Client) applyDefaultHeaders(req aoni.Request) {
	if c.config.Defaults.Headers == nil {
		return
	}

	for k, vv := range c.config.Defaults.Headers {
		if req.Header(k) == "" && len(vv) > 0 {
			req.SetHeader(k, vv[0])
		}
	}
}

func (c *Client) applyModifiers(req aoni.Request, mods []aoni.RequestModifier) {
	for _, defaultMod := range c.config.Defaults.DefaultMods {
		if defaultMod != nil {
			defaultMod(req)
		}
	}

	for _, m := range mods {
		if m != nil {
			m(req)
		}
	}
}

func (c *Client) executeFastHTTP(
	ctx context.Context,
	req *fasthttp.Request,
	resp *fasthttp.Response,
) error {
	if deadline, ok := ctx.Deadline(); ok {
		return c.engine.DoDeadline(req, resp, deadline)
	}

	if c.config.Engine.Timeout > 0 {
		return c.engine.DoTimeout(req, resp, c.config.Engine.Timeout)
	}

	return c.engine.Do(req, resp)
}

func (c *Client) resolveTargetURL(req aoni.Request, path string) error {
	baseURLStr := c.config.Defaults.BaseURL.String()

	if path == "" && baseURLStr == "" {
		return ErrTargetURLEmpty
	}

	if len(path) >= 7 && (bytesconv.EqualFoldASCII(path[:7], "http://") || bytesconv.EqualFoldASCII(path[:min(len(path), 8)], "https://")) {
		req.SetURL(path)
		return nil
	}

	if baseURLStr == "" {
		req.SetURL(path)
		return nil
	}

	baseURL := strings.TrimSuffix(baseURLStr, "/")
	cleanPath := path

	if len(path) == 0 || path[0] != '/' {
		cleanPath = "/" + path
	}

	req.SetURL(baseURL + cleanPath)

	return nil
}

// PooledResponse wraps a fasthttp response and returns instances back to [sync.Pool] upon invocation of [PooledResponse.Close].
type PooledResponse struct {
	*Response
	fastReq  *fasthttp.Request
	fastResp *fasthttp.Response
	once     sync.Once
}

// NewPooledResponse creates a [PooledResponse] adapter around fastResp.
func NewPooledResponse(fastReq *fasthttp.Request, fastResp *fasthttp.Response) *PooledResponse {
	return &PooledResponse{
		Response: NewResponse(fastResp),
		fastReq:  fastReq,
		fastResp: fastResp,
	}
}

// Close releases underlying fasthttp request and response objects back to [sync.Pool].
func (r *PooledResponse) Close() error {
	r.once.Do(func() {
		if r.fastReq != nil {
			fasthttp.ReleaseRequest(r.fastReq)
			r.fastReq = nil
		}

		if r.fastResp != nil {
			fasthttp.ReleaseResponse(r.fastResp)
			r.fastResp = nil
		}
	})

	return nil
}

var (
	_ aoni.Response    = (*PooledResponse)(nil)
	_ aoni.RequestDoer = (*Client)(nil)
)
