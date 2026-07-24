// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast/h2engine"
	"github.com/lemon4ksan/aoni/fast/h3engine"
)

// Client executes ultra-high-performance HTTP requests over fasthttp,
// seamlessly multiplexing native H1 (fasthttp), native H2 (h2engine), and native H3 (h3engine).
type Client struct {
	engine    *fasthttp.Client
	h2Clients map[string]*h2engine.Client
	h3Client  *h3engine.Client
	config    aoni.Config
	h2Mutex   sync.Mutex
	h3Once    sync.Once
}

// Option is an alias for [aoni.ClientOption].
type Option = aoni.ClientOption

// NewClient creates a new multiprotocol Client configured with fasthttp, uTLS,
// native HTTP/2 framing, and native HTTP/3 QUIC support.
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

// Request executes an HTTP request, seamlessly routing execution across
// HTTP/1.1 (fasthttp), native HTTP/2 (h2engine), or native HTTP/3 (h3engine).
//
// Postconditions:
//   - The returned [aoni.Response] MUST be closed via [aoni.Response.Close]
//     to return objects back to [sync.Pool].
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

	alpnMode := resolveALPNMode(reqAdapter.Context(), &c.config)
	switch alpnMode {
	case aoni.AlpnH3:
		h3 := c.getH3Client()
		err := h3.Do(ctx, fastReq, fastResp, c.config.Fingerprint.HeaderOrder)
		if err != nil {
			fasthttp.ReleaseRequest(fastReq)
			fasthttp.ReleaseResponse(fastResp)

			return nil, err
		}

		return NewPooledResponse(fastReq, fastResp), nil

	case aoni.AlpnH2:
		host := string(fastReq.URI().Host())
		h2Cl := c.getH2Client(host)
		err := h2Cl.Do(fastReq, fastResp)
		if err != nil {
			fasthttp.ReleaseRequest(fastReq)
			fasthttp.ReleaseResponse(fastResp)

			return nil, err
		}

		return NewPooledResponse(fastReq, fastResp), nil

	default:
		if err := c.executeFastHTTP(ctx, fastReq, fastResp); err != nil {
			fasthttp.ReleaseRequest(fastReq)
			fasthttp.ReleaseResponse(fastResp)

			return nil, err
		}

		return NewPooledResponse(fastReq, fastResp), nil
	}
}

// Do executes a prepared [aoni.Request] contract, routing through the target
// native protocol engine (H1, H2, or H3).
func (c *Client) Do(req aoni.Request) (aoni.Response, error) {
	if req == nil {
		req = NewRequest(nil)
	}

	ctx := req.Context()
	fastReq, ok := req.EngineRequest().(*fasthttp.Request)
	if !ok {
		fastReq = fasthttp.AcquireRequest()
		fastReq.Header.SetMethod(req.Method())
		fastReq.SetRequestURI(req.URL())

		if body := req.BodyBytes(); len(body) > 0 {
			fastReq.SetBody(body)
		}
	}

	fastResp := fasthttp.AcquireResponse()
	alpnMode := resolveALPNMode(ctx, &c.config)

	var err error
	switch alpnMode {
	case aoni.AlpnH3:
		err = c.getH3Client().Do(ctx, fastReq, fastResp, c.config.Fingerprint.HeaderOrder)
	case aoni.AlpnH2:
		host := string(fastReq.URI().Host())
		err = c.getH2Client(host).Do(fastReq, fastResp)
	default:
		err = c.engine.Do(fastReq, fastResp)
	}

	if err != nil {
		fasthttp.ReleaseResponse(fastResp)
		return nil, err
	}

	return NewResponse(fastResp), nil
}

func (c *Client) getH3Client() *h3engine.Client {
	c.h3Once.Do(func() {
		tlsCfg := &tls.Config{
			InsecureSkipVerify: c.config.Engine.InsecureSkipVerify,
		}

		if spec := c.config.Fingerprint.TLSQUICClientHelloSpec; spec != nil && len(spec.CipherSuites) > 0 {
			tlsCfg.CipherSuites = spec.CipherSuites
		}

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

		c.h3Client = h3engine.NewClient(tlsCfg, quicCfg)
	})

	return c.h3Client
}

func (c *Client) getH2Client(host string) *h2engine.Client {
	c.h2Mutex.Lock()
	defer c.h2Mutex.Unlock()

	if c.h2Clients == nil {
		c.h2Clients = make(map[string]*h2engine.Client)
	}

	if cl, ok := c.h2Clients[host]; ok {
		return cl
	}

	dialer := &h2engine.Dialer{
		Addr: host,
		RawDial: func(addr string) (net.Conn, error) {
			fastD := newFastDialer(&c.config)
			return fastD.DialH2(context.Background(), addr)
		},
	}

	cl := h2engine.NewClient(dialer, h2engine.ClientOpts{
		PingInterval: 15 * time.Second,
	})

	if len(c.config.Fingerprint.HeaderOrder) > 0 {
		cl.SetOrderedHeaders(c.config.Fingerprint.HeaderOrder)
	}

	c.h2Clients[host] = cl

	return cl
}

func resolveALPNMode(ctx context.Context, cfg *aoni.Config) string {
	reqCfg := aoni.GetRequestConfig(ctx)
	if reqCfg != nil && len(reqCfg.ALPNOverride) > 0 {
		first := reqCfg.ALPNOverride[0]
		if first == aoni.AlpnH3 || first == aoni.AlpnH2 {
			return first
		}
	}

	if len(cfg.Fingerprint.HeaderOrder) > 0 && slicesContains(cfg.Fingerprint.HeaderOrder, ":method") {
		return aoni.AlpnH2
	}

	return aoni.AlpnHTTP
}

func slicesContains(slice []string, target string) bool {
	for _, item := range slice {
		if item == target {
			return true
		}
	}

	return false
}

func (c *Client) applyEngineConfig() {
	if c.config.Engine.Timeout > 0 {
		c.engine.ReadTimeout = c.config.Engine.Timeout
		c.engine.WriteTimeout = c.config.Engine.Timeout
	}

	if c.config.Engine.InsecureSkipVerify {
		c.engine.TLSConfig = &tls.Config{InsecureSkipVerify: true}
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

	if len(path) >= 7 && (strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://")) {
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

// PooledResponse wraps a fasthttp response and returns instances back to [sync.Pool] upon Close.
type PooledResponse struct {
	*Response
	fastReq  *fasthttp.Request
	fastResp *fasthttp.Response
	once     sync.Once
}

// NewPooledResponse creates a PooledResponse adapter around fastResp.
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
