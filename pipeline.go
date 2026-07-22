// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	stdio "io"
	"mime"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/transform"

	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/fingerprint"
	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/internal/io"
	"github.com/lemon4ksan/aoni/internal/tcp"
	"github.com/lemon4ksan/aoni/netutil/fragment"
	"github.com/lemon4ksan/aoni/telemetry"
)

// execute executes a request through the full client pipeline.
func (c *Client) execute(req *http.Request, pipe PipelineConfig) (*http.Response, error) {
	startTime := time.Now()
	req = c.prepareRequest(req, pipe)

	if cachedResp := c.tryGetFromCache(req, pipe.Cache); cachedResp != nil {
		return cachedResp, nil
	}

	req, traceInfo, traceEnd := c.traceRequest(req, pipe)
	resp, err := c.dispatchRequest(req, pipe)
	duration := time.Since(startTime).Milliseconds()

	for _, hook := range c.defaults.AfterResponse {
		hook(resp, err)
	}

	if traceEnd != nil {
		traceEnd(resp)
	}

	if pipe.Inspect && c.defaults.Inspector != nil {
		c.defaults.Inspector.Capture(req, resp, err, traceInfo)
	}

	if pipe.HAR != nil && pipe.HAR.Tracker != nil {
		pipe.HAR.Tracker.Record(req, resp, startTime, duration)
	}

	if err != nil {
		return nil, err
	}

	c.finalizeJA4Report(GetRequestConfig(req.Context()))

	return c.postProcessResponse(req, resp, pipe)
}

// dispatchRequest routes request execution depending on proxy or hedging setup.
func (c *Client) dispatchRequest(req *http.Request, pipe PipelineConfig) (*http.Response, error) {
	switch {
	case pipe.ProxyFailover != nil:
		return c.executeWithProxyFailover(req, pipe.ProxyFailover, pipe.Hedging)
	case pipe.Hedging != nil:
		return c.executeWithHedging(req, pipe.Hedging)
	default:
		return c.engine.Do(req)
	}
}

// prepareRequestContext configures request deadlines and session keys.
func (c *Client) prepareRequestContext(req *http.Request) *http.Request {
	ctx := req.Context()

	cfg := GetRequestConfig(ctx)
	if cfg == nil {
		return req
	}

	if cfg.TimeoutOverride > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, cfg.TimeoutOverride) //nolint:gosec
		cfg.RequestTimeoutCancel = cancel
	}

	if cfg.SessionCache != nil && cfg.ProxyAddr != nil {
		cfg.SessionCache.SetProxyKey(cfg.ProxyAddr.String())
	}

	if cfg.ProxyAddr != nil {
		ctx = cookie.WithProxyAddress(ctx, cfg.ProxyAddr.String())
	}

	return req.WithContext(ctx)
}

// prepareRequest executes user hooks and applies evasion features.
func (c *Client) prepareRequest(req *http.Request, pipe PipelineConfig) *http.Request {
	req = c.prepareRequestContext(req)

	for _, hook := range c.defaults.BeforeRequest {
		hook(req)
	}

	if c.fingerprint.PacketPadding != nil {
		c.applyPacketPadding(req)
	}

	if c.defaults.RefererAutomaton {
		c.applyRefererHeader(req)
	}

	if pipe.RotateUA {
		c.rotateUserAgentAndHints(req)
	}

	if pipe.DPIJitter != nil {
		c.applyDPIJitter(req, pipe.DPIJitter)
	}

	if pipe.Redact != nil {
		req = c.redactSensitiveData(req, pipe.Redact)
	}

	return req
}

