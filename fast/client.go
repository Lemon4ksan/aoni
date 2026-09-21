// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/lemon4ksan/foundation/borrow"
	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/net/urlkit"
	mach "github.com/lemon4ksan/mach/proto/http"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/body"
	"github.com/lemon4ksan/aoni/internal/transport"
	"github.com/lemon4ksan/aoni/mod"
)

// Client is an ultra-high-throughput HTTP client engine designed for zero heap allocations
// on high-concurrency request paths.
//
// Concurrency & Architecture:
// Client multiplexes HTTP/1.1, HTTP/2, and HTTP/3 connections across an underlying
// [transport.Pool]. All methods on Client are safe for concurrent use by multiple goroutines.
// To achieve maximum performance, incoming and outgoing transactions utilize CPU-pinned
// storage pools ([pool.PerPStorage]), requiring callers to release response buffers
// via [Response.Close].
type Client struct {
	mu          sync.RWMutex
	engine      *transport.Pool
	cfg         aoni.Config
	middlewares []aoni.Middleware
	chain       aoni.RequestDoer
}

// NewClient creates a new high-performance baremetal fast.Client.
// It accepts composable [aoni.ClientOption] functional layers.
func NewClient(opts ...aoni.ClientOption) *Client {
	c := aoni.Config{}
	client := &Client{
		engine: transport.NewPool(),
	}

	generic.ApplyOptions(&c, opts...)

	if c.Network.TLSConfig != nil {
		client.engine.TLSConfig = c.Network.TLSConfig
	}

	if c.Ext.WrapTLSClient != nil {
		client.engine.WrapTLSClient = c.Ext.WrapTLSClient
	}

	if c.Engine.EnableH2 {
		client.engine.EnableH2 = true
		client.engine.ForceH2 = true
	}

	if c.Engine.EnableH3 {
		client.engine.EnableH3 = true
		client.engine.ForceH3 = true
	}

	if c.Engine.H3Settings != nil {
		client.engine.H3Settings = c.Engine.H3Settings
	}

	client.cfg = c
	if len(c.Middlewares) > 0 {
		client.middlewares = slices.Clone(c.Middlewares)
	}

	client.rebuildChainLocked()

	return client
}

// Engine returns the underlying multi-protocol connection pool.
func (c *Client) Engine() *transport.Pool {
	return c.engine
}

// Unwrap returns the underlying multi-protocol connection pool for onion-peeling traversal.
func (c *Client) Unwrap() *transport.Pool {
	return c.engine
}

// SetTLSConfig configures the TLS configuration on the underlying transport pool.
// Concurrency: Must be called before dispatching concurrent requests.
func (c *Client) SetTLSConfig(tlsConf *tls.Config) *Client {
	c.engine.TLSConfig = tlsConf
	return c
}

// EnableH2 enables or disables HTTP/2 in the underlying transport pool.
// Concurrency: Must be called before dispatching concurrent requests.
func (c *Client) EnableH2(enable bool) *Client {
	c.engine.EnableH2 = enable
	return c
}

// EnableH3 enables or disables HTTP/3 QUIC in the underlying transport pool.
// Concurrency: Must be called before dispatching concurrent requests.
func (c *Client) EnableH3(enable bool) *Client {
	c.engine.EnableH3 = enable
	return c
}

