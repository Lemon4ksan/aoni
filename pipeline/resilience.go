// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/iokit"
	"github.com/lemon4ksan/foundation/silicon/pool"

	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/ext/telemetry"
	"github.com/lemon4ksan/aoni/internal/core"
)

var ErrHedgingBodyNonRepeatable = errors.New("aoni: request body is not repeatable for hedging attempt")

// dispatchRequest routes the transaction through the configured pipeline (Failover, Hedging, or standard Doer).
//
// It intercepts specific protocol failure status codes to trigger transparent in-flight recovery:
//   - HTTP 421 (Misdirected Request): Disables Alt-Svc (HTTP/3) routing and re-dials over a fresh connection.
//   - HTTP 408 (Request Timeout): Enforces 'Connection: close', purging the stale socket from the pool.
//   - HTTP 425 (Too Early): Strips the 'Early-Data' header, disables 0-RTT, and retries safely over 1-RTT.
//
// DispatchRequest executes the HTTP request handling retries, hedging, and proxy failovers.
func (h *StdHandler) DispatchRequest(
	req *http.Request,
	genericDoer core.GenericDoer[*http.Request, *http.Response],
	tx *Tx,
) (*http.Response, error) {
	doer, _ := genericDoer.(Doer)
	if doer == nil {
		doer = DoerFunc(func(r *http.Request) (*http.Response, error) {
			return genericDoer.Do(r)
		})
	}

	var (
		resp *http.Response
		err  error
	)

	switch {
	case tx.Flags&FlagProxyFailover != 0 && tx.ProxyFailover != nil:
		resp, err = h.executeWithProxyFailover(req, doer, tx.ProxyFailover, tx.Hedging)
	case tx.Flags&FlagHedging != 0 && tx.Hedging != nil:
		resp, err = h.executeWithHedging(req, doer, tx.Hedging)
	default:
		resp, err = doer.Do(req)
	}

	if resp == nil {
		return nil, err
	}

	switch resp.StatusCode {
	case http.StatusMisdirectedRequest:
		return h.handle421Recovery(req, doer, resp)
	case http.StatusRequestTimeout:
		return h.handle408Recovery(req, doer, resp)
	case http.StatusTooEarly:
		return h.handle425Recovery(req, doer, resp)
	}

	if resp.Request == nil {
		resp.Request = req
	}

	return resp, err
}

func (h *StdHandler) retryWithMutation(
	req *http.Request,
	doer Doer,
	origResp *http.Response,
	mutate func(r *http.Request, cfg *RequestConfig),
) (*http.Response, error) {
	if origResp != nil && origResp.Body != nil {
		_ = origResp.Body.Close()
	}

	if httpClient, ok := doer.(*http.Client); ok {
		httpClient.CloseIdleConnections()
	}

	clonedReq, err := h.cloneRequest(req, req.Context())
	if err != nil {
		return nil, err
	}

	reqCfg := GetOrInitRequestConfig(clonedReq.Context())
	if mutate != nil {
		mutate(clonedReq, reqCfg)
	}

	retryResp, retryErr := doer.Do(clonedReq)
	if retryResp != nil && retryResp.Request == nil {
		retryResp.Request = clonedReq
	}

	return retryResp, retryErr
}

func (h *StdHandler) handle425Recovery(
	req *http.Request,
	doer Doer,
	origResp *http.Response,
) (*http.Response, error) {
	return h.retryWithMutation(req, doer, origResp, func(r *http.Request, cfg *RequestConfig) {
		cfg.Disable0RTT = true

		r.Header.Del("Early-Data")
	})
}

func (h *StdHandler) handle408Recovery(
	req *http.Request,
	doer Doer,
	origResp *http.Response,
) (*http.Response, error) {
	return h.retryWithMutation(req, doer, origResp, func(r *http.Request, _ *RequestConfig) {
		r.Close = true
	})
}

func (h *StdHandler) handle421Recovery(
	req *http.Request,
	doer Doer,
	origResp *http.Response,
) (*http.Response, error) {
	return h.retryWithMutation(req, doer, origResp, func(r *http.Request, cfg *RequestConfig) {
		cfg.DisableAltSvc = true

		r.Header.Del("Alt-Svc")
	})
}

func (h *StdHandler) executeWithProxyFailover(
	req *http.Request,
	doer Doer,
	failover *ProxyFailoverConfig,
	hedging *HedgingConfig,
) (*http.Response, error) {
	proxies := parseProxyURLs(failover.Proxies)
	if len(proxies) == 0 {
		return h.dispatchProxyAttempt(req, doer, hedging)
	}

	retryLimit := generic.Coalesce(failover.RetryLimit, len(proxies))

	var lastErr error
	for attempt := 0; attempt <= retryLimit; attempt++ {
		proxyURL := h.selectNextProxy(proxies, attempt > 0)

		proxyReq, prepErr := h.prepareRequestForProxy(req, proxyURL)
		if prepErr != nil {
			lastErr = prepErr
			continue
		}

		resp, err := h.dispatchProxyAttempt(proxyReq, doer, hedging)
		if err != nil {
			lastErr = err
			continue
		}

		if resp == nil {
			lastErr = errors.New("aoni: proxy failover received nil response")
			continue
		}

		if resp.StatusCode != http.StatusBadGateway && resp.StatusCode != http.StatusServiceUnavailable {
			return resp, nil
		}

		_ = resp.Body.Close()
		lastErr = fmt.Errorf("aoni: proxy failover received HTTP status %d from %s", resp.StatusCode, proxyURL.String())
	}

	return nil, lastErr
}

func (h *StdHandler) dispatchProxyAttempt(
	req *http.Request,
	doer Doer,
	hedging *HedgingConfig,
) (*http.Response, error) {
	if hedging != nil {
		return h.executeWithHedging(req, doer, hedging)
	}

	return doer.Do(req)
}

