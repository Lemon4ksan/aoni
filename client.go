// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	utls "github.com/refraction-networking/utls"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/transform"
)

// UserAgent is a default User-Agent string used for HTTP requests.
const UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

var (
	bytePool = sync.Pool{
		New: func() any {
			b := make([]byte, 32*1024)
			return &b
		},
	}
	bufferPool = sync.Pool{
		New: func() any {
			return new(bytes.Buffer)
		},
	}
)

var defaultSensitiveHeaders = []string{
	"Authorization",
	"Cookie",
	"X-Session-ID",
	"X-Access-Token",
	"X-Access-Key",
	"X-Api-Key",
	"X-Auth-Token",
}

type (
	capturerCtxKey           struct{}
	decoderCtxKey            struct{}
	errorModelCtxKey         struct{}
	downloadProgressCtxKey   struct{}
	hedgingCtxKey            struct{}
	queryErrorCtxKey         struct{}
	bodyErrorCtxKey          struct{}
	happyEyeballsDelayCtxKey struct{}
	multiReadCtxKey          struct{}
	ssrfGuardCtxKey          struct{}
)

// HTTPDoer is an interface for objects that can execute an [http.Request].
// It is satisfied by [http.Client].
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// DoerFunc is a function type that implements HTTPDoer.
type DoerFunc func(req *http.Request) (*http.Response, error)

// Do implements the HTTPDoer interface for DoerFunc.
func (f DoerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

// Requester defines the requirements for performing raw HTTP requests
// with path joining and query parameter encoding.
type Requester interface {
	Request(
		ctx context.Context,
		method, path string,
		mods ...RequestModifier,
	) (*http.Response, error)
}

// BaseResponseProvider is an optional interface that a Requester can implement
// to provide a BaseResponse wrapper for JSON requests.
type BaseResponseProvider interface {
	BaseResponse() BaseResponse
}

// ProgressFunc is a callback function to track upload or download progress.
// total represents the content length (-1 if unknown).
type ProgressFunc func(current, total int64)

// BaseResponse is an interface for response wrappers that include
// status information and a data payload.
//
// If a Client is configured with a BaseResponse provider, it will
// automatically unwrap the response and check for success.
type BaseResponse interface {
	// IsSuccess returns true if the response indicates a successful operation,
	// even if the HTTP status code is 200.
	IsSuccess() bool

	// Error returns an error if IsSuccess is false.
	Error() error

	// SetData provides a pointer where the data payload should be decoded.
	// This is called by the client before unmarshaling the JSON body.
	SetData(data any)
}

// Client is a concrete implementation of the [Requester] interface.
// It maintains a base URL and a set of default headers applied to every request.
//
// Create new instances of Client using the [NewClient] constructor.
type Client struct {
	http               HTTPDoer
	baseURL            *url.URL
	headers            http.Header
	baseResponse       func() BaseResponse
	hedgingDelay       time.Duration
	beforeRequest      []func(req *http.Request)
	afterResponse      []func(resp *http.Response, err error)
	maxResponseSize    int64
	ssrfGuard          bool
	happyEyeballsDelay time.Duration
	multiReadThreshold int64
}

// NewClient initializes a aoni client.
// If httpClient is nil, a default http.Client with a 15-second timeout and sensitive header scrubbing redirect policy is used.
func NewClient(httpClient HTTPDoer) *Client {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout:       15 * time.Second,
			CheckRedirect: redirectPolicy(10),
		}
	}

	c := &Client{
		http:               httpClient,
		baseURL:            &url.URL{},
		headers:            make(http.Header),
		maxResponseSize:    10 * 1024 * 1024,
		happyEyeballsDelay: 300 * time.Millisecond,
		multiReadThreshold: 0,
	}

	c.applyDialers()

	return c.WithUserAgent(UserAgent)
}

func (c *Client) applyDialers() {
	if httpClient, ok := c.http.(*http.Client); ok {
		transport := c.Transport()
		if transport != nil {
			clonedTransport := transport.Clone()
			clonedTransport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				return happyEyeballsDial(ctx, network, addr, c.happyEyeballsDelay, c.ssrfGuard)
			}
			clonedClient := *httpClient
			clonedClient.Transport = clonedTransport
			c.http = &clonedClient
		}
	}
}

