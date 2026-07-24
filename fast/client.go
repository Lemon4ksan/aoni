// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/bytesconv"
)

// ErrTargetURLEmpty is returned when no destination address is specified in a request path or base URL configuration.
var ErrTargetURLEmpty = errors.New("aoni fast: target URL is empty")

// Client executes high-performance HTTP transactions over a pooled [*fasthttp.Client] engine.
type Client struct {
	engine *fasthttp.Client
	config aoni.Config
}

// Option is an alias for [aoni.ClientOption].
type Option = aoni.ClientOption

// NewClient instantiates a new [Client] with fasthttp parameters configured via [aoni.ClientOption] options.
func NewClient(opts ...aoni.ClientOption) *Client {
	c := &Client{
		engine: &fasthttp.Client{
			ReadTimeout:         15 * time.Second,
			WriteTimeout:        15 * time.Second,
			MaxConnsPerHost:     512,
			MaxIdleConnDuration: 90 * time.Second,
		},
		config: aoni.Config{Defaults: aoni.ClientDefaults{Headers: make(http.Header)}},
	}

	for _, opt := range opts {
		if opt != nil {
			opt(&c.config)
		}
	}

	if c.config.Engine.Timeout > 0 {
		c.engine.ReadTimeout = c.config.Engine.Timeout
		c.engine.WriteTimeout = c.config.Engine.Timeout
	}

	return c
}

// Config returns a copy of the active client configuration struct.
func (c *Client) Config() aoni.Config {
	return c.config
}

// Engine returns the underlying [*fasthttp.Client] instance.
func (c *Client) Engine() *fasthttp.Client {
	return c.engine
}

// Request executes a transaction using fasthttp objects acquired from [sync.Pool].
//
// Postconditions:
//   - The returned [aoni.Response] MUST be closed via [aoni.Response.Close] to return acquired objects to [sync.Pool].
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

	if c.config.Defaults.Headers != nil {
		for k, vv := range c.config.Defaults.Headers {
			if reqAdapter.Header(k) == "" && len(vv) > 0 {
				reqAdapter.SetHeader(k, vv[0])
			}
		}
	}

	for _, defaultMod := range c.config.Defaults.DefaultMods {
		if defaultMod != nil {
			defaultMod(reqAdapter)
		}
	}

	for _, m := range mods {
		if m != nil {
			m(reqAdapter)
		}
	}

	if err := c.executeFastHTTP(ctx, fastReq, fastResp); err != nil {
		fasthttp.ReleaseRequest(fastReq)
		fasthttp.ReleaseResponse(fastResp)

		return nil, err
	}

	return NewPooledResponse(fastReq, fastResp), nil
}

// Do executes a prepared [aoni.Request] via fasthttp, applying defaults and modifiers.
func (c *Client) Do(req aoni.Request) (aoni.Response, error) {
	if req == nil {
		req = NewRequest(nil)
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
	_ aoni.Response     = (*PooledResponse)(nil)
	_ http.RoundTripper = (*Transport)(nil)
	_ aoni.RequestDoer  = (*Client)(nil)
)
