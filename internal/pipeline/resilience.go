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
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/foundation/generic"

	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/internal/io"
	"github.com/lemon4ksan/aoni/telemetry"
)

var ErrHedgingBodyNonRepeatable = errors.New("aoni: request body is not repeatable for hedging attempt")

func (p *Pipeline[Req, Resp]) dispatchRequest(req *http.Request, doer Doer, tx *Tx) (*http.Response, error) {
	var (
		resp *http.Response
		err  error
	)

	switch {
	case tx.Flags&FlagProxyFailover != 0 && tx.ProxyFailover != nil:
		resp, err = p.executeWithProxyFailover(req, doer, tx.ProxyFailover, tx.Hedging)
	case tx.Flags&FlagHedging != 0 && tx.Hedging != nil:
		resp, err = p.executeWithHedging(req, doer, tx.Hedging)
	default:
		resp, err = doer.Do(req)
	}

	if resp != nil {
		if resp.StatusCode == http.StatusMisdirectedRequest {
			return p.handle421Recovery(req, doer, resp)
		}

		if resp.StatusCode == http.StatusRequestTimeout {
			return p.handle408Recovery(req, doer, resp)
		}

		if resp.StatusCode == http.StatusTooEarly {
			return p.handle425Recovery(req, doer, resp)
		}

		if resp.Request == nil {
			resp.Request = req
		}
	}

	return resp, err
}

func (p *Pipeline[Req, Resp]) handle425Recovery(
	req *http.Request,
	doer Doer,
	origResp *http.Response,
) (*http.Response, error) {
	if origResp != nil && origResp.Body != nil {
		_ = origResp.Body.Close()
	}

	if httpClient, ok := doer.(*http.Client); ok {
		httpClient.CloseIdleConnections()
	}

	reqCfg := GetOrInitRequestConfig(req.Context())
	reqCfg.Disable0RTT = true

	clonedReq, err := p.cloneRequest(req, req.Context())
	if err != nil {
		return nil, err
	}

	clonedReq.Header.Del("Early-Data")

	retryResp, retryErr := doer.Do(clonedReq)
	if retryResp != nil && retryResp.Request == nil {
		retryResp.Request = clonedReq
	}

	return retryResp, retryErr
}

func (p *Pipeline[Req, Resp]) handle408Recovery(
	req *http.Request,
	doer Doer,
	origResp *http.Response,
) (*http.Response, error) {
	if origResp != nil && origResp.Body != nil {
		_ = origResp.Body.Close()
	}

	if httpClient, ok := doer.(*http.Client); ok {
		httpClient.CloseIdleConnections()
	}

	clonedReq, err := p.cloneRequest(req, req.Context())
	if err != nil {
		return nil, err
	}

	clonedReq.Close = true

	retryResp, retryErr := doer.Do(clonedReq)
	if retryResp != nil && retryResp.Request == nil {
		retryResp.Request = clonedReq
	}

	return retryResp, retryErr
}

func (p *Pipeline[Req, Resp]) handle421Recovery(
	req *http.Request,
	doer Doer,
	origResp *http.Response,
) (*http.Response, error) {
	if origResp != nil && origResp.Body != nil {
		_ = origResp.Body.Close()
	}

	reqCfg := GetOrInitRequestConfig(req.Context())
	reqCfg.DisableAltSvc = true

	if httpClient, ok := doer.(*http.Client); ok {
		httpClient.CloseIdleConnections()
	}

	clonedReq, err := p.cloneRequest(req, req.Context())
	if err != nil {
		return nil, err
	}

	clonedReq.Header.Del("Alt-Svc")

	retryResp, retryErr := doer.Do(clonedReq)
	if retryResp != nil && retryResp.Request == nil {
		retryResp.Request = clonedReq
	}

	return retryResp, retryErr
}

func (p *Pipeline[Req, Resp]) executeWithProxyFailover(
	req *http.Request,
	doer Doer,
	failover *ProxyFailoverConfig,
	hedging *HedgingConfig,
) (*http.Response, error) {
	proxies := parseProxyURLs(failover.Proxies)
	if len(proxies) == 0 {
		return p.dispatchProxyAttempt(req, doer, hedging)
	}

	retryLimit := generic.Coalesce(failover.RetryLimit, len(proxies))

	var lastErr error
	for attempt := 0; attempt <= retryLimit; attempt++ {
		proxyURL := p.selectNextProxy(proxies, attempt > 0)

		proxyReq, prepErr := p.prepareRequestForProxy(req, proxyURL)
		if prepErr != nil {
			lastErr = prepErr
			continue
		}

		resp, err := p.dispatchProxyAttempt(proxyReq, doer, hedging)
		if err == nil && resp != nil {
			if resp.StatusCode != http.StatusBadGateway && resp.StatusCode != http.StatusServiceUnavailable {
				return resp, nil
			}

			_ = resp.Body.Close()
		}

		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf(
				"aoni: proxy failover received HTTP status %d from %s",
				resp.StatusCode,
				proxyURL.String(),
			)
		}
	}

	return nil, lastErr
}