// WithBaseResponse returns a new Client instance that uses the provided
// function to create a BaseResponse wrapper for every JSON request.
func (c *Client) WithBaseResponse(provider func() BaseResponse) *Client {
	newClient := c.Clone()
	newClient.baseResponse = provider
	return newClient
}

// WithBaseURL returns a new Client instance with the specified base URL.
// It ensures the base URL has exactly one trailing slash to make
// url.ResolveReference work correctly with relative paths.
func (c *Client) WithBaseURL(raw string) *Client {
	if raw == "" {
		newClient := c.Clone()
		newClient.baseURL = &url.URL{}
		return newClient
	}

	if !strings.HasSuffix(raw, "/") {
		raw += "/"
	}

	baseURL, _ := url.Parse(raw)

	newClient := c.Clone()
	newClient.baseURL = baseURL

	return newClient
}

// WithHeader returns a new Client instance with an additional default header.
// This follows the immutable/chaining pattern.
func (c *Client) WithHeader(key, value string) *Client {
	newClient := c.Clone()
	newClient.headers.Set(key, value)
	return newClient
}

// WithTimeout returns a new Client instance with the specified timeout.
// This only works if the underlying HTTPDoer is an *http.Client.
func (c *Client) WithTimeout(d time.Duration) *Client {
	newClient := c.Clone()
	if httpClient, ok := newClient.http.(*http.Client); ok {
		cloned := *httpClient
		cloned.Timeout = d
		newClient.http = &cloned
	}

	return newClient
}

// WithRedirectLimit returns a new Client instance with a custom redirect policy.
// If max is 0, redirects are disabled. If max < 0, default Go behavior is used.
func (c *Client) WithRedirectLimit(max int) *Client {
	newClient := c.Clone()
	if httpClient, ok := newClient.http.(*http.Client); ok {
		cloned := *httpClient
		switch {
		case max == 0:
			cloned.CheckRedirect = func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			}
		case max > 0:
			cloned.CheckRedirect = redirectPolicy(max)
		default:
			cloned.CheckRedirect = redirectPolicy(10)
		}

		newClient.http = &cloned
	}

	return newClient
}

// WithLocalAddr returns a new Client instance bound to a specific local IP address.
// This only works if the underlying HTTPDoer is an *http.Client with an *http.Transport.
func (c *Client) WithLocalAddr(addr string) *Client {
	newClient := c.Clone()
	if httpClient, ok := newClient.http.(*http.Client); ok {
		if transport, ok := httpClient.Transport.(*http.Transport); ok {
			clonedTransport := transport.Clone()

			localAddr, err := net.ResolveIPAddr("ip", addr)
			if err == nil {
				clonedTransport.DialContext = (&net.Dialer{
					LocalAddr: &net.TCPAddr{IP: localAddr.IP},
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext
			}

			clonedClient := *httpClient
			clonedClient.Transport = clonedTransport
			newClient.http = &clonedClient
		}
	}

	return newClient
}

// WithHedging returns a new Client instance with the specified default hedging delay.
// Hedging is disabled if delay <= 0.
func (c *Client) WithHedging(d time.Duration) *Client {
	newClient := c.Clone()
	newClient.hedgingDelay = d
	return newClient
}

// WithMaxResponseSize returns a new Client instance with the specified maximum response size limit.
// Set to <= 0 to disable response size limits.
func (c *Client) WithMaxResponseSize(size int64) *Client {
	newClient := c.Clone()
	newClient.maxResponseSize = size
	return newClient
}

// WithSSRFGuard returns a new Client instance with SSRF request guarding enabled.
// If enabled, requests to loopback or private network IP addresses are blocked.
func (c *Client) WithSSRFGuard() *Client {
	newClient := c.Clone()
	newClient.ssrfGuard = true
	newClient.applyDialers()

	return newClient
}

// WithHappyEyeballs returns a new Client instance with custom Happy Eyeballs staggered dial delay.
// Set to <= 0 to disable staggered dialing (Happy Eyeballs is enabled by default with 300ms delay).
func (c *Client) WithHappyEyeballs(delay time.Duration) *Client {
	newClient := c.Clone()
	newClient.happyEyeballsDelay = delay
	newClient.applyDialers()

	return newClient
}

// WithMultiReadBody returns a RequestModifier that sets the multi-read re-readability buffer threshold for a single request.
func WithMultiReadBody(threshold int64) RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), multiReadCtxKey{}, threshold)
		*req = *req.WithContext(ctx)
	}
}

