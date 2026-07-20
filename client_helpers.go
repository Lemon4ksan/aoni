// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

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
	"io"
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
	"github.com/lemon4ksan/miyako/generic"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/transform"

	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/h2"
	"github.com/lemon4ksan/aoni/ja4"
)

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

func (c *Client) execute(req *http.Request, pipe PipelineConfig) (*http.Response, error) {
	startTime := time.Now()

	req = c.prepareRequest(req, pipe)

	if pipe.Cache != nil && req.Method == http.MethodGet {
		if cachedResp, err := c.tryGetFromCache(req, pipe.Cache); err == nil {
			return cachedResp, nil
		}
	}

	req, traceInfo, traceEnd := c.traceRequest(req, pipe)

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

	if pipe.HAR != nil {
		c.writeHARLog(req, resp, pipe.HAR, startTime, duration)
	}

	if err != nil {
		return nil, err
	}

	c.finalizeJA4Report(GetRequestConfig(req.Context()))

	return c.postProcessResponse(req, resp, pipe)
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

	return req
}

func (c *Client) traceRequest(
	req *http.Request,
	pipe PipelineConfig,
) (*http.Request, *TraceInfo, func(resp *http.Response)) {
	cfg := GetRequestConfig(req.Context())

	var traceInfo *TraceInfo
	switch {
	case cfg != nil && cfg.TraceInfo != nil:
		traceInfo = cfg.TraceInfo
	case pipe.Inspect && c.defaults.Inspector != nil:
		traceInfo = &TraceInfo{}

		store := &JA4ReportStore{Target: traceInfo}
		if cfg != nil {
			cfg.JA4ReportStore = store
		}

		traceInfo.JA4 = &ja4.Report{JA4H: computeJA4HFromRequest(req)}
	}

	if traceInfo == nil {
		return req, nil, nil
	}

	trace := &httptrace.ClientTrace{
		DNSStart:          func(_ httptrace.DNSStartInfo) { traceInfo.dnsStart = time.Now() },
		DNSDone:           func(_ httptrace.DNSDoneInfo) { traceInfo.DNSLookup = time.Since(traceInfo.dnsStart) },
		ConnectStart:      func(_, _ string) { traceInfo.connectStart = time.Now() },
		ConnectDone:       func(_, _ string, _ error) { traceInfo.TCPConn = time.Since(traceInfo.connectStart) },
		TLSHandshakeStart: func() { traceInfo.tlsStart = time.Now() },
		TLSHandshakeDone:  func(_ tls.ConnectionState, _ error) { traceInfo.TLSHandshake = time.Since(traceInfo.tlsStart) },
		GotConn: func(info httptrace.GotConnInfo) {
			traceInfo.gotConn = time.Now()
			if info.Conn != nil && info.Conn.RemoteAddr() != nil {
				traceInfo.RemoteAddr = info.Conn.RemoteAddr().String()
			}
		},
		GotFirstResponseByte: func() { traceInfo.ServerProcessing = time.Since(traceInfo.gotConn) },
	}

	return req.WithContext(
		httptrace.WithClientTrace(req.Context(), trace),
	), traceInfo, traceInfo.Start() //nolint:bodyclose
}

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

		resp.Body = newResponseBodyReadCloser(resp.Body)
	}

	if pipe.Cache != nil && req.Method == http.MethodGet {
		c.saveToCache(req, resp, pipe.Cache)
	}

	return resp, nil
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
		req.Body = &jitterReader{
			ReadCloser: req.Body,
			delay:      delay,
		}

		return
	}

	time.Sleep(delay)
}

