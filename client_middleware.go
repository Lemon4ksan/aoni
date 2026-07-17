// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/transform"
)

// ResponseSizeLimitMiddleware enforces response size limits.
func ResponseSizeLimitMiddleware(maxSize int64) Middleware {
	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
			resp, err := next.Do(req)
			if err != nil || resp == nil || resp.Body == nil || maxSize <= 0 {
				return resp, err
			}

			if resp.ContentLength > maxSize {
				_ = resp.Body.Close()
				return nil, fmt.Errorf("aoni: response too large: %w", ErrResponseTooLarge)
			}

			resp.Body = &limitCheckingReadCloser{
				ReadCloser: resp.Body,
				limit:      maxSize,
			}

			return resp, nil
		})
	}
}

// ResponseValidationMiddleware executes the validator registered on the request context.
func ResponseValidationMiddleware() Middleware {
	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
			resp, err := next.Do(req)
			if err != nil {
				return nil, err
			}

			if resp != nil {
				if fn := GetResponseValidator(req.Context()); fn != nil {
					if validErr := fn(resp); validErr != nil {
						if resp.Body != nil {
							_ = resp.Body.Close()
						}

						return nil, validErr
					}
				}
			}

			return resp, nil
		})
	}
}

// RefererAutomatonMiddleware manages Referer headers automatically.
func RefererAutomatonMiddleware(enabled bool, state *RefererState) Middleware {
	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
			if !enabled || state == nil {
				return next.Do(req)
			}

			if req.Header.Get("Referer") == "" {
				state.mu.Lock()
				lastURL := state.lastURL
				state.mu.Unlock()

				if lastURL != "" {
					req.Header.Set("Referer", lastURL)
				}
			}

			resp, err := next.Do(req)
			if err == nil && req != nil && req.URL != nil {
				state.mu.Lock()
				state.lastURL = req.URL.String()
				state.mu.Unlock()
			}

			return resp, err
		})
	}
}

// DecompressionAndTranscodingMiddleware handles automatic decompression and charset transcoding.
func DecompressionAndTranscodingMiddleware() Middleware {
	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
			resp, err := next.Do(req)
			if err != nil || resp == nil || resp.Body == nil {
				return resp, err
			}

			// Trigger download progress callback.
			if onProgress, ok := req.Context().Value(downloadProgressCtxKey{}).(ProgressFunc); ok && onProgress != nil {
				resp.Body = &progressReader{
					reader:     resp.Body,
					total:      resp.ContentLength,
					onProgress: onProgress,
				}
			}

			// Handle automatic response decompression.
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
				} else {
					resp.Header.Del("Content-Encoding")
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
				} else {
					resp.Header.Del("Content-Encoding")
				}
			}

			// Transcode response from non-UTF-8 character set.
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

			return resp, nil
		})
	}
}

// MultiReadBodyMiddleware enables replayable multi-read body caching.
func MultiReadBodyMiddleware(threshold int64, disableDisk bool) Middleware {
	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
			resp, err := next.Do(req)
			if err != nil || resp == nil || resp.Body == nil {
				return resp, err
			}

			var activeThreshold int64
			if val := req.Context().Value(multiReadCtxKey{}); val != nil {
				activeThreshold = val.(int64)
			} else {
				activeThreshold = threshold
			}

			if activeThreshold > 0 {
				var activeDisableDisk bool
				if val := req.Context().Value(multiReadDisableDiskCtxKey{}); val != nil {
					activeDisableDisk = val.(bool)
				} else {
					activeDisableDisk = disableDisk
				}

				mBody, err := newMultiReadBody(resp.Body, activeThreshold, activeDisableDisk)
				if err != nil {
					_ = resp.Body.Close()
					return nil, err
				}

				resp.Body = mBody
			}

			return resp, nil
		})
	}
}

// FinalizerMiddleware prevents socket leaks via finalizer.
func FinalizerMiddleware() Middleware {
	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
			resp, err := next.Do(req)
			if err != nil || resp == nil || resp.Body == nil {
				return resp, err
			}

			resp.Body = newFinalizerReadCloser(resp.Body)

			return resp, nil
		})
	}
}

// HooksMiddleware executes client-level beforeRequest/afterResponse hooks.
func HooksMiddleware(before []func(*http.Request), after []func(*http.Response, error)) Middleware {
	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
			for _, hook := range before {
				hook(req)
			}

			resp, err := next.Do(req)
			for _, hook := range after {
				hook(resp, err)
			}

			return resp, err
		})
	}
}