// WithMultiReadBody returns a new Client instance with the specified default response body re-readability threshold.
// Set to > 0 to cache response bodies (in memory if size <= threshold, on disk if size > threshold).
func (c *Client) WithMultiReadBody(threshold int64) *Client {
	newClient := c.Clone()
	newClient.multiReadThreshold = threshold
	return newClient
}

// WithBeforeRequest returns a new Client instance with the given hook added to the before-request hooks.
func (c *Client) WithBeforeRequest(hook func(req *http.Request)) *Client {
	newClient := c.Clone()
	newClient.beforeRequest = append(newClient.beforeRequest, hook)
	return newClient
}

// WithAfterResponse returns a new Client instance with the given hook added to the after-response hooks.
func (c *Client) WithAfterResponse(hook func(resp *http.Response, err error)) *Client {
	newClient := c.Clone()
	newClient.afterResponse = append(newClient.afterResponse, hook) //nolint:bodyclose
	return newClient
}

// WithUserAgent returns a new Client instance with a custom User-Agent header.
func (c *Client) WithUserAgent(ua string) *Client {
	return c.WithHeader("User-Agent", ua)
}

// UserAgent returns the default User-Agent header value configured on the client.
func (c *Client) UserAgent() string {
	return c.headers.Get("User-Agent")
}

// WithOrigin returns a new Client instance with a custom Origin header.
func (c *Client) WithOrigin(origin string) *Client {
	return c.WithHeader("Origin", origin)
}

// WithCookieJar returns a new Client instance with the specified cookie jar.
// This only works if the underlying HTTPDoer is an *http.Client.
func (c *Client) WithCookieJar(jar http.CookieJar) *Client {
	newClient := c.Clone()
	if httpClient, ok := newClient.http.(*http.Client); ok {
		cloned := *httpClient
		cloned.Jar = jar
		newClient.http = &cloned
	}

	return newClient
}

// BaseResponse returns a new BaseResponse wrapper if a provider is configured.
func (c *Client) BaseResponse() BaseResponse {
	if c.baseResponse == nil {
		return nil
	}

	return c.baseResponse()
}

// HTTP returns the underlying [HTTPDoer].
func (c *Client) HTTP() HTTPDoer {
	return c.http
}

// Transport returns the underlying *http.Transport of the client if the HTTPDoer
// is an *http.Client and its Transport is an *http.Transport.
// Otherwise, it returns nil.
func (c *Client) Transport() *http.Transport {
	if httpClient, ok := c.http.(*http.Client); ok {
		if httpClient.Transport == nil {
			httpClient.Transport = &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				ForceAttemptHTTP2:     true,
				MaxIdleConns:          100,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			}
		}

		if transport, ok := httpClient.Transport.(*http.Transport); ok {
			return transport
		}
	}

	return nil
}

// ConnectionPoolConfig defines options for tuning the client's HTTP transport pool.
type ConnectionPoolConfig struct {
	MaxIdleConns          int
	MaxIdleConnsPerHost   int
	MaxConnsPerHost       int
	IdleConnTimeout       time.Duration
	ResponseHeaderTimeout time.Duration
}