// Use registers one or more [aoni.Middleware] interceptors into the client execution chain.
// Middlewares wrap the core transport and execute sequentially around each HTTP transaction.
// Returns the client receiver for fluent chaining.
func (c *Client) Use(mws ...aoni.Middleware) *Client {
	if len(mws) == 0 {
		return c
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, mw := range mws {
		if mw != nil {
			c.middlewares = append(c.middlewares, mw)
			c.cfg.Middlewares = append(c.cfg.Middlewares, mw)
		}
	}

	c.rebuildChainLocked()

	return c
}

func (c *Client) rebuildChainLocked() {
	if len(c.middlewares) == 0 {
		c.chain = nil
		return
	}

	var curr aoni.RequestDoer = aoni.DoerFunc(c.doRaw)
	for i := len(c.middlewares) - 1; i >= 0; i-- {
		if mw := c.middlewares[i]; mw != nil {
			curr = mw(curr)
		}
	}

	c.chain = curr
}

// Do executes a unified [aoni.Request] transaction and returns an [aoni.Response].
//
// Protocol & Lifecycle:
//   - Zero Allocation: Hot paths recycle pooled request/response adapters via Per-P storage.
//   - Redirects: Automatically follows redirects according to configured limits, scrubbing sensitive headers across cross-domain hops.
//   - Memory Management: The caller MUST call resp.Close() when finished with the response to return underlying buffers to the pool.
func (c *Client) Do(req aoni.Request) (aoni.Response, error) {
	if req == nil {
		return nil, ErrNilRequest
	}

	c.mu.RLock()
	chain := c.chain
	c.mu.RUnlock()

	if chain != nil {
		return chain.Do(req)
	}

	return c.doRaw(req)
}

func (c *Client) doRaw(req aoni.Request) (aoni.Response, error) {
	if req == nil {
		return nil, ErrNilRequest
	}

	fastReq, isAdapter := toFastRequest(req)
	if isAdapter {
		defer fastReq.Release()
	}

	fastRes := NewResponse(nil)
	if fastReq.req.Header.IsHead() || string(fastReq.req.Header.Method()) == http.MethodHead ||
		req.Method() == http.MethodHead {
		fastRes.resp.SkipBody = true
	}

	ctx := req.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	trailers, err, autoReleased := c.executeWithRedirects(ctx, fastReq.req, fastRes.resp)
	if err != nil {
		if !autoReleased {
			fastRes.Release()
		}

		return nil, err
	}

	fastRes.SetTrailers(trailers)
	decompressFastResponse(fastRes.resp)

	return fastRes, nil
}

func (c *Client) execute(
	ctx context.Context,
	fastReq *mach.Request,
	fastRes *mach.Response,
) (map[string][]string, error, bool) {
	if fastReq.Header.IsHead() || string(fastReq.Header.Method()) == http.MethodHead {
		fastRes.SkipBody = true
	}

	trailers, err := c.engine.DoCtx(ctx, fastReq, fastRes)

	return trailers, err, false
}

// Clone creates an independent copy of the fast.Client with isolated configuration
// and cloned middleware chains, sharing the underlying transport connection pool.
func (c *Client) Clone() *Client {
	return c.With()
}

// With returns a new fast.Client with the provided options applied.
//
// Concurrency:
// The receiver Client is not modified and remains safe for concurrent use.
// The returned Client is fully independent with isolated configuration state.
func (c *Client) With(opts ...aoni.ClientOption) *Client {
	c.mu.RLock()
	newCfg := c.cfg.Clone()
	c.mu.RUnlock()

	generic.ApplyOptions(&newCfg, opts...)

	cloned := &Client{
		engine: c.engine,
		cfg:    newCfg,
	}

	if len(newCfg.Middlewares) > 0 {
		cloned.middlewares = slices.Clone(newCfg.Middlewares)
	}

	cloned.rebuildChainLocked()

	return cloned
}

func (c *Client) resolveURL(path string) (string, error) {
	if path == "" {
		if c.cfg.Defaults.BaseURL != nil && c.cfg.Defaults.BaseURL.Scheme != "" {
			return c.cfg.Defaults.BaseURL.String(), nil
		}

		return "", ErrTargetURLEmpty
	}

	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path, nil
	}

	if c.cfg.Defaults.BaseURL != nil && c.cfg.Defaults.BaseURL.Scheme != "" {
		u, err := urlkit.Resolve(c.cfg.Defaults.BaseURL, path)
		if err != nil {
			return "", &aoni.Error{Op: "resolve_url", Err: err}
		}

		return u.String(), nil
	}

	return path, nil
}

// Request executes an HTTP request using the specified method, destination path, and modifiers.
//
// Memory Lifetime:
// The returned [*Response] is acquired from [responseAdapterStorage]. Callers MUST call
// resp.Close() or resp.Release() when finished processing to avoid memory leaks.
func (c *Client) Request(
	ctx context.Context,
	method, path string,
	mods ...aoni.RequestModifier,
) (*Response, error) {
	resolvedURL, err := c.resolveURL(path)
	if err != nil {
		return nil, err
	}

	fastReq := NewRequest(nil)
	defer fastReq.Release()

	if ctx != nil && ctx != context.Background() && ctx != context.TODO() {
		fastReq.SetContext(ctx)
	}

	fastReq.SetMethod(method)
	fastReq.SetURL(resolvedURL)

	for k, vv := range c.cfg.Defaults.Headers {
		for _, v := range vv {
			fastReq.AddHeader(k, v)
		}
	}

	for _, m := range c.cfg.Defaults.DefaultMods {
		m.Apply(fastReq)
	}

	for _, m := range mods {
		m.Apply(fastReq)
	}

	resp, err := c.Do(fastReq)
	if err != nil {
		return nil, err
	}

	if fastResp, ok := resp.(*Response); ok {
		return fastResp, nil
	}

	return toFastResponse(resp), nil
}