func parseProxyURLs(proxies []string) []*url.URL {
	parsed := make([]*url.URL, 0, len(proxies))
	for _, pr := range proxies {
		if u, err := url.Parse(pr); err == nil {
			parsed = append(parsed, u)
		}
	}

	return parsed
}

func (h *StdHandler) selectNextProxy(proxies []*url.URL, isRetry bool) *url.URL {
	var idx uint32
	if isRetry {
		idx = h.counter.Add(1)
	} else {
		idx = h.counter.Load()
	}

	return proxies[idx%uint32(len(proxies))] //nolint:gosec
}

func (h *StdHandler) prepareRequestForProxy(req *http.Request, proxyURL *url.URL) (*http.Request, error) {
	newReq := req

	cfg := GetRequestConfig(req)
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

func isIdempotentMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

func (h *StdHandler) executeWithHedging(
	req *http.Request,
	doer Doer,
	pipeHedging *HedgingConfig,
) (*http.Response, error) {
	cfg := GetRequestConfig(req)

	allowNonReadOnly := (cfg != nil && cfg.AllowNonReadOnlyHedging) ||
		(pipeHedging != nil && pipeHedging.AllowNonReadOnly)

	if !allowNonReadOnly && !isIdempotentMethod(req.Method) {
		return doer.Do(req)
	}

	requestStart := time.Now()
	delay := h.resolveHedgingDelay(cfg, pipeHedging)

	var (
		resp *http.Response
		err  error
	)

	if delay > 0 {
		resp, err = h.dispatchHedgingAttempts(req, doer, delay)
	} else {
		resp, err = doer.Do(req)
	}

	tracker := h.resolveRTTTracker(pipeHedging)
	if tracker != nil && err == nil {
		tracker.Record(time.Since(requestStart))
	}

	return resp, err
}

func (h *StdHandler) resolveHedgingDelay(cfg *RequestConfig, pipeHedging *HedgingConfig) time.Duration {
	switch {
	case cfg != nil && cfg.HedgingDelayOverride != nil:
		return *cfg.HedgingDelayOverride
	case pipeHedging != nil && pipeHedging.DynamicHedging != nil:
		return pipeHedging.DynamicHedging.ComputeDelay()
	case pipeHedging != nil:
		return pipeHedging.DefaultDelay
	default:
		return 0
	}
}

func (h *StdHandler) resolveRTTTracker(pipeHedging *HedgingConfig) *telemetry.RTTTracker {
	if pipeHedging != nil && pipeHedging.DynamicHedging != nil {
		return pipeHedging.DynamicHedging.Tracker
	}

	return nil
}

type hedgeResult struct {
	resp *http.Response
	err  error
}

func (h *StdHandler) dispatchHedgingAttempts(
	req *http.Request,
	doer Doer,
	delay time.Duration,
) (*http.Response, error) {
	resultsCh := make(chan hedgeResult, 2)

	ctx1, ctx2, cancel1, cancel2, cleanup := h.buildHedgeContext(req)
	defer func() { cleanup(0) }()

	h.launchHedgeAttempt(ctx1, req, doer, resultsCh)

	timer := pool.AcquireTimer(delay)
	defer pool.ReleaseTimer(timer)

	var (
		req2Started bool
		firstErr    error
	)

	drainRemainder := func(remaining int) {
		if remaining <= 0 {
			return
		}

		go func(count int) {
			for range count {
				r := <-resultsCh
				if r.resp != nil && r.resp.Body != nil {
					_ = r.resp.Body.Close()
				}
			}
		}(remaining)
	}

	activeCount := 1

	for activeCount > 0 {
		select {
		case <-req.Context().Done():
			drainRemainder(activeCount)
			return nil, req.Context().Err()

		case <-timer.C:
			if !req2Started {
				req2Started = true
				activeCount++

				h.launchHedgeAttempt(ctx2, req, doer, resultsCh)
			}

		case res := <-resultsCh:
			activeCount--

			if res.err == nil {
				drainRemainder(activeCount)
				return h.handleHedgeWinner(res, ctx2, cancel1, cancel2, cleanup), nil
			}

			if firstErr == nil {
				firstErr = res.err
			}

			if activeCount == 0 && !req2Started {
				timer.Stop()

				req2Started = true
				activeCount++

				h.launchHedgeAttempt(ctx2, req, doer, resultsCh)
			}
		}
	}

	return nil, firstErr
}

func (h *StdHandler) handleHedgeWinner(
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

	res.resp.Body = &iokit.ContextCancelingReadCloser{
		ReadCloser: res.resp.Body,
		Cancel:     cancelWinner,
	}

	return res.resp
}

func (h *StdHandler) launchHedgeAttempt(
	ctx context.Context,
	req *http.Request,
	doer Doer,
	resultsCh chan<- hedgeResult,
) {
	cloned, err := h.cloneRequest(req, ctx)
	if err != nil {
		resultsCh <- hedgeResult{err: err}
		return
	}

	go func() {
		resp, err := doer.Do(cloned) //nolint:bodyclose
		resultsCh <- hedgeResult{resp: resp, err: err}
	}()
}

func (h *StdHandler) buildHedgeContext(
	req *http.Request,
) (context.Context, context.Context, context.CancelFunc, context.CancelFunc, func(winner int)) {
	ctx := req.Context()
	ctx1, cancel1 := context.WithCancel(ctx)
	ctx2, cancel2 := context.WithCancel(ctx)

	var cleaned atomic.Bool

	cleanup := func(winner int) {
		if !cleaned.CompareAndSwap(false, true) {
			return
		}

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

func (h *StdHandler) cloneRequest(orig *http.Request, reqCtx context.Context) (*http.Request, error) {
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