// WithConnectionPool returns a new Client instance with the connection pool tuned to the given configuration.
// This only works if the client is using *http.Client with an *http.Transport.
func (c *Client) WithConnectionPool(cfg ConnectionPoolConfig) *Client {
	newClient := c.Clone()
	if httpClient, ok := newClient.http.(*http.Client); ok {
		clonedClient := *httpClient

		transport := c.Transport()
		if transport != nil {
			clonedTransport := transport.Clone()

			if cfg.MaxIdleConns > 0 {
				clonedTransport.MaxIdleConns = cfg.MaxIdleConns
			}

			if cfg.MaxIdleConnsPerHost > 0 {
				clonedTransport.MaxIdleConnsPerHost = cfg.MaxIdleConnsPerHost
			}

			if cfg.MaxConnsPerHost > 0 {
				clonedTransport.MaxConnsPerHost = cfg.MaxConnsPerHost
			}

			if cfg.IdleConnTimeout > 0 {
				clonedTransport.IdleConnTimeout = cfg.IdleConnTimeout
			}

			if cfg.ResponseHeaderTimeout > 0 {
				clonedTransport.ResponseHeaderTimeout = cfg.ResponseHeaderTimeout
			}

			clonedClient.Transport = clonedTransport
		}

		newClient.http = &clonedClient
	}

	return newClient
}

// CloseIdleConnections closes any connections which were previously connected from previous requests
// but are now sitting idle in a "keep-alive" state.
func (c *Client) CloseIdleConnections() {
	if httpClient, ok := c.http.(*http.Client); ok {
		httpClient.CloseIdleConnections()
	}
}

// BrowserID defines browser TLS fingerprints for JA3 evasion.
type BrowserID int

// Possible browser ids.
const (
	BrowserNone BrowserID = iota
	BrowserChrome
	BrowserFirefox
	BrowserSafari
)

// WithTLSFingerprint returns a new Client instance that configures the underlying transport
// to use uTLS for JA3 signature evasion matching the specified browser.
func (c *Client) WithTLSFingerprint(browser BrowserID) *Client {
	newClient := c.Clone()
	if browser == BrowserNone {
		return newClient
	}

	if httpClient, ok := newClient.http.(*http.Client); ok {
		transport := newClient.Transport()
		if transport != nil {
			clonedTransport := transport.Clone()
			clonedTransport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialTLSWithUTLS(ctx, network, addr, browser)
			}
			clonedClient := *httpClient
			clonedClient.Transport = clonedTransport
			newClient.http = &clonedClient
		}
	}

	return newClient
}

func dialTLSWithUTLS(ctx context.Context, network, addr string, browser BrowserID) (net.Conn, error) {
	ssrfGuard := ctx.Value(ssrfGuardCtxKey{}) != nil

	var delay time.Duration
	if val := ctx.Value(happyEyeballsDelayCtxKey{}); val != nil {
		delay = val.(time.Duration)
	} else {
		delay = 300 * time.Millisecond
	}

	conn, err := happyEyeballsDial(ctx, network, addr, delay, ssrfGuard)
	if err != nil {
		return nil, err
	}

	var spec utls.ClientHelloID
	switch browser {
	case BrowserFirefox:
		spec = utls.HelloFirefox_Auto
	case BrowserSafari:
		spec = utls.HelloSafari_Auto
	default:
		spec = utls.HelloChrome_Auto
	}

	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}

	uConn := utls.UClient(conn, &utls.Config{ServerName: host}, spec)
	if err := uConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}

	return uConn, nil
}

// Clone returns a deep copy of the Client instance.
func (c *Client) Clone() *Client {
	beforeCopy := make([]func(req *http.Request), len(c.beforeRequest))
	copy(beforeCopy, c.beforeRequest)

	afterCopy := make([]func(resp *http.Response, err error), len(c.afterResponse))
	copy(afterCopy, c.afterResponse)

	return &Client{
		http:               c.http,
		baseURL:            c.baseURL,
		headers:            c.headers.Clone(),
		baseResponse:       c.baseResponse,
		hedgingDelay:       c.hedgingDelay,
		beforeRequest:      beforeCopy,
		afterResponse:      afterCopy,
		maxResponseSize:    c.maxResponseSize,
		ssrfGuard:          c.ssrfGuard,
		happyEyeballsDelay: c.happyEyeballsDelay,
		multiReadThreshold: c.multiReadThreshold,
	}
}

