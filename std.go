// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/fingerprint/h2"
	"github.com/lemon4ksan/aoni/internal/pipeline"
	"github.com/lemon4ksan/aoni/internal/std"
	"github.com/lemon4ksan/aoni/netutil/digest"
)

// StdRequest adapts a standard net/http [*http.Request] to the unified [Request] contract.
type StdRequest = std.Request

// StdResponse adapts a standard net/http [*http.Response] to the unified [Response] contract.
type StdResponse = std.Response

// NewStdRequest wraps a standard *http.Request into a unified [Request] adapter.
func NewStdRequest(req *http.Request) *StdRequest {
	return std.NewRequest(req)
}

// NewStdResponse wraps a standard *http.Response into a unified [Response] adapter.
func NewStdResponse(resp *http.Response) *StdResponse {
	return std.NewResponse(resp)
}

// HTTPDoer specifies the minimal execution contract for processing standard *http.Request transactions.
// It matches the exact signature of standard library [*http.Client.Do].
type HTTPDoer = std.HTTPDoer

// HTTPDoerFunc adapts a plain execution closure to the [HTTPDoer] interface.
type HTTPDoerFunc = std.HTTPDoerFunc

// NewHTTPDoerAdapter wraps doer in a [RequestDoer] adapter. Safe for concurrent execution.
func NewHTTPDoerAdapter(doer HTTPDoer) RequestDoer {
	return std.NewHTTPDoerAdapter(doer)
}

// NewRequestDoerAdapter wraps doer in an [HTTPDoer] adapter. Safe for concurrent execution.
func NewRequestDoerAdapter(doer RequestDoer) HTTPDoer {
	return std.NewRequestDoerAdapter(doer)
}

// ToStdRequest converts a generic [Request] interface into a standard [*http.Request].
func ToStdRequest(req Request) (*http.Request, error) {
	return std.ToHTTPRequest(req)
}

type requesterLike interface {
	Request(ctx context.Context, method, path string, mods ...RequestModifier) (*http.Response, error)
}

type requesterHTTPDoer struct {
	r requesterLike
}

func (d requesterHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil {
		return nil, ErrNilURL
	}

	var mods []RequestModifier
	for k, vv := range req.Header {
		for _, v := range vv {
			mods = append(mods, WithHeader(k, v))
		}
	}

	if req.Body != nil && req.Body != http.NoBody {
		mods = append(mods, WithSmartBody(req.Body))
	}

	return d.r.Request(req.Context(), req.Method, req.URL.String(), mods...)
}

func (d requesterHTTPDoer) Unwrap() any {
	return d.r
}

// DefaultEngine normalizes arbitrary execution targets (RequestDoer, *http.Client, HTTPDoer, or nil)
// into a standardized, production-ready [HTTPDoer] instance.
//
// # Architectural Normalization Pipeline
//
// To support progressive composition and seamless interoperability, DefaultEngine resolves targets
// through a hierarchical evaluation sequence:
//  1. Recursive Unwrapping: If target implements [Unwrapper] or sub-client accessor interfaces
//     ([requesterLike], Rest(), Requester()), traverses nested layers to find the innermost engine.
//  2. Type-Switch Priority:
//     - [RequestDoer]: Wrapped in a zero-allocation adapter via [NewRequestDoerAdapter].
//     - [*http.Client]: Deep-cloned via [CloneHTTPClient] to ensure complete socket pool isolation.
//     - [HTTPDoer]: Accepted directly as an active execution handler (e.g. mocks or closures).
//  3. Production-Hardened Fallback: If target is nil, constructs a fresh [*http.Client] equipped
//     with a 15s request timeout, 10-hop redirect bounds, HTTP/2 force-attempt, and environment proxy resolution.
func DefaultEngine(doer any) HTTPDoer {
	if doer == nil {
		return &http.Client{
			Timeout:       15 * time.Second,
			CheckRedirect: DefaultRedirectPolicy(10),
			Transport:     newDefaultTransport(),
		}
	}

	if unwrapper, ok := doer.(interface{ Unwrap() any }); ok {
		if inner := unwrapper.Unwrap(); inner != nil && inner != doer {
			return DefaultEngine(inner)
		}
	}

	if rd, ok := doer.(interface{ Rest() any }); ok {
		if inner := rd.Rest(); inner != nil && inner != doer {
			return DefaultEngine(inner)
		}
	}

	if rd, ok := doer.(interface{ Requester() any }); ok {
		if inner := rd.Requester(); inner != nil && inner != doer {
			return DefaultEngine(inner)
		}
	}

	if reqLike, ok := doer.(requesterLike); ok && reqLike != nil {
		return requesterHTTPDoer{r: reqLike}
	}

	switch doer := doer.(type) {
	case RequestDoer:
		if doer != nil {
			return NewRequestDoerAdapter(doer)
		}
	case *http.Client:
		if doer != nil {
			return CloneHTTPClient(doer)
		}
	case HTTPDoer:
		if doer != nil {
			return doer
		}
	}

	return &http.Client{
		Timeout:       15 * time.Second,
		CheckRedirect: DefaultRedirectPolicy(10),
		Transport:     newDefaultTransport(),
	}
}

