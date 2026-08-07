// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package fast provides high-performance fasthttp engine adapters for [aoni.Request] and [aoni.Response].
package fast

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zstd"
	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/bytesconv"
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

// AcquireRequest satisfies [aoni.RequestFactory] by acquiring a pooled [Request] instance.
func (c *Client) AcquireRequest() aoni.Request {
	return NewRequest(nil)
}

// ReleaseRequest satisfies [aoni.RequestFactory] by returning req back to the memory pool.
func (c *Client) ReleaseRequest(req aoni.Request) {
	if fastReq, ok := req.(*Request); ok {
		fastReq.Release()
	}
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

	stdResp, err := c.pipelineEngine.Execute(reqCtx, reqAdapter, c.HTTP(), c.resolvePipeline(reqCtx)) //nolint:bodyclose
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

	stdResp, err := c.pipelineEngine.Execute(ctx, req, c.HTTP(), c.resolvePipeline(ctx)) //nolint:bodyclose
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
		fastReq.Header.SetHost(req.URL.Host)

		for k, vv := range req.Header {
			for _, v := range vv {
				fastReq.Header.Add(k, v)
			}
		}

		if req.Body != nil && req.Body != http.NoBody {
			contentLen := req.ContentLength
			if contentLen <= 0 {
				if clStr := req.Header.Get("Content-Length"); clStr != "" {
					if parsed, err := strconv.ParseInt(strings.TrimSpace(clStr), 10, 64); err == nil {
						contentLen = parsed
					}
				}
			}

			fastReq.SetBodyStream(req.Body, int(contentLen))
		}

		ctx := req.Context()

		_, err, autoReleased := c.executeWithRedirects(ctx, fastReq, fastResp)
		if err != nil {
			if !autoReleased {
				fasthttp.ReleaseRequest(fastReq)
				fasthttp.ReleaseResponse(fastResp)
			}

			return nil, err
		}

		uncompressed := decompressFastResponse(fastResp)

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
			Uncompressed:  uncompressed,
			Request:       req,
		}

		fastResp.Header.All()(func(k, v []byte) bool {
			httpResp.Header.Add(string(k), string(v))
			return true
		})

		return httpResp, nil
	})
}

func (c *Client) executeWithRedirects(
	ctx context.Context,
	fastReq *fasthttp.Request,
	fastResp *fasthttp.Response,
) (trailers map[string][]string, err error, autoReleased bool) {
	redirectLimit := c.config.Engine.RedirectLimit
	if redirectLimit < 0 {
		redirectLimit = 10
	}

	if redirectLimit == 0 {
		c.applyCookies(ctx, fastReq)
		extractUserInfoAndSetAuth(fastReq)

		trailers, err, autoReleased = c.dispatchSingleRequest(ctx, fastReq, fastResp)
		if err == nil {
			c.captureCookies(ctx, fastReq, fastResp)
		}

		return trailers, err, autoReleased
	}

	currentURI := fasthttp.AcquireURI()
	defer fasthttp.ReleaseURI(currentURI)

	var redirectsFollowed int

	for {
		c.applyCookies(ctx, fastReq)
		fastReq.URI().CopyTo(currentURI)
		extractUserInfoAndSetAuth(fastReq)

		trailers, err, autoReleased = c.dispatchSingleRequest(ctx, fastReq, fastResp)
		if err != nil {
			return nil, err, autoReleased
		}

		c.captureCookies(ctx, fastReq, fastResp)

		statusCode := fastResp.StatusCode()
		if !isRedirectStatus(statusCode) {
			return trailers, nil, false
		}

		location := fastResp.Header.Peek("Location")
		if len(location) == 0 {
			return trailers, nil, false
		}

		redirectsFollowed++
		if redirectsFollowed > redirectLimit {
			return nil, ErrMaxRedirectsExceeded, false
		}

		if err := applyRedirectMethodAndBody(statusCode, fastReq); err != nil {
			return nil, err, false
		}

		nextURI := fasthttp.AcquireURI()
		currentURI.CopyTo(nextURI)
		nextURI.UpdateBytes(location)

		if len(nextURI.Scheme()) == 0 {
			nextURI.SetSchemeBytes(currentURI.Scheme())
		}

		if len(nextURI.Host()) == 0 {
			nextURI.SetHostBytes(currentURI.Host())
		}

		nextURI.CopyTo(fastReq.URI())

		if !isSameHost(currentURI, nextURI) {
			scrubSensitiveHeaders(fastReq, currentURI, nextURI)
		}

		if isHTTPSDowngrade(currentURI, nextURI) {
			fastReq.Header.Del("Referer")
		} else {
			fastReq.Header.SetBytesK(bytesconv.S2B("Referer"), string(currentURI.FullURI()))
		}

		fasthttp.ReleaseURI(nextURI)
		fastResp.Reset()
	}
}