// traceRequest attaches telemetry tracer configs to the context.
func (c *Client) traceRequest(
	req *http.Request,
	pipe PipelineConfig,
) (*http.Request, *telemetry.TraceInfo, func(resp *http.Response)) {
	cfg := GetRequestConfig(req.Context())

	var traceInfo *telemetry.TraceInfo

	switch {
	case cfg != nil && cfg.TraceInfo != nil:
		traceInfo = cfg.TraceInfo
	case pipe.Inspect && c.defaults.Inspector != nil:
		traceInfo = &telemetry.TraceInfo{}

		store := &JA4ReportStore{Target: traceInfo}
		if cfg != nil {
			cfg.JA4ReportStore = store
		}

		traceInfo.JA4 = &ja4.Report{JA4H: telemetry.ComputeJA4HFromRequest(req)}
	}

	if traceInfo == nil {
		return req, nil, nil
	}

	trace := &httptrace.ClientTrace{
		DNSStart:          func(_ httptrace.DNSStartInfo) { traceInfo.DNSStart = time.Now() },
		DNSDone:           func(_ httptrace.DNSDoneInfo) { traceInfo.DNSLookup = time.Since(traceInfo.DNSStart) },
		ConnectStart:      func(_, _ string) { traceInfo.ConnectStart = time.Now() },
		ConnectDone:       func(_, _ string, _ error) { traceInfo.TCPConn = time.Since(traceInfo.ConnectStart) },
		TLSHandshakeStart: func() { traceInfo.TLSStart = time.Now() },
		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			traceInfo.TLSHandshake = time.Since(traceInfo.TLSStart)
			if err == nil {
				stCopy := state
				traceInfo.TLSState = &stCopy
				traceInfo.PeerCertificates = state.PeerCertificates
			}
		},
		GotConn: func(info httptrace.GotConnInfo) {
			traceInfo.GotConn = time.Now()
			if info.Conn != nil && info.Conn.RemoteAddr() != nil {
				traceInfo.RemoteAddr = info.Conn.RemoteAddr().String()
			}
		},
		GotFirstResponseByte: func() { traceInfo.ServerProcessing = time.Since(traceInfo.GotConn) },
	}

	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

	return req, traceInfo, traceInfo.Start() //nolint:bodyclose
}

// rotateUserAgentAndHints rotates UA headers and client hints.
func (c *Client) rotateUserAgentAndHints(req *http.Request) {
	profiles := c.defaults.UARotationProfiles
	if len(profiles) == 0 {
		profiles = DefaultBrowserProfiles
	}

	idx := atomic.AddUint32(&c.userAgentRotationCounter, 1) - 1
	prof := profiles[idx%uint32(len(profiles))] //nolint:gosec

	req.Header.Set("User-Agent", prof.UserAgent)

	for k, v := range prof.ClientHints {
		req.Header.Set(k, v)
	}
}

// applyDPIJitter introduces randomized delays before sending payloads.
func (c *Client) applyDPIJitter(req *http.Request, cfg *DPIJitterConfig) {
	delay := cfg.MinDelay
	if cfg.MinDelay > 0 && cfg.MaxDelay >= cfg.MinDelay {
		if delta := cfg.MaxDelay - cfg.MinDelay; delta > 0 {
			delay = cfg.MinDelay + time.Duration(time.Now().UnixNano()%int64(delta))
		}
	}

	if delay <= 0 {
		return
	}

	if req.Body != nil && req.Body != http.NoBody {
		req.Body = &io.JitterReader{
			ReadCloser: req.Body,
			Delay:      delay,
		}

		return
	}

	time.Sleep(delay)
}

// applyPacketPadding injects random padding headers to obscure TLS segment boundaries.
func (c *Client) applyPacketPadding(req *http.Request) {
	if padding := fingerprint.GeneratePadding(*c.fingerprint.PacketPadding); len(padding) > 0 {
		headerName := fingerprint.PaddingHeaderName(*c.fingerprint.PacketPadding)
		req.Header.Set(headerName, hex.EncodeToString(padding))
	}
}

// applyRefererHeader tracks and automatically injects Referer headers.
func (c *Client) applyRefererHeader(req *http.Request) {
	if req.Header.Get("Referer") == "" {
		state := c.defaults.RefererState
		state.mu.Lock()
		lastURL := state.lastURL
		state.mu.Unlock()

		if lastURL != "" {
			req.Header.Set("Referer", lastURL)
		}
	}
}