func (c *Client) tryGetFromCache(req *http.Request, cfg *CacheConfig) (*http.Response, error) {
	if req.Method != http.MethodGet || cfg == nil || cfg.Store == nil {
		return nil, errors.New("aoni cache: bypass")
	}

	cc := req.Header.Get("Cache-Control")
	if strings.Contains(cc, "no-cache") || strings.Contains(cc, "no-store") {
		return nil, errors.New("aoni cache: bypass via request header")
	}

	cacheKey := req.Method + ":" + req.URL.String()

	cachedData, err := cfg.Store.Get(req.Context(), cacheKey)
	if err != nil {
		return nil, err
	}

	var cached cachedResponse
	if decodeErr := json.Unmarshal(cachedData, &cached); decodeErr != nil {
		return nil, decodeErr
	}

	bodyBytes, _ := base64.StdEncoding.DecodeString(cached.BodyBase64)
	resp := &http.Response{
		StatusCode:    cached.StatusCode,
		Header:        cached.Header,
		Body:          io.NopCloser(bytes.NewReader(bodyBytes)),
		ContentLength: int64(len(bodyBytes)),
		Request:       req,
	}

	return resp, nil
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

	var bodyBuf bytes.Buffer

	tee := io.TeeReader(resp.Body, &bodyBuf)

	bodyBytes, readErr := io.ReadAll(tee)
	if readErr != nil {
		return
	}

	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	cached := cachedResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		BodyBase64: base64.StdEncoding.EncodeToString(bodyBytes),
	}

	if cachedData, marshalErr := json.Marshal(cached); marshalErr == nil {
		ttl := cfg.DefaultTTL
		if reqCfg := GetRequestConfig(req.Context()); reqCfg != nil && reqCfg.CacheTTL > 0 {
			ttl = reqCfg.CacheTTL
		}

		_ = cfg.Store.Set(req.Context(), req.Method+":"+req.URL.String(), cachedData, ttl)
	}
}

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