// Request builds and executes an HTTP request.
// The path is joined with the client's base URL, and query values are appended to the URL.
func (c *Client) Request(
	ctx context.Context,
	method, path string,
	mods ...RequestModifier,
) (*http.Response, error) {
	if c.ssrfGuard {
		ctx = context.WithValue(ctx, ssrfGuardCtxKey{}, true)
	}

	ctx = context.WithValue(ctx, happyEyeballsDelayCtxKey{}, c.happyEyeballsDelay)

	rel, err := url.Parse(strings.TrimLeft(path, "/"))
	if err != nil {
		return nil, fmt.Errorf("aoni: invalid path: %w", err)
	}

	u := c.baseURL.ResolveReference(rel)

	req, err := http.NewRequestWithContext(ctx, method, u.String(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("aoni: failed to create request: %w", err)
	}

	maps.Copy(req.Header, c.headers)

	if req.Header.Get("Accept-Encoding") == "" {
		req.Header.Set("Accept-Encoding", "zstd, br, gzip")
	}

	for _, mod := range mods {
		if mod != nil {
			mod(req)
		}
	}

	if errVal := req.Context().Value(queryErrorCtxKey{}); errVal != nil {
		if serializationErr, ok := errVal.(error); ok {
			return nil, fmt.Errorf("aoni: query encoding failed: %w", serializationErr)
		}
	}

	for _, hook := range c.beforeRequest {
		hook(req)
	}

	var (
		resp   *http.Response
		reqErr error
	)

	var hedgingDelay time.Duration
	if req.Context().Value(hedgingCtxKey{}) != nil {
		hedgingDelay = req.Context().Value(hedgingCtxKey{}).(time.Duration)
	} else {
		hedgingDelay = c.hedgingDelay
	}

	if hedgingDelay > 0 {
		resp, reqErr = c.executeWithHedging(ctx, hedgingDelay, req)
	} else {
		resp, reqErr = c.http.Do(req)
	}

	for _, hook := range c.afterResponse {
		hook(resp, reqErr)
	}

	if reqErr != nil {
		return nil, fmt.Errorf("aoni: request failed: %w", reqErr)
	}

	if resp != nil && resp.Body != nil {
		// 1. Download progress callback
		if onProgress, ok := req.Context().Value(downloadProgressCtxKey{}).(ProgressFunc); ok && onProgress != nil {
			resp.Body = &progressReader{
				reader:     resp.Body,
				total:      resp.ContentLength,
				onProgress: onProgress,
			}
		}

		// 2. Smart decompression (zstd, br, gzip)
		switch resp.Header.Get("Content-Encoding") {
		case "br":
			resp.Body = &decompressReadCloser{
				Reader: brotli.NewReader(resp.Body),
				closer: resp.Body,
			}
			resp.Header.Del("Content-Encoding")
			resp.Header.Del("Content-Length")
			resp.ContentLength = -1

		case "zstd":
			zstdDec, err := zstd.NewReader(resp.Body)
			if err == nil {
				resp.Body = &decompressReadCloser{
					Reader: zstdDec,
					closer: resp.Body,
				}
				resp.Header.Del("Content-Encoding")
				resp.Header.Del("Content-Length")
				resp.ContentLength = -1
			}

		case "gzip":
			gzReader, err := gzip.NewReader(resp.Body)
			if err == nil {
				resp.Body = &decompressReadCloser{
					Reader: gzReader,
					closer: resp.Body,
				}
				resp.Header.Del("Content-Encoding")
				resp.Header.Del("Content-Length")
				resp.ContentLength = -1
			}
		}

		// 3. Automatic transcoding from non-UTF8 charset
		if contentType := resp.Header.Get("Content-Type"); contentType != "" {
			if _, params, err := mime.ParseMediaType(contentType); err == nil {
				if charset := params["charset"]; charset != "" {
					charset = strings.ToLower(charset)
					if charset != "utf-8" && charset != "utf8" {
						if enc, err := htmlindex.Get(charset); err == nil {
							resp.Body = struct {
								io.Reader
								io.Closer
							}{
								Reader: transform.NewReader(resp.Body, enc.NewDecoder()),
								Closer: resp.Body,
							}
						}
					}
				}
			}
		}
	}

	if resp != nil && resp.Body != nil {
		// 4. Response size limit checking
		if c.maxResponseSize > 0 {
			if resp.ContentLength > c.maxResponseSize {
				_ = resp.Body.Close()
				return nil, fmt.Errorf("aoni: response too large: %w", ErrResponseTooLarge)
			}

			resp.Body = &limitCheckingReadCloser{
				ReadCloser: resp.Body,
				limit:      c.maxResponseSize,
			}
		}

		// 4.5. Replayable multi-read body caching
		var multiReadThreshold int64
		if val := req.Context().Value(multiReadCtxKey{}); val != nil {
			multiReadThreshold = val.(int64)
		} else {
			multiReadThreshold = c.multiReadThreshold
		}

		if multiReadThreshold > 0 {
			mBody, err := newMultiReadBody(resp.Body, multiReadThreshold)
			if err == nil {
				resp.Body = mBody
			}
		}

		// 5. Socket leak prevention via GC Finalizer
		resp.Body = newFinalizerReadCloser(resp.Body)
	}

	return resp, nil
}

func (c *Client) executeWithHedging(
	ctx context.Context,
	delay time.Duration,
	req *http.Request,
) (*http.Response, error) {
	type result struct {
		resp *http.Response
		err  error
	}

	resultsCh := make(chan result, 2)
	ctx1, cancel1 := context.WithCancel(ctx)
	ctx2, cancel2 := context.WithCancel(ctx)

	// Track clean up state
	var (
		cleaned bool
		mu      sync.Mutex
	)

	cleanup := func(winner int) {
		mu.Lock()
		defer mu.Unlock()

		if cleaned {
			return
		}

		cleaned = true

		switch winner {
		case 1:
			cancel2()
		case 2:
			cancel1()
		default:
			cancel1()
			cancel2()
		}
	}

	defer func() {
		// Fallback cleanup in case of error/panic
		cleanup(0)
	}()

	cloneReq := func(orig *http.Request, reqCtx context.Context) (*http.Request, error) {
		cloned := orig.Clone(reqCtx)
		if orig.Body != nil && orig.Body != http.NoBody {
			if orig.GetBody != nil {
				body, err := orig.GetBody()
				if err != nil {
					return nil, err
				}

				cloned.Body = body
			} else {
				return nil, errors.New("aoni: request body cannot be duplicated for hedging")
			}
		}

		return cloned, nil
	}

	req1, err := cloneReq(req, ctx1)
	if err != nil {
		return nil, err
	}

	go func() {
		resp, err := c.http.Do(req1) //nolint:bodyclose
		resultsCh <- result{resp: resp, err: err}
	}()

	timer := time.NewTimer(delay)
	defer timer.Stop()

	var (
		req2Started bool
		firstErr    error
	)

	activeCount := 1

	for activeCount > 0 {
		select {
		case res := <-resultsCh:
			activeCount--

			if res.err == nil {
				winner := 1

				cancelWinner := cancel1
				if res.resp.Request != nil && res.resp.Request.Context() == ctx2 {
					winner = 2
					cancelWinner = cancel2
				}

				cleanup(winner)

				res.resp.Body = &contextCancelingReadCloser{
					ReadCloser: res.resp.Body,
					cancel:     cancelWinner,
				}

				return res.resp, nil
			}

			if firstErr == nil {
				firstErr = res.err
			}

			if activeCount == 0 && !req2Started {
				timer.Stop()

				select {
				case <-timer.C:
				default:
				}

				req2Started = true

				req2, err := cloneReq(req, ctx2)
				if err != nil {
					return nil, err
				}

				activeCount++

				go func() {
					resp, err := c.http.Do(req2) //nolint:bodyclose
					resultsCh <- result{resp: resp, err: err}
				}()
			}

		case <-timer.C:
			if !req2Started {
				req2Started = true

				req2, err := cloneReq(req, ctx2)
				if err != nil {
					break
				}

				activeCount++

				go func() {
					resp, err := c.http.Do(req2) //nolint:bodyclose
					resultsCh <- result{resp: resp, err: err}
				}()
			}

		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return nil, firstErr
}

func isCrossOrigin(u1, u2 *url.URL) bool {
	if u1.Scheme != u2.Scheme {
		return true
	}

	if u1.Host != u2.Host {
		return true
	}

	return false
}

func scrubSensitiveHeaders(req *http.Request, via []*http.Request) {
	if len(via) == 0 {
		return
	}

	if isCrossOrigin(req.URL, via[0].URL) {
		for _, h := range defaultSensitiveHeaders {
			req.Header.Del(h)
		}
	}
}

func redirectPolicy(maxRedirects int) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if maxRedirects >= 0 && len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}

		scrubSensitiveHeaders(req, via)

		return nil
	}
}