func newDefaultTransport() *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		// RFC 8996 §4, §5 & RFC 7525 (BCP 195): Deprecating TLS 1.0 and TLS 1.1. Minimum version MUST be TLS 1.2.
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// TransportUnwrapper is implemented by RoundTripper decorators that wrap an inner RoundTripper.
type TransportUnwrapper interface {
	Unwrap() http.RoundTripper
}

// TransportCloner is implemented by RoundTripper decorators capable of creating a deep-copy
// of themselves around a newly cloned inner RoundTripper.
type TransportCloner interface {
	CloneTransport(next http.RoundTripper) http.RoundTripper
}

// CloneHTTPClient produces a deep, memory-isolated copy of an [*http.Client] and its nested transport layers.
//
// Recursive Isolation Invariants:
// In standard Go, performing `*c = *parent` leaves the underlying [http.Transport] and TLS configurations
// shared, resulting in socket pool collisions and race hazards when modifying options concurrently.
//
// CloneHTTPClient recursively traverses nested decorator layers (via [TransportCloner] and [TransportUnwrapper]),
// cloning base [*http.Transport] instances ([http.Transport.Clone]) and TLS configurations to ensure
// that the cloned client is completely decoupled from the original.
// If c is nil, returns nil.
func CloneHTTPClient(c *http.Client) *http.Client {
	if c == nil {
		return nil
	}

	cloned := *c
	if cloned.Transport == nil {
		return &cloned
	}

	cloned.Transport = cloneRoundTripper(cloned.Transport)

	return &cloned
}

func cloneRoundTripper(tr http.RoundTripper) http.RoundTripper {
	if tr == nil {
		return nil
	}

	if cloner, ok := tr.(TransportCloner); ok {
		if unwrapper, ok := tr.(TransportUnwrapper); ok {
			nextCloned := cloneRoundTripper(unwrapper.Unwrap())
			return cloner.CloneTransport(nextCloned)
		}
	}

	if unwrapper, ok := tr.(TransportUnwrapper); ok {
		return cloneRoundTripper(unwrapper.Unwrap())
	}

	if baseTr, ok := tr.(*http.Transport); ok && baseTr != nil {
		return baseTr.Clone()
	}

	return tr
}

// NewStdClient adapts an aoni [Client] contract into a standard [*http.Client].
// Outgoing requests executed via this standard client will transparently pass through
// the entire aoni pipeline, including uTLS browser impersonation, proxy rotators, and retries.
func NewStdClient(c *Client) *http.Client {
	return &http.Client{
		Transport: NewTransport(c),
		Jar:       nil,
	}
}

// NewTransport constructs an [http.RoundTripper] (as a [*Transport]) configured
// to route all outgoing requests through the provided aoni [Client] pipeline.
func NewTransport(c *Client) *Transport {
	return &Transport{client: c}
}

// Transport implements the standard [http.RoundTripper] interface, intercepting
// outbound requests from standard library consumers (e.g. AWS SDK, Google Cloud SDK, Stripe SDK)
// and executing them through an active aoni [Client] pipeline.
//
// # Automatic URL & Scheme Correction
//
// If a request specifies a host without a scheme (e.g. `req.URL.Host != "" && req.URL.Scheme == ""`),
// Transport automatically normalizes the scheme to "https" to prevent routing failures.
type Transport struct {
	client *Client

	// BeforeRoundTrip is an optional interceptor hook invoked immediately before a request
	// enters the aoni pipeline, allowing dynamic per-request client cloning and modifier injection.
	BeforeRoundTrip func(cloned *Client, origReq *http.Request) *Client
}

// Unwrap returns the underlying aoni [*Client].
func (t *Transport) Unwrap() *Client {
	return t.client
}

// RoundTrip satisfies [http.RoundTripper] by executing requests through the aoni pipeline.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil {
		closeReqBody(req)

		var op string
		if req != nil {
			op = req.Method
		}

		return nil, &url.Error{
			Op:  op,
			Err: ErrNilURL,
		}
	}

	if req.URL.Host != "" && req.URL.Scheme == "" {
		u := *req.URL
		u.Scheme = "https"
		reqClone := req.Clone(req.Context())
		reqClone.URL = &u
		req = reqClone
	}

	activeClient := t.client
	if t.BeforeRoundTrip != nil {
		activeClient = t.BeforeRoundTrip(t.client.Clone(), req)
	}

	resp, err := activeClient.HTTP().Do(req)
	if err != nil {
		return nil, t.wrapError(req, err)
	}

	return resp, nil
}

func (t *Transport) wrapError(req *http.Request, err error) error {
	closeReqBody(req)

	reqURL := req.URL.String()
	bridgeErr := &BridgeError{
		Op:  req.Method,
		URL: reqURL,
		Err: err,
		Metadata: map[string]any{
			"host":   req.URL.Host,
			"scheme": req.URL.Scheme,
		},
	}

	return &url.Error{
		Op:  req.Method,
		URL: reqURL,
		Err: bridgeErr,
	}
}