func toFastResponse(resp aoni.Response) *Response {
	if resp == nil {
		return nil
	}

	if fastResp, ok := resp.(*Response); ok {
		return fastResp
	}

	fastResp := NewResponse(nil)
	fastResp.resp.SetStatusCode(resp.StatusCode())

	for k, vv := range resp.Headers() {
		for _, v := range vv {
			fastResp.resp.Header.Add(k, v)
		}
	}

	if b := resp.BodyBytes(); len(b) > 0 {
		fastResp.resp.SetBody(b)
	} else if stream := resp.BodyStream(); stream != nil {
		data, err := io.ReadAll(stream)
		if err == nil && len(data) > 0 {
			fastResp.resp.SetBody(data)
		}
	}

	_ = resp.Close()

	return fastResp
}

// Get executes an HTTP GET request against path and returns the [*Response].
func (c *Client) Get(ctx context.Context, path string, mods ...aoni.RequestModifier) (*Response, error) {
	return c.Request(ctx, http.MethodGet, path, mods...)
}

// Head executes an HTTP HEAD request against path to inspect headers without fetching the body.
func (c *Client) Head(ctx context.Context, path string, mods ...aoni.RequestModifier) (*Response, error) {
	return c.Request(ctx, http.MethodHead, path, mods...)
}

// Delete executes an HTTP DELETE request and returns the [*Response].
func (c *Client) Delete(ctx context.Context, path string, mods ...aoni.RequestModifier) (*Response, error) {
	return c.Request(ctx, http.MethodDelete, path, mods...)
}

// Options executes an HTTP OPTIONS request and returns the [*Response].
func (c *Client) Options(ctx context.Context, path string, mods ...aoni.RequestModifier) (*Response, error) {
	return c.Request(ctx, http.MethodOptions, path, mods...)
}

// Fetch executes a request carrying an arbitrary payload body, automatically detecting
// and serializing JSON, Protobuf, URL form values, or raw bytes/streams.
func (c *Client) Fetch(
	ctx context.Context,
	method, path string,
	bodyInput any,
	mods ...aoni.RequestModifier,
) (*Response, error) {
	allMods, err := prepareBodyMods(bodyInput, mods)
	if err != nil {
		return nil, err
	}

	return c.Request(ctx, method, path, allMods...)
}

// Post executes an HTTP POST request carrying body and returns the [*Response].
func (c *Client) Post(ctx context.Context, path string, body any, mods ...aoni.RequestModifier) (*Response, error) {
	return c.Fetch(ctx, http.MethodPost, path, body, mods...)
}

// Put executes an HTTP PUT request carrying body and returns the [*Response].
func (c *Client) Put(ctx context.Context, path string, body any, mods ...aoni.RequestModifier) (*Response, error) {
	return c.Fetch(ctx, http.MethodPut, path, body, mods...)
}

// Patch executes an HTTP PATCH request carrying body and returns the [*Response].
func (c *Client) Patch(ctx context.Context, path string, body any, mods ...aoni.RequestModifier) (*Response, error) {
	return c.Fetch(ctx, http.MethodPatch, path, body, mods...)
}

func prepareBodyMods(bodyInput any, mods []aoni.RequestModifier) ([]aoni.RequestModifier, error) {
	if bodyInput == nil {
		return mods, nil
	}

	payload, err := body.ValidateAndMarshal(bodyInput)
	if err != nil {
		return nil, err
	}

	if payload.IsEmpty() {
		return mods, nil
	}

	totalLen := len(mods) + 1
	if payload.HasContentType() {
		totalLen++
	}

	allMods := make([]aoni.RequestModifier, 0, totalLen)
	if len(payload.Bytes) > 0 {
		allMods = append(allMods, mod.WithBodyBytes(payload.Bytes))
	} else if payload.Reader != nil {
		allMods = append(allMods, mod.WithBody(payload.Reader))
	}

	if payload.HasContentType() {
		allMods = append(allMods, mod.WithContentType(payload.ContentType))
	}

	allMods = append(allMods, mods...)

	return allMods, nil
}