func (c *Client) writeHARLog(
	req *http.Request,
	resp *http.Response,
	har *HARConfig,
	startTime time.Time,
	duration int64,
) {
	if har == nil || har.Generator == nil || resp == nil {
		return
	}

	var reqHeaders []HARHeaderField
	for k, v := range req.Header {
		for _, val := range v {
			reqHeaders = append(reqHeaders, HARHeaderField{Name: k, Value: val})
		}
	}

	var reqBodySize int64
	if req.Body != nil && req.Body != http.NoBody {
		if req.ContentLength > 0 {
			reqBodySize = req.ContentLength
		}
	}

	var respHeaders []HARHeaderField
	for k, v := range resp.Header {
		for _, val := range v {
			respHeaders = append(respHeaders, HARHeaderField{Name: k, Value: val})
		}
	}

	var bodyBytes []byte
	if resp.Body != nil {
		bodyBytes, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	har.Generator.AddEntry(HAREntry{
		StartedDateTime: startTime.UTC().Format(time.RFC3339Nano),
		Time:            duration,
		Request: HARRequest{
			Method:      req.Method,
			URL:         req.URL.String(),
			HTTPVersion: req.Proto,
			Headers:     reqHeaders,
			Cookies:     []any{},
			QueryString: []any{},
			HeadersSize: -1,
			BodySize:    reqBodySize,
		},
		Response: HARResponse{
			Status:      resp.StatusCode,
			StatusText:  resp.Status,
			HTTPVersion: resp.Proto,
			Headers:     respHeaders,
			Cookies:     []any{},
			Content: HARContent{
				Size:     int64(len(bodyBytes)),
				MimeType: resp.Header.Get("Content-Type"),
				Text:     string(bodyBytes),
			},
			RedirectURL: resp.Header.Get("Location"),
			HeadersSize: -1,
			BodySize:    int64(len(bodyBytes)),
		},
		Cache: struct{}{},
		Timings: HARTimings{
			Send:    0,
			Wait:    duration,
			Receive: 0,
		},
	})
}

func (c *Client) limitResponseSize(resp *http.Response, maxSize int64) error {
	if resp == nil || resp.Body == nil || maxSize <= 0 {
		return nil
	}

	if resp.ContentLength > maxSize {
		_ = resp.Body.Close()
		return fmt.Errorf("aoni: response too large: %w", ErrResponseTooLarge)
	}

	resp.Body = &limitCheckingReadCloser{
		ReadCloser: resp.Body,
		limit:      maxSize,
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
		// Per-request validation was configured and executed.
		// Its result overrides the client-level validator result.
		if requestErr != nil {
			if resp.Body != nil {
				_ = resp.Body.Close()
			}

			return requestErr
		}

		// Request-level validator succeeded (returned nil).
		// This overrides any client-level validation failure.
		return nil
	}

	// If no request-level validator was configured, return the client-level validation error if any.
	if clientErr != nil {
		if resp.Body != nil {
			_ = resp.Body.Close()
		}

		return clientErr
	}

	return nil
}

func (c *Client) executeWithProxyFailover(
	req *http.Request,
	failover *ProxyFailoverConfig,
	hedging *HedgingConfig,
) (*http.Response, error) {
	var parsed []*url.URL
	for _, p := range failover.Proxies {
		if u, err := url.Parse(p); err == nil {
			parsed = append(parsed, u)
		}
	}

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
		var idx uint32
		if lastErr != nil {
			idx = atomic.AddUint32(&c.proxyFailoverCounter, 1)
		} else {
			idx = atomic.LoadUint32(&c.proxyFailoverCounter)
		}

		proxy := parsed[idx%uint32(len(parsed))] //nolint:gosec

		newReq := req

		cfg := GetRequestConfig(req.Context())
		if cfg != nil {
			ctx := req.Context()
			ctx = cookie.WithProxyAddress(ctx, proxy.String())
			newReq = req.WithContext(ctx)
		}

		if req.Body != nil && req.Body != http.NoBody && req.GetBody != nil {
			body, getBodyErr := req.GetBody()
			if getBodyErr == nil {
				newReq.Body = body
			}
		}

		if hedging != nil {
			resp, lastErr = c.executeWithHedging(newReq, hedging)
		} else {
			resp, lastErr = c.engine.Do(newReq)
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

func (c *Client) applyPacketPadding(req *http.Request) {
	if padding := GeneratePadding(*c.fingerprint.PacketPadding); len(padding) > 0 {
		headerName := PaddingHeaderName(*c.fingerprint.PacketPadding)
		req.Header.Set(headerName, hex.EncodeToString(padding))
	}
}

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

func (c *Client) handleWAFChallenge(req *http.Request, resp *http.Response) (*http.Response, error) {
	if c.defaults.ChallengeSolver == nil {
		return resp, nil
	}

	if resp != nil && resp.Body != nil {
		// Read up to 100 KB explicitly to analyze the body for WAF signatures
		bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 100*1024))
		if err != nil {
			return resp, nil //nolint:nilerr
		}

		buffered := &ExplicitBufferedBody{
			Prefix: bodyBytes,
			Stream: resp.Body,
		}
		resp.Body = buffered

		detector := generic.CoalesceNil(c.defaults.ChallengeDetector, DefaultChallengeDetector)

		isChallenge, challengeErr := detector(resp)
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

func (c *Client) applyMultiReadBuffering(_ *http.Request, resp *http.Response, cfg *RequestConfig) error {
	threshold := c.defaults.MultiReadThreshold

	disableDisk := c.defaults.MultiReadDisableDisk
	if cfg != nil {
		threshold = cfg.MultiReadThreshold
		disableDisk = cfg.MultiReadDisableDisk
	}

	if threshold > 0 && resp.Body != nil {
		mBody, err := newMultiReadBody(resp.Body, threshold, disableDisk)
		if err != nil {
			_ = resp.Body.Close()
			return err
		}

		resp.Body = mBody
	}

	return nil
}

func (c *Client) handleDecompressionAndTranscoding(req *http.Request, resp *http.Response) *http.Response {
	if resp == nil || resp.Body == nil {
		return resp
	}

	cfg := GetRequestConfig(req.Context())
	if cfg != nil && cfg.DownloadProgress != nil {
		resp.Body = &progressReader{
			reader:     resp.Body,
			total:      resp.ContentLength,
			onProgress: cfg.DownloadProgress,
		}
	}

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
		if zstdDec, err := zstd.NewReader(resp.Body); err == nil {
			resp.Body = &decompressReadCloser{
				Reader: zstdDec,
				closer: resp.Body,
			}
			resp.Header.Del("Content-Encoding")
			resp.Header.Del("Content-Length")
			resp.ContentLength = -1
		} else {
			resp.Header.Del("Content-Encoding")
		}

	case "gzip":
		if gzReader, err := gzip.NewReader(resp.Body); err == nil {
			resp.Body = &decompressReadCloser{
				Reader: gzReader,
				closer: resp.Body,
			}
			resp.Header.Del("Content-Encoding")
			resp.Header.Del("Content-Length")
			resp.ContentLength = -1
		} else {
			resp.Header.Del("Content-Encoding")
		}
	}

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

	return resp
}

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

	var tracker *RTTTracker
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

type hedgeResult struct {
	resp *http.Response
	err  error
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

				req2Started = true
				activeCount++

				c.launchHedgeAttempt(ctx2, req, resultsCh)
			}

		case <-timer.C:
			if !req2Started {
				req2Started = true
				activeCount++

				c.launchHedgeAttempt(ctx2, req, resultsCh)
			}

		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}

	return nil, firstErr
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
		return nil, errors.New("aoni: request body cannot be duplicated for hedging")
	}

	body, err := orig.GetBody()
	if err != nil {
		return nil, err
	}

	cloned.Body = body

	return cloned, nil
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

