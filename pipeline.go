// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	stdio "io"
	"mime"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
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

// execute routes an outgoing [*http.Request] through the complete client request-response pipeline.
//
// Execution Order:
//  1. Request context preparation and before-request hooks execution.
//  2. Cache lookup for idempotent requests.
//  3. Network dispatch (direct, proxy failover, or request hedging).
//  4. Telemetry collection and traffic inspection capture.
//  5. Response post-processing (decompression, WAF challenge solving, validation, multi-read buffering).
//
// Postconditions:
//   - Finalizes JA4 signatures into the request's [telemetry.TraceInfo] if tracing was enabled.
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

func (c *Client) dispatchRequest(req *http.Request, pipe PipelineConfig) (*http.Response, error) {
	var (
		resp *http.Response
		err  error
	)

	switch {
	case pipe.ProxyFailover != nil:
		resp, err = c.executeWithProxyFailover(req, pipe.ProxyFailover, pipe.Hedging)
	case pipe.Hedging != nil:
		resp, err = c.executeWithHedging(req, pipe.Hedging)
	default:
		resp, err = c.engine.Do(req)
	}

	if resp != nil && resp.Request == nil {
		resp.Request = req
	}

	return resp, err
}

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

	cfg := GetRequestConfig(req.Context())
	if cfg != nil && cfg.UploadProgress != nil && req.Body != nil && req.Body != http.NoBody {
		req.Body = &io.ProgressReader{
			Reader:     req.Body,
			Total:      req.ContentLength,
			OnProgress: cfg.UploadProgress,
		}
	}

	return req
}

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

func (c *Client) applyPacketPadding(req *http.Request) {
	if padding := fingerprint.GeneratePadding(*c.fingerprint.PacketPadding); len(padding) > 0 {
		headerName := fingerprint.PaddingHeaderName(*c.fingerprint.PacketPadding)
		req.Header.Set(headerName, hex.EncodeToString(padding))
	}
}

func (c *Client) applyRefererHeader(req *http.Request) {
	if req.Header.Get("Referer") != "" {
		return
	}

	state := c.defaults.RefererState
	state.mu.Lock()
	lastURL := state.lastURL
	state.mu.Unlock()

	if lastURL != "" {
		req.Header.Set("Referer", lastURL)
	}
}

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

	if !matchVaryHeaders(req, cached.VaryHeaders) {
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

func matchVaryHeaders(req *http.Request, varyHeaders map[string]string) bool {
	if len(varyHeaders) == 0 {
		return true
	}

	for k, expectedVal := range varyHeaders {
		if req.Header.Get(k) != expectedVal {
			return false
		}
	}

	return true
}

func parseFreshnessLifetime(resp *http.Response) (time.Duration, bool) {
	cc := resp.Header.Get("Cache-Control")
	for p := range strings.SplitSeq(cc, ",") {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "s-maxage=") {
			if secs, err := strconv.ParseInt(p[9:], 10, 64); err == nil && secs >= 0 {
				return time.Duration(secs) * time.Second, true
			}
		}

		if strings.HasPrefix(p, "max-age=") {
			if secs, err := strconv.ParseInt(p[8:], 10, 64); err == nil && secs >= 0 {
				return time.Duration(secs) * time.Second, true
			}
		}
	}

	if exp := resp.Header.Get("Expires"); exp != "" {
		if t, err := http.ParseTime(exp); err == nil {
			return max(time.Until(t), 0), true
		}
	}

	return 0, false
}

func (c *Client) saveToCache(req *http.Request, resp *http.Response, cfg *CacheConfig) {
	if req.Method != http.MethodGet || resp == nil || resp.StatusCode != http.StatusOK || cfg == nil ||
		cfg.Store == nil {
		return
	}

	respCC := resp.Header.Get("Cache-Control")
	if strings.Contains(respCC, "no-store") || strings.Contains(respCC, "private") {
		return
	}

	varyHeader := resp.Header.Get("Vary")
	if varyHeader == "*" {
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
		StatusCode:  resp.StatusCode,
		Header:      resp.Header,
		VaryHeaders: extractVaryHeaders(req, varyHeader),
		BodyBase64:  base64.StdEncoding.EncodeToString(bodyBytes),
	}

	cachedData, marshalErr := json.Marshal(cached)
	if marshalErr != nil {
		return
	}

	ttl := cfg.DefaultTTL
	if reqCfg := GetRequestConfig(req.Context()); reqCfg != nil && reqCfg.CacheTTL > 0 {
		ttl = reqCfg.CacheTTL
	} else if parsedTTL, ok := parseFreshnessLifetime(resp); ok {
		ttl = parsedTTL
	}

	_ = cfg.Store.Set(req.Context(), CacheKey{Method: req.Method, URL: req.URL.String()}, cachedData, ttl)
}