// unwrapBody traverses the Unwrap() chain of response body wrappers,
// returning the innermost io.Closer. This is analogous to errors.Unwrap
// and avoids reflection entirely.
func unwrapBody(c io.Closer) io.Closer {
	for {
		u, ok := c.(interface{ Unwrap() io.Closer })
		if !ok {
			break
		}

		c = u.Unwrap()
	}

	return c
}

func closeResponse(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}

	// Close the outermost wrapper; this drains/closes the network socket
	// through the entire chain of wrappers.
	_ = resp.Body.Close()

	// Recursively unwrap to find the innermost body. If it is a
	// multiReadBody we must call ReallyClose() to delete the temp file,
	// because Close() on multiReadBody only resets the read cursor.
	if rb, ok := unwrapBody(resp.Body).(interface{ ReallyClose() }); ok {
		rb.ReallyClose()
	}
}

func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() {
		return true
	}

	// Check private IP ranges
	if ip4 := ip.To4(); ip4 != nil {
		return ip4[0] == 10 ||
			(ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31) ||
			(ip4[0] == 192 && ip4[1] == 168)
	}

	if ip6 := ip.To16(); ip6 != nil {
		// Unique Local IPv6 (fc00::/7)
		return (ip6[0] & 0xfe) == 0xfc
	}

	return false
}