type connWrapper struct{}

func (c connWrapper) Wrap(ctx context.Context, conn net.Conn) net.Conn {
	var fCfg *FragmentConfig

	if cfg := GetRequestConfig(ctx); cfg != nil {
		if cfg.PacketPadding != nil && cfg.PacketPadding.MaxSegmentSize > 0 {
			conn = c.WithMSSLimit(conn, cfg.PacketPadding.MaxSegmentSize)
		}

		if len(cfg.OrderedHeaders) > 0 {
			conn = &h2.HeaderOrderingConn{Conn: conn, OrderedKeys: cfg.OrderedHeaders}
		}

		if cfg.Fragment != nil {
			fCfg = cfg.Fragment
		}
	}

	if fCfg != nil && fCfg.ChunkSize > 0 {
		conn = c.WithFragmentation(conn, *fCfg)
	}

	return conn
}

// WithMSSLimit wraps a connection with TCP MSS limiting.
// This forces TCP to fragment data into smaller segments, disrupting
// DPI analysis of packet length signatures during TLS handshake and
// initial data transfer.
func (connWrapper) WithMSSLimit(conn net.Conn, mss int) net.Conn {
	if mss <= 0 {
		return conn
	}

	if tc, ok := conn.(*net.TCPConn); ok {
		raw, err := tc.SyscallConn()
		if err != nil {
			return conn
		}

		_ = raw.Control(func(fd uintptr) {
			setTCPMaxSeg(fd, mss)
		})
	}

	return conn
}

func (connWrapper) WithFragmentation(conn net.Conn, cfg FragmentConfig) net.Conn {
	return &fragmentedConn{
		Conn:      conn,
		chunkSize: cfg.ChunkSize,
		maxDelay:  cfg.MaxDelay,
	}
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

func isBlockedIP(ip net.IP) bool {
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() {
		return true
	}

	// Check private IP ranges.
	if ip4 := ip.To4(); ip4 != nil {
		return ip4[0] == 0 ||
			ip4[0] == 10 ||
			(ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31) ||
			(ip4[0] == 192 && ip4[1] == 168)
	}

	if ip6 := ip.To16(); ip6 != nil {
		// Check unique local IPv6.
		return (ip6[0] & 0xfe) == 0xfc
	}

	return false
}