func extractVaryHeaders(req *http.Request, varyHeader string) map[string]string {
	if varyHeader == "" {
		return nil
	}

	varyMap := make(map[string]string)
	for p := range strings.SplitSeq(varyHeader, ",") {
		hName := strings.TrimSpace(p)
		if hName != "" && hName != "*" {
			varyMap[hName] = req.Header.Get(hName)
		}
	}

	return varyMap
}

func (c *Client) postProcessResponse(
	req *http.Request,
	resp *http.Response,
	pipe PipelineConfig,
) (*http.Response, error) {
	if err := validateAndNormalizeContentLength(resp); err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}

		return nil, err
	}

	if pipe.Decompress {
		resp = c.handleDecompressionAndTranscoding(req, resp)
	}

	if pipe.SizeLimit > 0 {
		if limitErr := c.limitResponseSize(resp, pipe.SizeLimit); limitErr != nil {
			return nil, limitErr
		}
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
	}

	if pipe.Cache != nil && req.Method == http.MethodGet {
		c.saveToCache(req, resp, pipe.Cache)
	}

	return resp, nil
}

func validateAndNormalizeContentLength(resp *http.Response) error {
	if resp == nil || len(resp.Header["Content-Length"]) <= 1 {
		return nil
	}

	clValues := resp.Header["Content-Length"]
	firstVal := strings.TrimSpace(clValues[0])

	for _, val := range clValues[1:] {
		if strings.TrimSpace(val) != firstVal {
			return ErrConflictingContentLength
		}
	}

	resp.Header["Content-Length"] = []string{firstVal}

	return nil
}

func (c *Client) redactSensitiveData(req *http.Request, redact *RedactConfig) *http.Request {
	headers := make(map[string]struct{}, len(redact.HeadersToRedact))
	for _, h := range redact.HeadersToRedact {
		headers[strings.ToLower(h)] = struct{}{}
	}

	if len(headers) == 0 {
		headers["authorization"] = struct{}{}
		headers["cookie"] = struct{}{}
		headers["set-cookie"] = struct{}{}
	}

	cfg := GetOrInitRequestConfig(req)
	cfg.Redact = &RedactConfig{Headers: headers}

	return req
}

func (c *Client) limitResponseSize(resp *http.Response, maxSize int64) error {
	if resp == nil || resp.Body == nil || maxSize <= 0 {
		return nil
	}

	if resp.ContentLength > 0 && resp.ContentLength <= maxSize {
		return nil
	}

	if resp.ContentLength > maxSize {
		_ = resp.Body.Close()
		return &Error{Op: "limit response size", Err: ErrResponseTooLarge}
	}

	resp.Body = &io.LimitCheckingReadCloser{
		ReadCloser: resp.Body,
		Limit:      maxSize,
	}

	return nil
}

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

func (c *Client) applyMultiReadBuffering(_ *http.Request, resp *http.Response, cfg *RequestConfig) error {
	threshold := c.defaults.MultiReadThreshold
	disableDisk := c.defaults.MultiReadDisableDisk

	if cfg != nil {
		if cfg.MultiReadThreshold > 0 {
			threshold = cfg.MultiReadThreshold
		}

		if cfg.MultiReadDisableDisk {
			disableDisk = cfg.MultiReadDisableDisk
		}
	}

	if threshold <= 0 || resp == nil || resp.Body == nil || resp.Body == http.NoBody {
		return nil
	}

	mBody, err := io.NewMultiReadBody(resp.Body, threshold, disableDisk)
	if err != nil {
		_ = resp.Body.Close()
		return err
	}

	resp.Body = &io.ResponseBodyReadCloser{ReadCloser: mBody}

	return nil
}