// tryGetFromCache retrieves a cached response if available. Returns nil on cache bypass or miss.
func (c *Client) tryGetFromCache(req *http.Request, cfg *CacheConfig) *http.Response {
	if req.Method != http.MethodGet || cfg == nil || cfg.Store == nil {
		return nil
	}

	cc := req.Header.Get("Cache-Control")
	if strings.Contains(cc, "no-cache") || strings.Contains(cc, "no-store") {
		return nil
	}

	cachedData, err := cfg.Store.Get(req.Context(), CacheKey{Method: req.Method, URL: req.URL.String()})
	if err != nil {
		return nil
	}

	var cached CachedResponse
	if decodeErr := json.Unmarshal(cachedData, &cached); decodeErr != nil {
		return nil
	}

	bodyBytes, _ := base64.StdEncoding.DecodeString(cached.BodyBase64)

	return &http.Response{
		StatusCode:    cached.StatusCode,
		Header:        cached.Header,
		Body:          stdio.NopCloser(bytes.NewReader(bodyBytes)),
		ContentLength: int64(len(bodyBytes)),
		Request:       req,
	}
}

// saveToCache caches a successful response.
func (c *Client) saveToCache(req *http.Request, resp *http.Response, cfg *CacheConfig) {
	if req.Method != http.MethodGet || resp == nil || resp.StatusCode != http.StatusOK || cfg == nil ||
		cfg.Store == nil {
		return
	}

	respCC := resp.Header.Get("Cache-Control")
	if strings.Contains(respCC, "no-store") || strings.Contains(respCC, "private") {
		return
	}

	var bodyBuf bytes.Buffer

	tee := stdio.TeeReader(resp.Body, &bodyBuf)

	bodyBytes, readErr := stdio.ReadAll(tee)
	if readErr != nil {
		return
	}

	_ = resp.Body.Close()
	resp.Body = stdio.NopCloser(bytes.NewReader(bodyBytes))

	cached := CachedResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		BodyBase64: base64.StdEncoding.EncodeToString(bodyBytes),
	}

	if cachedData, marshalErr := json.Marshal(cached); marshalErr == nil {
		ttl := cfg.DefaultTTL
		if reqCfg := GetRequestConfig(req.Context()); reqCfg != nil && reqCfg.CacheTTL > 0 {
			ttl = reqCfg.CacheTTL
		}

		_ = cfg.Store.Set(req.Context(), CacheKey{Method: req.Method, URL: req.URL.String()}, cachedData, ttl)
	}
}

// postProcessResponse applies WAF checks, decompression, buffering and validation.
func (c *Client) postProcessResponse(
	req *http.Request,
	resp *http.Response,
	pipe PipelineConfig,
) (*http.Response, error) {
	if pipe.SizeLimit > 0 {
		if limitErr := c.limitResponseSize(resp, pipe.SizeLimit); limitErr != nil {
			return nil, limitErr
		}
	}

	if pipe.Decompress {
		resp = c.handleDecompressionAndTranscoding(req, resp)
	}

	if pipe.Challenge {
		var err error

		resp, err = c.handleWAFChallenge(req, resp)
		if err != nil {
			return nil, err
		}
	}

	if pipe.Validate {
		if valErr := c.validateResponse(resp); valErr != nil {
			return nil, valErr
		}
	}

	if c.defaults.RefererAutomaton && c.defaults.RefererState != nil && req != nil && req.URL != nil {
		c.defaults.RefererState.mu.Lock()
		c.defaults.RefererState.lastURL = req.URL.String()
		c.defaults.RefererState.mu.Unlock()
	}

	if resp != nil && resp.Body != nil {
		if bufErr := c.applyMultiReadBuffering(req, resp, GetRequestConfig(req.Context())); bufErr != nil {
			return nil, bufErr
		}

		resp.Body = &io.ResponseBodyReadCloser{ReadCloser: resp.Body}
	}

	if pipe.Cache != nil && req.Method == http.MethodGet {
		c.saveToCache(req, resp, pipe.Cache)
	}

	return resp, nil
}

// redactSensitiveData replaces sensitive request header values with REDACTED.
func (c *Client) redactSensitiveData(req *http.Request, redact *RedactConfig) *http.Request {
	headers := make(map[string]struct{})
	for _, h := range redact.HeadersToRedact {
		headers[strings.ToLower(h)] = struct{}{}
	}

	if len(headers) == 0 {
		headers["authorization"] = struct{}{}
		headers["cookie"] = struct{}{}
		headers["set-cookie"] = struct{}{}
	}

	return req.WithContext(
		context.WithValue(req.Context(), RedactConfigCtxKey{}, &RedactConfig{Headers: headers}),
	)
}