// CloseIdleConnections closes all idle keep-alive connections in the underlying pool.
func (c *Client) CloseIdleConnections() {
	if c.engine != nil {
		c.engine.CloseIdleConnections()
	}
}

// Close releases background connections and engine resources. Safe for repeated calls.
func (c *Client) Close() {
	c.CloseIdleConnections()
}

// DoScoped executes an aoni Request within an auto-releasing [borrow.Scope] context.
func (c *Client) DoScoped(
	req aoni.Request,
	fn func(s *borrow.Scope, resp *Response) error,
) error {
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Close()

	scope := borrow.AcquireScope()
	defer scope.Release()

	fastResp, ok := resp.(*Response)
	if !ok {
		fastResp = toFastResponse(resp)
	}

	return fn(scope, fastResp)
}

// RequestScoped executes an HTTP request within an auto-releasing [borrow.Scope] context.
func (c *Client) RequestScoped(
	ctx context.Context,
	method, path string,
	fn func(s *borrow.Scope, resp *Response) error,
	mods ...aoni.RequestModifier,
) error {
	resp, err := c.Request(ctx, method, path, mods...)
	if err != nil {
		return err
	}
	defer resp.Close()

	scope := borrow.AcquireScope()
	defer scope.Release()

	return fn(scope, resp)
}

// Config returns a clone copy of the active client configuration.
func (c *Client) Config() aoni.Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cfg.Clone()
}

// Middlewares returns a copy of the registered middleware slice.
func (c *Client) Middlewares() []aoni.Middleware {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return slices.Clone(c.middlewares)
}

// AcquireRequest obtains a pooled [Request] instance.
func (c *Client) AcquireRequest() aoni.Request {
	return NewRequest(nil)
}

// ReleaseRequest releases a pooled [Request] instance back to the memory pool.
func (c *Client) ReleaseRequest(req aoni.Request) {
	ReleaseRequest(req)
}

// ReleaseResponse releases a pooled [Response] instance back to the memory pool.
func (c *Client) ReleaseResponse(res aoni.Response) {
	ReleaseResponse(res)
}

// ReleaseRequest releases a request adapter back to the pool.
func ReleaseRequest(req aoni.Request) {
	if r, ok := req.(*Request); ok {
		r.Release()
	}
}

// ReleaseResponse releases a response adapter back to the pool.
func ReleaseResponse(res aoni.Response) {
	if r, ok := res.(*Response); ok {
		r.Release()
	}
}

func toFastRequest(req aoni.Request) (*Request, bool) {
	if fastReq, ok := req.(*Request); ok {
		return fastReq, false
	}

	fastReq := NewRequest(nil)
	fastReq.SetContext(req.Context())
	fastReq.SetMethod(req.Method())

	rawURL := req.URL()
	if rawQuery := req.RawQuery(); rawQuery != "" && !strings.Contains(rawURL, "?") {
		rawURL += "?" + rawQuery
	}

	fastReq.SetURL(rawURL)

	if httpReq := req.HTTPRequest(); httpReq != nil {
		for k, vv := range httpReq.Header {
			for _, v := range vv {
				fastReq.AddHeader(k, v)
			}
		}

		if httpReq.Host != "" {
			fastReq.req.Header.SetHost(httpReq.Host)
		}

		if httpReq.Body != nil && httpReq.Body != http.NoBody {
			fastReq.SetBodyStream(httpReq.Body, httpReq.ContentLength)

			if httpReq.GetBody != nil {
				fastReq.SetGetBody(httpReq.GetBody)
			}
		}
	} else {
		for k, v := range req.Headers() {
			fastReq.AddHeaderBytes(k, v)
		}

		if b := req.BodyBytes(); len(b) > 0 {
			fastReq.SetBodyBytes(b)
		} else if stream := req.BodyStream(); stream != nil {
			fastReq.SetBodyStream(stream, -1)
		}
	}

	fastReq.SetConfig(req.Config())

	return fastReq, true
}