// PacketPaddingMiddleware adds packet padding header to disrupt DPI analysis.
func PacketPaddingMiddleware(cfg *PaddingConfig) Middleware {
	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
			if cfg != nil {
				if padding := GeneratePadding(*cfg); len(padding) > 0 {
					headerName := PaddingHeaderName(*cfg)
					req.Header.Set(headerName, hex.EncodeToString(padding))
				}
			}

			return next.Do(req)
		})
	}
}

// InspectorMiddleware handles trace/JA4 collection and Traffic Inspector logging.
func InspectorMiddleware(inspector TrafficInspector) Middleware {
	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
			if inspector == nil {
				return next.Do(req)
			}

			traceInfo := &TraceInfo{}
			Trace(traceInfo)(req)
			TraceJA4(traceInfo)(req)
			traceEnd := traceInfo.Start()

			resp, err := next.Do(req)

			if traceEnd != nil {
				traceEnd(resp)
			}

			inspector.Capture(req, resp, err, traceInfo)

			return resp, err
		})
	}
}

// ChallengeSolverMiddleware handles Challenge Solver if registered.
func ChallengeSolverMiddleware(solver ChallengeSolver, detector ChallengeDetector) Middleware {
	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
			resp, err := next.Do(req)
			if err == nil && solver != nil {
				det := detector
				if det == nil {
					det = DefaultChallengeDetector
				}

				if isChallenge, challengeErr := det(resp); isChallenge {
					if resp.Body != nil {
						_ = resp.Body.Close()
					}

					newResp, solveErr := solver.Solve(req.Context(), challengeErr, req)
					if solveErr == nil {
						return newResp, nil
					}

					return nil, solveErr
				}
			}

			return resp, err
		})
	}
}

// ContextMiddleware enriches context with Client's specific configurations.
func ContextMiddleware(c *Client) Middleware {
	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
			ctx := req.Context()
			if c.network.SSRFGuard {
				ctx = context.WithValue(ctx, ssrfGuardCtxKey{}, true)
			}

			ctx = context.WithValue(ctx, happyEyeballsDelayCtxKey{}, c.network.HappyEyeballsDelay)

			if c.fingerprint.JA4Callback != nil {
				ctx = context.WithValue(ctx, ja4CallbackCtxKey{}, c.fingerprint.JA4Callback)
			}

			if c.fingerprint.P0fSignature != nil {
				ctx = context.WithValue(ctx, p0fSignatureCtxKey{}, c.fingerprint.P0fSignature)
			}

			if c.network.ProxyDNS {
				ctx = context.WithValue(ctx, proxyDNSCtxKey{}, true)
				if c.network.ProxyAddr != nil {
					ctx = context.WithValue(ctx, proxyAddrCtxKey{}, c.network.ProxyAddr)
				}
			}

			if c.network.ProxyAddr != nil {
				ctx = context.WithValue(ctx, proxyCtxKey{}, c.network.ProxyAddr.String())
			}

			if c.fingerprint.SessionCache != nil {
				ctx = context.WithValue(ctx, sessionCacheCtxKey{}, c.fingerprint.SessionCache)
				if c.network.ProxyAddr != nil {
					c.fingerprint.SessionCache.SetProxyKey(c.network.ProxyAddr.String())
				}
			}

			if c.fingerprint.PacketPadding != nil {
				ctx = context.WithValue(ctx, packetPaddingCtxKey{}, c.fingerprint.PacketPadding)
			}

			req = req.WithContext(ctx)
			resp, err := next.Do(req)

			// Copy TLS JA4 report from the dialer store to the target TraceInfo.
			if store, ok := req.Context().Value(ja4ReportCtxKey{}).(*ja4ReportStore); ok && store.target != nil &&
				store.report != nil {
				store.target.JA4.JA4 = store.report.JA4
				store.target.JA4.Protocol = store.report.Protocol
				store.target.JA4.Version = store.report.Version
				store.target.JA4.SNI = store.report.SNI
				store.target.JA4.CipherCount = store.report.CipherCount
				store.target.JA4.ExtCount = store.report.ExtCount
				store.target.JA4.ALPN = store.report.ALPN
			}

			return resp, err
		})
	}
}