// limitResponseSize enforces limits on the maximum response size.
func (c *Client) limitResponseSize(resp *http.Response, maxSize int64) error {
	if resp == nil || resp.Body == nil || maxSize <= 0 {
		return nil
	}

	if resp.ContentLength > maxSize {
		_ = resp.Body.Close()
		return fmt.Errorf("aoni: response too large: %w", ErrResponseTooLarge)
	}

	resp.Body = &io.LimitCheckingReadCloser{
		ReadCloser: resp.Body,
		Limit:      maxSize,
	}

	return nil
}

// validateResponse executes validation hooks on the response.
func (c *Client) validateResponse(resp *http.Response) error {
	if resp == nil || resp.Request == nil {
		return nil
	}

	var clientErr error
	if c.defaults.ResponseValidator != nil {
		clientErr = c.defaults.ResponseValidator(resp)
	}

	fn := GetResponseValidator(resp.Request.Context()) //nolint:bodyclose
	if fn != nil {
		requestErr := fn(resp)
		if requestErr != nil {
			if resp.Body != nil {
				_ = resp.Body.Close()
			}

			return requestErr
		}

		return nil
	}

	if clientErr != nil {
		if resp.Body != nil {
			_ = resp.Body.Close()
		}

		return clientErr
	}

	return nil
}

// applyMultiReadBuffering applies high-performance buffering for multiple reads.
func (c *Client) applyMultiReadBuffering(_ *http.Request, resp *http.Response, cfg *RequestConfig) error {
	threshold := c.defaults.MultiReadThreshold

	disableDisk := c.defaults.MultiReadDisableDisk
	if cfg != nil {
		threshold = cfg.MultiReadThreshold
		disableDisk = cfg.MultiReadDisableDisk
	}

	if threshold > 0 && resp.Body != nil {
		mBody, err := io.NewMultiReadBody(resp.Body, threshold, disableDisk)
		if err != nil {
			_ = resp.Body.Close()
			return err
		}

		resp.Body = mBody
	}

	return nil
}

// handleDecompressionAndTranscoding transcodes non-UTF-8 charsets and decompresses payloads.
func (c *Client) handleDecompressionAndTranscoding(req *http.Request, resp *http.Response) *http.Response {
	if resp == nil || resp.Body == nil {
		return resp
	}

	if cfg := GetRequestConfig(req.Context()); cfg != nil && cfg.DownloadProgress != nil {
		applyDownloadProgress(resp, cfg.DownloadProgress)
	}

	applyContentDecompression(resp)
	applyCharsetTranscoding(resp)

	return resp
}

// applyDownloadProgress attaches a progress reader wrapper to the response stream.
func applyDownloadProgress(resp *http.Response, progress ProgressFunc) {
	resp.Body = &io.ProgressReader{
		Reader:     resp.Body,
		Total:      resp.ContentLength,
		OnProgress: progress,
	}
}

// applyContentDecompression handles decoding of br, zstd, and gzip encodings.
func applyContentDecompression(resp *http.Response) {
	encoding := resp.Header.Get("Content-Encoding")
	switch encoding {
	case "br":
		resp.Body = &io.DecompressReadCloser{
			Reader: brotli.NewReader(resp.Body),
			Closer: resp.Body,
		}
		resetDecompressedHeader(resp)

	case "zstd":
		if zstdDec, err := zstd.NewReader(resp.Body); err == nil {
			resp.Body = &io.DecompressReadCloser{
				Reader: zstdDec,
				Closer: resp.Body,
			}
			resetDecompressedHeader(resp)
		} else {
			resp.Header.Del("Content-Encoding")
		}

	case "gzip":
		if gzReader, err := gzip.NewReader(resp.Body); err == nil {
			resp.Body = &io.DecompressReadCloser{
				Reader: gzReader,
				Closer: resp.Body,
			}
			resetDecompressedHeader(resp)
		} else {
			resp.Header.Del("Content-Encoding")
		}
	}
}

// resetDecompressedHeader resets HTTP headers once payload is fully decompressed.
func resetDecompressedHeader(resp *http.Response) {
	resp.Header.Del("Content-Encoding")
	resp.Header.Del("Content-Length")
	resp.ContentLength = -1
}