func (c *Client) handleDecompressionAndTranscoding(req *http.Request, resp *http.Response) *http.Response {
	if resp == nil || resp.Body == nil {
		return resp
	}

	if cfg := GetRequestConfig(req.Context()); cfg != nil && cfg.DownloadProgress != nil {
		applyDownloadProgress(resp, cfg.DownloadProgress)
	}

	// Only transparently decompress if Accept-Encoding was injected automatically by client defaults
	if !hasExplicitAcceptEncoding(req) {
		if decompressed := applyContentDecompression(resp); decompressed {
			resp.Uncompressed = true
		}
	}

	applyCharsetTranscoding(resp)

	return resp
}

func hasExplicitAcceptEncoding(req *http.Request) bool {
	if req == nil || req.Context() == nil {
		return false
	}

	cfg := GetRequestConfig(req.Context())

	return cfg != nil && cfg.HasExplicitAcceptEncoding
}

func applyDownloadProgress(resp *http.Response, progress ProgressFunc) {
	resp.Body = &io.ProgressReader{
		Reader:     resp.Body,
		Total:      resp.ContentLength,
		OnProgress: progress,
	}
}

func applyContentDecompression(resp *http.Response) bool {
	encoding := resp.Header.Get("Content-Encoding")
	switch encoding {
	case "br":
		resp.Body = &io.DecompressReadCloser{
			Reader: brotli.NewReader(resp.Body),
			Closer: resp.Body,
		}
		resetDecompressedHeader(resp)

		return true

	case "zstd":
		if zstdDec, err := zstd.NewReader(resp.Body); err == nil {
			resp.Body = &io.DecompressReadCloser{
				Reader: zstdDec,
				Closer: resp.Body,
			}
			resetDecompressedHeader(resp)

			return true
		}

		resp.Header.Del("Content-Encoding")

	case "gzip":
		if gzReader, err := io.NewPooledGzipReader(resp.Body); err == nil {
			resp.Body = gzReader
			resetDecompressedHeader(resp)

			return true
		}

		resp.Header.Del("Content-Encoding")
	}

	return false
}

func resetDecompressedHeader(resp *http.Response) {
	resp.Header.Del("Content-Encoding")
	resp.Header.Del("Content-Length")
	resp.ContentLength = -1
}

func applyCharsetTranscoding(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		return
	}

	lower := strings.ToLower(contentType)
	if !strings.Contains(lower, "charset=") || isUTF8Charset(lower) {
		return
	}

	parseAndApplyNonUTF8Transcoder(resp, contentType)
}

func isUTF8Charset(lowerContentType string) bool {
	return strings.Contains(lowerContentType, "charset=utf-8") ||
		strings.Contains(lowerContentType, "charset=utf8") ||
		strings.Contains(lowerContentType, "charset=\"utf-8\"") ||
		strings.Contains(lowerContentType, "charset=\"utf8\"")
}

