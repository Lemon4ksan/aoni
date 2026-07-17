// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"compress/gzip"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
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
func InspectorMiddleware(inspector *TrafficInspector) Middleware {
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