// applyCharsetTranscoding transcodes charsets into clean UTF-8 payloads.
func applyCharsetTranscoding(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		return
	}

	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return
	}

	charset := strings.ToLower(params["charset"])
	if charset == "" || charset == "utf-8" || charset == "utf8" {
		return
	}

	enc, err := htmlindex.Get(charset)
	if err != nil {
		return
	}

	type transcodeReadCloser struct {
		stdio.Reader
		stdio.Closer
	}

	resp.Body = &transcodeReadCloser{
		Reader: transform.NewReader(resp.Body, enc.NewDecoder()),
		Closer: resp.Body,
	}
}

// executeWithProxyFailover rotates alternative proxies in the pool if the current proxy fails.
func (c *Client) executeWithProxyFailover(
	req *http.Request,
	failover *ProxyFailoverConfig,
	hedging *HedgingConfig,
) (*http.Response, error) {
	parsed := parseProxyURLs(failover.Proxies)
	if len(parsed) == 0 {
		if hedging != nil {
			return c.executeWithHedging(req, hedging)
		}

		return c.engine.Do(req)
	}

	var (
		lastErr error
		resp    *http.Response
	)

	for i := 0; i <= failover.RetryLimit; i++ {
		proxyURL := c.selectNextProxy(parsed, lastErr != nil)

		clonedReq, err := c.prepareRequestForProxy(req, proxyURL)
		if err != nil {
			lastErr = err
			continue
		}

		if hedging != nil {
			resp, lastErr = c.executeWithHedging(clonedReq, hedging)
		} else {
			resp, lastErr = c.engine.Do(clonedReq)
		}

		if lastErr == nil && resp != nil {
			if resp.StatusCode != http.StatusBadGateway && resp.StatusCode != http.StatusServiceUnavailable {
				return resp, nil
			}

			lastErr = fmt.Errorf("aoni: proxy returned status %d", resp.StatusCode)
			_ = resp.Body.Close()
		}
	}

	return nil, fmt.Errorf("aoni proxy failover: exhausted %d retries, last error: %w", failover.RetryLimit, lastErr)
}

// parseProxyURLs parses a slice of raw proxy URL strings into structured URLs.
func parseProxyURLs(proxies []string) []*url.URL {
	var parsed []*url.URL
	for _, p := range proxies {
		if u, err := url.Parse(p); err == nil {
			parsed = append(parsed, u)
		}
	}

	return parsed
}

// selectNextProxy returns the next cyclic proxy URL in the failover pool.
func (c *Client) selectNextProxy(proxies []*url.URL, isRetry bool) *url.URL {
	var idx uint32
	if isRetry {
		idx = atomic.AddUint32(&c.proxyFailoverCounter, 1)
	} else {
		idx = atomic.LoadUint32(&c.proxyFailoverCounter)
	}

	return proxies[idx%uint32(len(proxies))] //nolint:gosec
}

// prepareRequestForProxy clones the outgoing request context and bodies to target proxy routes.
func (c *Client) prepareRequestForProxy(req *http.Request, proxyURL *url.URL) (*http.Request, error) {
	newReq := req

	cfg := GetRequestConfig(req.Context())
	if cfg != nil {
		ctx := cookie.WithProxyAddress(req.Context(), proxyURL.String())
		newReq = req.WithContext(ctx)
	}

	if req.Body != nil && req.Body != http.NoBody && req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, err
		}

		newReq.Body = body
	}

	return newReq, nil
}

// handleWAFChallenge intercepts response flows to solve JS/DDoS challenges on detection.
func (c *Client) handleWAFChallenge(req *http.Request, resp *http.Response) (*http.Response, error) {
	if c.defaults.ChallengeDetector == nil || c.defaults.ChallengeSolver == nil {
		return resp, nil
	}

	if resp != nil && resp.Body != nil {
		bodyBytes, err := stdio.ReadAll(stdio.LimitReader(resp.Body, 100*1024))
		if err != nil {
			return resp, nil //nolint:nilerr
		}

		buffered := &io.ExplicitBufferedBody{
			Prefix: bodyBytes,
			Stream: resp.Body,
		}
		resp.Body = buffered

		isChallenge, challengeErr := c.defaults.ChallengeDetector(resp)
		if !isChallenge {
			buffered.Rewind()
			return resp, nil
		}

		_ = resp.Body.Close()

		newResp, solveErr := c.defaults.ChallengeSolver.Solve(req.Context(), challengeErr, req)
		if solveErr != nil {
			return nil, solveErr
		}

		return newResp, nil
	}

	return resp, nil
}