func closeReqBody(req *http.Request) {
	if req != nil && req.Body != nil {
		_ = req.Body.Close()
	}
}

var _ http.RoundTripper = (*Transport)(nil)

func (c *Client) reapplyH2Settings(tr *http.Transport) {
	if tr == nil {
		return
	}

	needsH2 := c.cfg.Fingerprint.H2Configurer != nil ||
		c.cfg.Fingerprint.H2Settings != nil ||
		c.cfg.Fingerprint.BrowserID != BrowserNone ||
		c.cfg.Fingerprint.TLSClientHelloID != nil ||
		c.cfg.Fingerprint.TLSClientHelloSpecProvider != nil

	if !needsH2 {
		return
	}

	settings := h2.ChromeSettings
	if c.cfg.Fingerprint.H2Settings != nil {
		settings = *c.cfg.Fingerprint.H2Settings
	} else if c.cfg.Fingerprint.BrowserID == BrowserFirefox {
		settings = h2.FirefoxSettings
	}

	framed := h2.NewFramedTransport(tr, settings, c.cfg.Fingerprint.HeaderOrder...)
	if c.cfg.Fingerprint.H2Configurer != nil && framed.H2Transport() != nil {
		_ = c.cfg.Fingerprint.H2Configurer.ConfigureHTTP2(framed.H2Transport())
	}

	if httpClient, ok := c.engine.(*http.Client); ok {
		if cjTrans, ok := httpClient.Transport.(*cookie.Transport); ok {
			cjTrans.Next = framed
		} else {
			httpClient.Transport = framed
		}
	}
}

func applyEngineConfig(c *Client, eng EngineConfig) {
	if eng.CustomEngine != nil {
		if httpClient, ok := eng.CustomEngine.(*http.Client); ok {
			c.engine = CloneHTTPClient(httpClient)
		} else {
			c.engine = eng.CustomEngine
			return
		}
	}

	httpClient, ok := c.engine.(*http.Client)
	if !ok {
		return
	}

	if httpClient.Transport == nil {
		httpClient.Transport = newDefaultTransport()
	}

	if eng.Timeout > 0 {
		httpClient.Timeout = eng.Timeout
	}

	applyRedirectPolicy(httpClient, eng)
	applyCookieJar(httpClient, eng.CookieJar)
	applyDigestAuth(httpClient, eng.DigestAuth)
	applyTransportOverrides(c, eng)
}

func applyCookieJar(httpClient *http.Client, jar http.CookieJar) {
	if jar == nil {
		return
	}

	httpClient.Jar = jar

	pJar, ok := jar.(*cookie.ProxyIsolatedJar)
	if !ok {
		return
	}

	baseTr := httpClient.Transport
	if baseTr == nil {
		baseTr = http.DefaultTransport
	}

	if cjTrans, ok := baseTr.(*cookie.Transport); ok {
		baseTr = cjTrans.Unwrap()
	}

	httpClient.Transport = &cookie.Transport{Next: baseTr, CookieJar: pJar}
}

func applyDigestAuth(httpClient *http.Client, digestCfg *DigestAuthConfig) {
	if digestCfg == nil || digestCfg.Username == "" {
		return
	}

	baseTr := httpClient.Transport
	if baseTr == nil {
		baseTr = http.DefaultTransport
	}

	if dt, ok := baseTr.(*digest.Transport); ok {
		baseTr = dt.Unwrap()
	}

	httpClient.Transport = &digest.Transport{
		Username:  digestCfg.Username,
		Password:  digestCfg.Password,
		Transport: baseTr,
	}
}

func applyTransportOverrides(c *Client, eng EngineConfig) {
	tr := c.Transport()
	if tr == nil {
		return
	}

	var poolDTO *pipeline.ConnectionPoolConfigDTO
	if pool := eng.ConnectionPool; pool != nil {
		poolDTO = &pipeline.ConnectionPoolConfigDTO{
			MaxIdleConns:          pool.MaxIdleConns,
			MaxIdleConnsPerHost:   pool.MaxIdleConnsPerHost,
			MaxConnsPerHost:       pool.MaxConnsPerHost,
			IdleConnTimeout:       pool.IdleConnTimeout,
			ResponseHeaderTimeout: pool.ResponseHeaderTimeout,
			ReadBufferSize:        pool.ReadBufferSize,
			WriteBufferSize:       pool.WriteBufferSize,
		}
	}

	var h2DTO *pipeline.HTTP2ConfigDTO
	if h2Cfg := eng.HTTP2Config; h2Cfg != nil {
		h2DTO = &pipeline.HTTP2ConfigDTO{
			ReadIdleTimeout: h2Cfg.ReadIdleTimeout,
			PingTimeout:     h2Cfg.PingTimeout,
			AllowHTTP:       h2Cfg.AllowHTTP,
		}
	}

	pipeline.ApplyTransportOverrides(tr, eng.InsecureSkipVerify, poolDTO, h2DTO)
}