func isRedirectStatus(code int) bool {
	return code == fasthttp.StatusMovedPermanently ||
		code == fasthttp.StatusFound ||
		code == fasthttp.StatusSeeOther ||
		code == fasthttp.StatusTemporaryRedirect ||
		code == fasthttp.StatusPermanentRedirect
}

func isSameHost(u1, u2 *fasthttp.URI) bool {
	return bytes.EqualFold(u1.Host(), u2.Host())
}

func isHTTPSDowngrade(u1, u2 *fasthttp.URI) bool {
	return bytes.EqualFold(u1.Scheme(), []byte("https")) && bytes.EqualFold(u2.Scheme(), []byte("http"))
}

func applyRedirectMethodAndBody(statusCode int, req *fasthttp.Request) error {
	switch statusCode {
	case fasthttp.StatusMovedPermanently, fasthttp.StatusFound, fasthttp.StatusSeeOther:
		method := string(req.Header.Method())
		if method != http.MethodGet && method != http.MethodHead {
			req.Header.SetMethod(http.MethodGet)
			req.SetBody(nil)
			req.Header.Del("Content-Type")
			req.Header.Del("Content-Length")
		}
	}

	return nil
}

func decompressFastResponse(resp *fasthttp.Response) bool {
	encodingBytes := resp.Header.Peek("Content-Encoding")
	if len(encodingBytes) == 0 {
		return false
	}

	encoding := strings.ToLower(bytesconv.B2S(encodingBytes))

	body := resp.Body()
	if len(body) == 0 {
		return false
	}

	var (
		decompressed []byte
		err          error
	)

	switch {
	case strings.Contains(encoding, "gzip"):
		gzReader, gzErr := gzip.NewReader(bytes.NewReader(body))
		if gzErr == nil {
			decompressed, err = io.ReadAll(gzReader)
			_ = gzReader.Close()
		}

	case strings.Contains(encoding, "br"):
		brReader := brotli.NewReader(bytes.NewReader(body))
		decompressed, err = io.ReadAll(brReader)

	case strings.Contains(encoding, "zstd"):
		if zDec, zErr := zstd.NewReader(bytes.NewReader(body)); zErr == nil {
			decompressed, err = io.ReadAll(zDec)
			zDec.Close()
		}
	}

	if err == nil && len(decompressed) > 0 {
		resp.SetBody(decompressed)
		resp.Header.Del("Content-Encoding")
		resp.Header.Del("Content-Length")

		return true
	}

	return false
}

func (c *Client) resolvePipeline(ctx context.Context) aoni.PipelineConfig {
	if reqPipe, ok := aoni.GetPipeline(ctx); ok {
		if reqPipe.PrecomputedFlags == 0 {
			reqPipe.BuildFlags()
		}

		return reqPipe
	}

	pipe := c.config.Defaults.Pipeline
	if !pipe.RotateUA && len(c.config.Defaults.UARotationProfiles) > 0 {
		pipe.RotateUA = true
	}

	if pipe.SizeLimit == 0 {
		pipe.SizeLimit = c.config.Defaults.MaxResponseSize
	}

	if !pipe.Inspect && c.config.Defaults.Inspector != nil {
		pipe.Inspect = true
	}

	if pipe.Hedging == nil && (c.config.Network.HedgingDelay > 0 || c.config.Network.DynamicHedging != nil) {
		pipe.Hedging = &aoni.HedgingConfig{
			DefaultDelay:   c.config.Network.HedgingDelay,
			DynamicHedging: c.config.Network.DynamicHedging,
		}
	}

	pipe.BuildFlags()

	return pipe
}

var (
	_ aoni.RequestDoer = (*Client)(nil)
	_ aoni.WSDialer    = (*Client)(nil)
)