func parseAndApplyNonUTF8Transcoder(resp *http.Response, contentType string) {
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

func (c *Client) executeWithProxyFailover(
	req *http.Request,
	failover *ProxyFailoverConfig,
	hedging *HedgingConfig,
) (*http.Response, error) {
	parsed := parseProxyURLs(failover.Proxies)
	if len(parsed) == 0 {
		return c.dispatchProxyAttempt(req, hedging)
	}

	var lastErr error

	for range failover.RetryLimit + 1 {
		proxyURL := c.selectNextProxy(parsed, lastErr != nil)

		clonedReq, err := c.prepareRequestForProxy(req, proxyURL)
		if err != nil {
			lastErr = err
			continue
		}

		resp, err := c.dispatchProxyAttempt(clonedReq, hedging)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode != http.StatusBadGateway && resp.StatusCode != http.StatusServiceUnavailable {
			return resp, nil
		}

		_ = resp.Body.Close()
		lastErr = &Error{
			Op:  "proxy failover",
			Err: errors.New("proxy returned status " + strconv.Itoa(resp.StatusCode)),
		}
	}

	return nil, &Error{Op: "proxy failover exhausted " + strconv.Itoa(failover.RetryLimit) + " retries", Err: lastErr}
}

func (c *Client) dispatchProxyAttempt(req *http.Request, hedging *HedgingConfig) (*http.Response, error) {
	if hedging != nil {
		return c.executeWithHedging(req, hedging)
	}

	return c.engine.Do(req)
}

func parseProxyURLs(proxies []string) []*url.URL {
	parsed := make([]*url.URL, 0, len(proxies))
	for _, p := range proxies {
		if u, err := url.Parse(p); err == nil {
			parsed = append(parsed, u)
		}
	}

	return parsed
}

func (c *Client) selectNextProxy(proxies []*url.URL, isRetry bool) *url.URL {
	var idx uint32
	if isRetry {
		idx = atomic.AddUint32(&c.proxyFailoverCounter, 1)
	} else {
		idx = atomic.LoadUint32(&c.proxyFailoverCounter)
	}

	return proxies[idx%uint32(len(proxies))] //nolint:gosec
}

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

func (c *Client) handleWAFChallenge(req *http.Request, resp *http.Response) (*http.Response, error) {
	if c.defaults.ChallengeDetector == nil || c.defaults.ChallengeSolver == nil || resp == nil || resp.Body == nil {
		return resp, nil
	}

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

	return c.defaults.ChallengeSolver.Solve(req.Context(), challengeErr, req)
}

func isIdempotentMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

func (c *Client) executeWithHedging(req *http.Request, pipeHedging *HedgingConfig) (*http.Response, error) {
	cfg := GetRequestConfig(req.Context())

	allowNonReadOnly := (cfg != nil && cfg.AllowNonReadOnlyHedging) ||
		(pipeHedging != nil && pipeHedging.AllowNonReadOnly)

	if !allowNonReadOnly && !isIdempotentMethod(req.Method) {
		return c.engine.Do(req)
	}

	requestStart := time.Now()
	delay := c.resolveHedgingDelay(cfg, pipeHedging)

	var (
		resp *http.Response
		err  error
	)

	if delay > 0 {
		resp, err = c.dispatchHedgingAttempts(req, delay)
	} else {
		resp, err = c.engine.Do(req)
	}

	tracker := c.resolveRTTTracker(pipeHedging)
	if tracker != nil && err == nil {
		tracker.Record(time.Since(requestStart))
	}

	return resp, err
}

func (c *Client) resolveHedgingDelay(cfg *RequestConfig, pipeHedging *HedgingConfig) time.Duration {
	switch {
	case cfg != nil && cfg.HedgingDelayOverride != nil:
		return *cfg.HedgingDelayOverride
	case pipeHedging != nil && pipeHedging.DynamicHedging != nil:
		return pipeHedging.DynamicHedging.ComputeDelay()
	case pipeHedging != nil:
		return pipeHedging.DefaultDelay
	default:
		return c.network.HedgingDelay
	}
}

func (c *Client) resolveRTTTracker(pipeHedging *HedgingConfig) *telemetry.RTTTracker {
	if pipeHedging != nil && pipeHedging.DynamicHedging != nil {
		return pipeHedging.DynamicHedging.Tracker
	}

	if c.network.DynamicHedging != nil {
		return c.network.DynamicHedging.Tracker
	}

	return nil
}

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

func (c *Client) cloneRequest(orig *http.Request, reqCtx context.Context) (*http.Request, error) {
	cloned := orig.Clone(reqCtx)
	if orig.Body == nil || orig.Body == http.NoBody {
		return cloned, nil
	}

	if orig.GetBody == nil {
		return nil, ErrHedgingBodyNonRepeatable
	}

	body, err := orig.GetBody()
	if err != nil {
		return nil, err
	}

	cloned.Body = body

	return cloned, nil
}

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

func (c *Client) determineProxy(req *http.Request) (*url.URL, error) {
	if raw, ok := GetProxyOverride(req.Context()).Value(); ok && raw != "" {
		return url.Parse(raw)
	}

	if c.network.ProxyAddr != nil {
		return c.network.ProxyAddr, nil
	}

	return http.ProxyFromEnvironment(req)
}

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

func applyFragmentation(conn net.Conn, cfg fragment.Config) net.Conn {
	return &fragment.FragmentedConn{
		Conn:      conn,
		ChunkSize: cfg.ChunkSize,
		MaxDelay:  cfg.MaxDelay,
	}
}

func isCrossOrigin(u1, u2 *url.URL) bool {
	return u1.Scheme != u2.Scheme || u1.Host != u2.Host
}