// executeWithHedging executes attempts with static or dynamic delays.
func (c *Client) executeWithHedging(req *http.Request, pipeHedging *HedgingConfig) (*http.Response, error) {
	requestStart := time.Now()

	var delay time.Duration

	cfg := GetRequestConfig(req.Context())
	switch {
	case cfg != nil && cfg.HedgingDelayOverride != nil:
		delay = *cfg.HedgingDelayOverride
	case pipeHedging != nil && pipeHedging.DynamicHedging != nil:
		delay = pipeHedging.DynamicHedging.ComputeDelay()
	case pipeHedging != nil:
		delay = pipeHedging.DefaultDelay
	default:
		delay = c.network.HedgingDelay
	}

	var (
		resp *http.Response
		err  error
	)

	if delay > 0 {
		resp, err = c.dispatchHedgingAttempts(req, delay)
	} else {
		resp, err = c.engine.Do(req)
	}

	var tracker *telemetry.RTTTracker
	if pipeHedging != nil && pipeHedging.DynamicHedging != nil {
		tracker = pipeHedging.DynamicHedging.Tracker
	} else if c.network.DynamicHedging != nil {
		tracker = c.network.DynamicHedging.Tracker
	}

	if tracker != nil && err == nil {
		rtt := time.Since(requestStart)
		tracker.Record(rtt)
	}

	return resp, err
}

// dispatchHedgingAttempts coordinates primary and secondary hedging attempt threads.
func (c *Client) dispatchHedgingAttempts(req *http.Request, delay time.Duration) (*http.Response, error) {
	resultsCh := make(chan hedgeResult, 2)

	ctx1, ctx2, cancel1, cancel2, cleanup := c.buildHedgeContext(req)
	defer func() { cleanup(0) }()

	c.launchHedgeAttempt(ctx1, req, resultsCh)

	timer := time.NewTimer(delay)
	defer timer.Stop()

	var (
		req2Started bool
		firstErr    error
	)

	activeCount := 1

	for activeCount > 0 {
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()

		case <-timer.C:
			if !req2Started {
				req2Started = true
				activeCount++

				c.launchHedgeAttempt(ctx2, req, resultsCh)
			}

		case res := <-resultsCh:
			activeCount--

			if res.err == nil {
				return c.handleHedgeWinner(res, ctx2, cancel1, cancel2, cleanup), nil
			}

			if firstErr == nil {
				firstErr = res.err
			}

			if activeCount == 0 && !req2Started {
				timer.Stop()

				req2Started = true
				activeCount++

				c.launchHedgeAttempt(ctx2, req, resultsCh)
			}
		}
	}

	return nil, firstErr
}

type hedgeResult struct {
	resp *http.Response
	err  error
}

// handleHedgeWinner shuts down alternative hedge routines once a winning connection is achieved.
func (c *Client) handleHedgeWinner(
	res hedgeResult,
	ctx2 context.Context,
	cancel1, cancel2 context.CancelFunc,
	cleanup func(int),
) *http.Response {
	winner := 1

	cancelWinner := cancel1
	if res.resp.Request != nil && res.resp.Request.Context() == ctx2 {
		winner = 2
		cancelWinner = cancel2
	}

	cleanup(winner)

	res.resp.Body = &io.ContextCancelingReadCloser{
		ReadCloser: res.resp.Body,
		Cancel:     cancelWinner,
	}

	return res.resp
}

// launchHedgeAttempt schedules a single request hedging attempt in the background.
func (c *Client) launchHedgeAttempt(ctx context.Context, req *http.Request, resultsCh chan<- hedgeResult) {
	cloned, err := c.cloneRequest(req, ctx)
	if err != nil {
		resultsCh <- hedgeResult{err: err}
		return
	}

	go func() {
		resp, err := c.engine.Do(cloned) //nolint:bodyclose
		resultsCh <- hedgeResult{resp: resp, err: err}
	}()
}