func happyEyeballsDial(
	ctx context.Context,
	network, addr string,
	delay time.Duration,
	ssrfGuard bool,
) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	addrs, err := (&net.Resolver{}).LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}

	var filtered []net.IP
	for _, ia := range addrs {
		if ssrfGuard && isBlockedIP(ia.IP) {
			continue
		}

		filtered = append(filtered, ia.IP)
	}

	if len(filtered) == 0 {
		return nil, fmt.Errorf("%w: %s resolves to blocked or empty IPs", ErrSSRFBlocked, host)
	}

	if len(filtered) == 1 || delay <= 0 {
		dialer := &net.Dialer{Timeout: 30 * time.Second}
		return dialer.DialContext(ctx, network, net.JoinHostPort(filtered[0].String(), port))
	}

	type dialResult struct {
		conn net.Conn
		err  error
	}

	resultCh := make(chan dialResult, len(filtered))

	dialCtx, cancelAll := context.WithCancel(ctx)
	defer cancelAll()

	var (
		wg   sync.WaitGroup
		done uint32
	)

	for i, ip := range filtered {
		wg.Add(1)

		go func(targetIP net.IP, idx int) {
			defer wg.Done()

			if idx > 0 {
				select {
				case <-dialCtx.Done():
					return
				case <-time.After(time.Duration(idx) * delay):
				}
			}

			if atomic.LoadUint32(&done) == 1 {
				return
			}

			dialer := &net.Dialer{Timeout: 30 * time.Second}

			conn, err := dialer.DialContext(dialCtx, network, net.JoinHostPort(targetIP.String(), port))
			if err == nil {
				if atomic.CompareAndSwapUint32(&done, 0, 1) {
					resultCh <- dialResult{conn: conn}

					cancelAll()
				} else {
					_ = conn.Close()
				}
			} else {
				resultCh <- dialResult{err: err}
			}
		}(ip, i)
	}

	var firstErr error

	failedCount := 0

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case res := <-resultCh:
			if res.conn != nil {
				return res.conn, nil
			}

			if firstErr == nil {
				firstErr = res.err
			}

			failedCount++
			if failedCount == len(filtered) {
				return nil, firstErr
			}
		}
	}
}