// HedgingMiddleware executes request with hedging delay and records RTT.
func HedgingMiddleware(defaultDelay time.Duration, dynamicHedging *DynamicHedgingConfig) Middleware {
	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
			requestStart := time.Now()

			var delay time.Duration
			switch {
			case req.Context().Value(hedgingCtxKey{}) != nil:
				delay = req.Context().Value(hedgingCtxKey{}).(time.Duration)
			case dynamicHedging != nil:
				delay = dynamicHedging.ComputeDelay()
			default:
				delay = defaultDelay
			}

			var (
				resp *http.Response
				err  error
			)
			if delay > 0 {
				resp, err = executeWithHedging(req.Context(), next, delay, req)
			} else {
				resp, err = next.Do(req)
			}

			if dynamicHedging != nil && dynamicHedging.Tracker != nil && err == nil {
				rtt := time.Since(requestStart)
				dynamicHedging.Tracker.Record(rtt)
			}

			return resp, err
		})
	}
}

func executeWithHedging(
	ctx context.Context,
	doer HTTPDoer,
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
		resp, err := doer.Do(req1) //nolint:bodyclose
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
					resp, err := doer.Do(req2) //nolint:bodyclose
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
					resp, err := doer.Do(req2) //nolint:bodyclose
					resultsCh <- result{resp: resp, err: err}
				}()
			}

		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return nil, firstErr
}

// BrowserProfile represents a user agent aligned with its modern client hints.
type BrowserProfile struct {
	UserAgent   string
	ClientHints map[string]string
}

// DefaultBrowserProfiles provides a list of realistic, modern Chrome browser profiles.
var DefaultBrowserProfiles = []BrowserProfile{
	{
		UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		ClientHints: map[string]string{
			"Sec-CH-UA":          `"Not_A Brand";v="8", "Chromium";v="120", "Google Chrome";v="120"`,
			"Sec-CH-UA-Mobile":   "?0",
			"Sec-CH-UA-Platform": `"Windows"`,
		},
	},
	{
		UserAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		ClientHints: map[string]string{
			"Sec-CH-UA":          `"Not_A Brand";v="8", "Chromium";v="120", "Google Chrome";v="120"`,
			"Sec-CH-UA-Mobile":   "?0",
			"Sec-CH-UA-Platform": `"macOS"`,
		},
	},
	{
		UserAgent: "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36",
		ClientHints: map[string]string{
			"Sec-CH-UA":          `"Not_A Brand";v="8", "Chromium";v="120", "Google Chrome";v="120"`,
			"Sec-CH-UA-Mobile":   "?1",
			"Sec-CH-UA-Platform": `"Android"`,
		},
	},
}

// UserAgentAndHintsRotationMiddleware rotates User-Agent and corresponding Sec-Ch-Ua-* client hints
// to match the selected profile consistently on every request.
func UserAgentAndHintsRotationMiddleware(profiles []BrowserProfile) Middleware {
	if len(profiles) == 0 {
		profiles = DefaultBrowserProfiles
	}

	var (
		mu      sync.Mutex
		counter int
	)

	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
			mu.Lock()
			prof := profiles[counter%len(profiles)]
			counter++
			mu.Unlock()

			req.Header.Set("User-Agent", prof.UserAgent)

			for k, v := range prof.ClientHints {
				req.Header.Set(k, v)
			}

			return next.Do(req)
		})
	}
}

type jitterReader struct {
	io.ReadCloser
	delay time.Duration
	once  sync.Once
}

func (r *jitterReader) Read(p []byte) (int, error) {
	r.once.Do(func() {
		time.Sleep(r.delay)
	})

	return r.ReadCloser.Read(p)
}

// DPIJitterMiddleware introduces a randomized delay before sending headers or reading request body.
// When request has a body (like POST), the delay staggers header send and body send phases.
func DPIJitterMiddleware(minDelay, maxDelay time.Duration) Middleware {
	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
			var delay time.Duration
			if minDelay > 0 && maxDelay >= minDelay {
				delta := maxDelay - minDelay
				if delta > 0 {
					// Seed random delay via time UnixNano
					r := time.Duration(time.Now().UnixNano() % int64(delta))
					delay = minDelay + r
				} else {
					delay = minDelay
				}
			}

			if delay > 0 {
				if req.Body != nil && req.Body != http.NoBody {
					req.Body = &jitterReader{
						ReadCloser: req.Body,
						delay:      delay,
					}
				} else {
					time.Sleep(delay)
				}
			}

			return next.Do(req)
		})
	}
}