// buildHedgeContext generates cancelable context pairs for request hedging attempts.
func (c *Client) buildHedgeContext(
	req *http.Request,
) (context.Context, context.Context, context.CancelFunc, context.CancelFunc, func(winner int)) {
	ctx := req.Context()
	ctx1, cancel1 := context.WithCancel(ctx)
	ctx2, cancel2 := context.WithCancel(ctx)

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

	return ctx1, ctx2, cancel1, cancel2, cleanup
}

// cloneRequest creates deep-copied request structures used during request hedging.
func (c *Client) cloneRequest(orig *http.Request, reqCtx context.Context) (*http.Request, error) {
	cloned := orig.Clone(reqCtx)
	if orig.Body == nil || orig.Body == http.NoBody {
		return cloned, nil
	}

	if orig.GetBody == nil {
		return nil, errors.New("aoni: request body cannot be duplicated for hedging")
	}

	body, err := orig.GetBody()
	if err != nil {
		return nil, err
	}

	cloned.Body = body

	return cloned, nil
}

// finalizeJA4Report maps compiled JA4 reports back to the trace metrics structures.
func (c *Client) finalizeJA4Report(cfg *RequestConfig) {
	if cfg == nil || cfg.JA4ReportStore == nil || cfg.JA4ReportStore.Report == nil {
		return
	}

	store := cfg.JA4ReportStore
	store.Target.JA4.JA4 = store.Report.JA4
	store.Target.JA4.Protocol = store.Report.Protocol
	store.Target.JA4.Version = store.Report.Version
	store.Target.JA4.SNI = store.Report.SNI
	store.Target.JA4.CipherCount = store.Report.CipherCount
	store.Target.JA4.ExtCount = store.Report.ExtCount
	store.Target.JA4.ALPN = store.Report.ALPN
}

// resolvePipeline extracts and resolves active pipeline configs.
func (c *Client) resolvePipeline(req *http.Request) PipelineConfig {
	if reqPipe, ok := GetPipeline(req.Context()); ok {
		return reqPipe
	}

	pipe := c.defaults.Pipeline
	if !pipe.RotateUA && len(c.defaults.UARotationProfiles) > 0 {
		pipe.RotateUA = true
	}

	if pipe.SizeLimit == 0 {
		pipe.SizeLimit = c.defaults.MaxResponseSize
	}

	if !pipe.Inspect && c.defaults.Inspector != nil {
		pipe.Inspect = true
	}

	if pipe.Hedging == nil && (c.network.HedgingDelay > 0 || c.network.DynamicHedging != nil) {
		pipe.Hedging = &HedgingConfig{
			DefaultDelay:   c.network.HedgingDelay,
			DynamicHedging: c.network.DynamicHedging,
		}
	}

	return pipe
}

// determineProxy evaluates proxy resolutions priorities.
func (c *Client) determineProxy(req *http.Request) (*url.URL, error) {
	if raw, ok := GetProxyOverride(req.Context()).Value(); ok && raw != "" {
		return url.Parse(raw)
	}

	if c.network.ProxyAddr != nil {
		return c.network.ProxyAddr, nil
	}

	return http.ProxyFromEnvironment(req)
}

// applyMSSLimit sets the TCP Maximum Segment Size (MSS) socket option on the connection.
func applyMSSLimit(conn net.Conn, mss int) net.Conn {
	if mss <= 0 {
		return conn
	}

	if tc, ok := conn.(*net.TCPConn); ok {
		raw, err := tc.SyscallConn()
		if err != nil {
			return conn
		}

		_ = raw.Control(func(fd uintptr) {
			tcp.SetTCPMaxSeg(fd, mss)
		})
	}

	return conn
}

// applyFragmentation wraps a connection with TCP packet fragmentation.
func applyFragmentation(conn net.Conn, cfg FragmentConfig) net.Conn {
	return &fragment.FragmentedConn{
		Conn:      conn,
		ChunkSize: cfg.ChunkSize,
		MaxDelay:  cfg.MaxDelay,
	}
}

// isCrossOrigin reports whether the target URL belongs to a different origin.
func isCrossOrigin(u1, u2 *url.URL) bool {
	return u1.Scheme != u2.Scheme || u1.Host != u2.Host
}