func (p *Pipeline[Req, Resp]) dispatchProxyAttempt(
	req *http.Request,
	doer Doer,
	hedging *HedgingConfig,
) (*http.Response, error) {
	if hedging != nil {
		return p.executeWithHedging(req, doer, hedging)
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

func (p *Pipeline[Req, Resp]) selectNextProxy(proxies []*url.URL, isRetry bool) *url.URL {
	var idx uint32
	if isRetry {
		idx = atomic.AddUint32(&p.counter, 1)
	} else {
		idx = atomic.LoadUint32(&p.counter)
	}

	return proxies[idx%uint32(len(proxies))] //nolint:gosec
}

func (p *Pipeline[Req, Resp]) prepareRequestForProxy(req *http.Request, proxyURL *url.URL) (*http.Request, error) {
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

func isIdempotentMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

func (p *Pipeline[Req, Resp]) executeWithHedging(
	req *http.Request,
	doer Doer,
	pipeHedging *HedgingConfig,
) (*http.Response, error) {
	cfg := GetRequestConfig(req.Context())

	allowNonReadOnly := (cfg != nil && cfg.AllowNonReadOnlyHedging) ||
		(pipeHedging != nil && pipeHedging.AllowNonReadOnly)

	if !allowNonReadOnly && !isIdempotentMethod(req.Method) {
		return doer.Do(req)
	}

	requestStart := time.Now()
	delay := p.resolveHedgingDelay(cfg, pipeHedging)

	var (
		resp *http.Response
		err  error
	)

	if delay > 0 {
		resp, err = p.dispatchHedgingAttempts(req, doer, delay)
	} else {
		resp, err = doer.Do(req)
	}

	tracker := p.resolveRTTTracker(pipeHedging)
	if tracker != nil && err == nil {
		tracker.Record(time.Since(requestStart))
	}

	return resp, err
}

func (p *Pipeline[Req, Resp]) resolveHedgingDelay(cfg *RequestConfig, pipeHedging *HedgingConfig) time.Duration {
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

func (p *Pipeline[Req, Resp]) resolveRTTTracker(pipeHedging *HedgingConfig) *telemetry.RTTTracker {
	if pipeHedging != nil && pipeHedging.DynamicHedging != nil {
		return pipeHedging.DynamicHedging.Tracker
	}

	return nil
}

type hedgeResult struct {
	resp *http.Response
	err  error
}

func (p *Pipeline[Req, Resp]) dispatchHedgingAttempts(
	req *http.Request,
	doer Doer,
	delay time.Duration,
) (*http.Response, error) {
	resultsCh := make(chan hedgeResult, 2)

	ctx1, ctx2, cancel1, cancel2, cleanup := p.buildHedgeContext(req)
	defer func() { cleanup(0) }()

	p.launchHedgeAttempt(ctx1, req, doer, resultsCh)

	timer := time.NewTimer(delay)
	defer timer.Stop()

	var (
		req2Started bool
		firstErr    error
	)

	drainRemainder := func(remaining int) {
		if remaining <= 0 {
			return
		}

		go func(count int) {
			for i := 0; i < count; i++ {
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

				p.launchHedgeAttempt(ctx2, req, doer, resultsCh)
			}

		case res := <-resultsCh:
			activeCount--

			if res.err == nil {
				drainRemainder(activeCount)
				return p.handleHedgeWinner(res, ctx2, cancel1, cancel2, cleanup), nil
			}

			if firstErr == nil {
				firstErr = res.err
			}

			if activeCount == 0 && !req2Started {
				timer.Stop()

				req2Started = true
				activeCount++

				p.launchHedgeAttempt(ctx2, req, doer, resultsCh)
			}
		}
	}

	return nil, firstErr
}

func (p *Pipeline[Req, Resp]) handleHedgeWinner(
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

func (p *Pipeline[Req, Resp]) launchHedgeAttempt(
	ctx context.Context,
	req *http.Request,
	doer Doer,
	resultsCh chan<- hedgeResult,
) {
	cloned, err := p.cloneRequest(req, ctx)
	if err != nil {
		resultsCh <- hedgeResult{err: err}
		return
	}

	go func() {
		resp, err := doer.Do(cloned) //nolint:bodyclose
		resultsCh <- hedgeResult{resp: resp, err: err}
	}()
}

func (p *Pipeline[Req, Resp]) buildHedgeContext(
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

func (p *Pipeline[Req, Resp]) cloneRequest(orig *http.Request, reqCtx context.Context) (*http.Request, error) {
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