// ProxyFailoverMiddleware handles transparent proxy switching on connection or HTTP 502/503 errors.
func ProxyFailoverMiddleware(proxies []string, retryLimit int) Middleware {
	var parsed []*url.URL
	for _, p := range proxies {
		if u, err := url.Parse(p); err == nil {
			parsed = append(parsed, u)
		}
	}

	var (
		mu      sync.Mutex
		counter int
	)

	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
			if len(parsed) == 0 {
				return next.Do(req)
			}

			var (
				lastErr error
				resp    *http.Response
			)

			for i := 0; i <= retryLimit; i++ {
				mu.Lock()

				proxy := parsed[counter%len(parsed)]
				if lastErr != nil {
					counter++
					proxy = parsed[counter%len(parsed)]
				}

				mu.Unlock()

				ctx := context.WithValue(req.Context(), proxyOverrideCtxKey{}, proxy.String())
				newReq := req.WithContext(ctx)

				if req.Body != nil && req.Body != http.NoBody && req.GetBody != nil {
					body, getBodyErr := req.GetBody()
					if getBodyErr == nil {
						newReq.Body = body
					}
				}

				resp, lastErr = next.Do(newReq)
				if lastErr == nil && resp != nil {
					if resp.StatusCode != http.StatusBadGateway && resp.StatusCode != http.StatusServiceUnavailable {
						return resp, nil
					}

					lastErr = fmt.Errorf("aoni: proxy returned status %d", resp.StatusCode)
					_ = resp.Body.Close()
				}
			}

			return nil, fmt.Errorf("aoni proxy failover: exhausted %d retries, last error: %w", retryLimit, lastErr)
		})
	}
}

// CacheStore defines the storage interface for HTTP caching.
type CacheStore interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
}

type inMemoryCacheEntry struct {
	Value     []byte
	ExpiresAt time.Time
}

// InMemoryCacheStore implements CacheStore in memory.
type InMemoryCacheStore struct {
	mu    sync.RWMutex
	cache map[string]inMemoryCacheEntry
}

// NewInMemoryCacheStore creates a thread-safe in-memory CacheStore.
func NewInMemoryCacheStore() *InMemoryCacheStore {
	return &InMemoryCacheStore{
		cache: make(map[string]inMemoryCacheEntry),
	}
}

// Get retrieves cached response bytes from memory.
func (s *InMemoryCacheStore) Get(ctx context.Context, key string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.cache[key]
	if !ok || time.Now().After(entry.ExpiresAt) {
		return nil, errors.New("aoni cache: miss")
	}

	return entry.Value, nil
}

// Set stores response bytes in memory with TTL.
func (s *InMemoryCacheStore) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cache[key] = inMemoryCacheEntry{
		Value:     val,
		ExpiresAt: time.Now().Add(ttl),
	}

	return nil
}

type cachedResponse struct {
	StatusCode int                 `json:"status_code"`
	Header     map[string][]string `json:"header"`
	BodyBase64 string              `json:"body_base64"`
}

// CacheMiddleware intercepts GET requests and serves them from CacheStore if valid.
func CacheMiddleware(store CacheStore, defaultTTL time.Duration) Middleware {
	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodGet {
				return next.Do(req)
			}

			// Respect no-cache requests
			if cc := req.Header.Get(
				"Cache-Control",
			); strings.Contains(cc, "no-cache") ||
				strings.Contains(cc, "no-store") {
				return next.Do(req)
			}

			cacheKey := req.Method + ":" + req.URL.String()
			if cachedData, err := store.Get(req.Context(), cacheKey); err == nil {
				var cached cachedResponse
				if decodeErr := json.Unmarshal(cachedData, &cached); decodeErr == nil {
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
			}

			resp, err := next.Do(req)
			if err != nil || resp == nil || resp.StatusCode != http.StatusOK {
				return resp, err
			}

			// Check response cache control
			respCC := resp.Header.Get("Cache-Control")
			if strings.Contains(respCC, "no-store") || strings.Contains(respCC, "private") {
				return resp, nil
			}

			var bodyBuf bytes.Buffer

			tee := io.TeeReader(resp.Body, &bodyBuf)

			// We need to read the body to cache it, then restore it
			bodyBytes, readErr := io.ReadAll(tee)
			if readErr != nil {
				return resp, nil //nolint:nilerr
			}

			_ = resp.Body.Close()
			resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))

			cached := cachedResponse{
				StatusCode: resp.StatusCode,
				Header:     resp.Header,
				BodyBase64: base64.StdEncoding.EncodeToString(bodyBytes),
			}

			if cachedData, marshalErr := json.Marshal(cached); marshalErr == nil {
				_ = store.Set(req.Context(), cacheKey, cachedData, defaultTTL)
			}

			return resp, nil
		})
	}
}

// RedactConfig holds the configuration for redacting sensitive data in requests.
type RedactConfig struct {
	Headers map[string]bool
}

// RedactConfigCtxKey is the context key used to store the RedactConfig in the request context.
type RedactConfigCtxKey struct{}

// SensitiveDataRedactorMiddleware masks sensitive headers such as Authorization or Cookie
// for the inspector or logs without modifying the actual network request.
func SensitiveDataRedactorMiddleware(headersToRedact, jsonKeysToRedact []string) Middleware {
	headersMap := make(map[string]bool)
	for _, h := range headersToRedact {
		headersMap[strings.ToLower(h)] = true
	}

	if len(headersMap) == 0 {
		headersMap["authorization"] = true
		headersMap["cookie"] = true
		headersMap["set-cookie"] = true
	}

	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
			ctx := context.WithValue(req.Context(), RedactConfigCtxKey{}, &RedactConfig{Headers: headersMap})
			return next.Do(req.WithContext(ctx))
		})
	}
}

// HARLog represents the top-level HAR log structure.
type HARLog struct {
	Log HARLogDetail `json:"log"`
}

// HARLogDetail represents the log details in a HAR file.
type HARLogDetail struct {
	Version string     `json:"version"`
	Creator HARCreator `json:"creator"`
	Entries []HAREntry `json:"entries"`
}

// HARCreator represents the creator of the HAR file.
type HARCreator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// HAREntry represents a single request-response session entry in the HAR log.
type HAREntry struct {
	StartedDateTime string      `json:"startedDateTime"`
	Time            int64       `json:"time"`
	Request         HARRequest  `json:"request"`
	Response        HARResponse `json:"response"`
	Cache           any         `json:"cache"`
	Timings         HARTimings  `json:"timings"`
}

// HARRequest represents a captured HTTP request in the HAR log.
type HARRequest struct {
	Method      string           `json:"method"`
	URL         string           `json:"url"`
	HTTPVersion string           `json:"httpVersion"`
	Headers     []HARHeaderField `json:"headers"`
	Cookies     []any            `json:"cookies"`
	QueryString []any            `json:"queryString"`
	HeadersSize int              `json:"headersSize"`
	BodySize    int64            `json:"bodySize"`
}

// HARResponse represents a captured HTTP response in the HAR log.
type HARResponse struct {
	Status      int              `json:"status"`
	StatusText  string           `json:"statusText"`
	HTTPVersion string           `json:"httpVersion"`
	Headers     []HARHeaderField `json:"headers"`
	Cookies     []any            `json:"cookies"`
	Content     HARContent       `json:"content"`
	RedirectURL string           `json:"redirectURL"`
	HeadersSize int              `json:"headersSize"`
	BodySize    int64            `json:"bodySize"`
}

// HARHeaderField represents an HTTP header name-value pair in the HAR log.
type HARHeaderField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// HARContent represents the response body content details in the HAR log.
type HARContent struct {
	Size     int64  `json:"size"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text,omitempty"`
}

// HARTimings represents the timing metrics for a request-response session in the HAR log.
type HARTimings struct {
	Send    int64 `json:"send"`
	Wait    int64 `json:"wait"`
	Receive int64 `json:"receive"`
}

// HARGenerator accumulates HTTP request-response entries.
type HARGenerator struct {
	mu      sync.RWMutex
	entries []HAREntry
}

// NewHARGenerator creates a new HARGenerator.
func NewHARGenerator() *HARGenerator {
	return &HARGenerator{}
}

// AddEntry adds a single HAREntry thread-safely.
func (g *HARGenerator) AddEntry(entry HAREntry) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.entries = append(g.entries, entry)
}

// Export returns the serialized HAR archive JSON bytes.
func (g *HARGenerator) Export() ([]byte, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	log := HARLog{
		Log: HARLogDetail{
			Version: "1.2",
			Creator: HARCreator{
				Name:    "aoni",
				Version: "0.5.0",
			},
			Entries: g.entries,
		},
	}

	return json.MarshalIndent(log, "", "  ")
}

// HARGeneratorMiddleware logs completed sessions to the provided HARGenerator.
func HARGeneratorMiddleware(gen *HARGenerator) Middleware {
	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
			startTime := time.Now()

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

			resp, err := next.Do(req)
			duration := time.Since(startTime).Milliseconds()

			if err != nil || resp == nil {
				return resp, err
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

			entry := HAREntry{
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
			}

			gen.AddEntry(entry)

			return resp, nil
		})
	}
}
