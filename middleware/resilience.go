// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package middleware

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/sync/breaker"
	"github.com/lemon4ksan/foundation/sync/keylock"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/netutil/netdial"
)

// ErrCircuitOpen is returned when a circuit breaker blocks requests to an unhealthy host.
var ErrCircuitOpen = errors.New("aoni: circuit breaker open for target host")

// CircuitBreakerConfig configures failure thresholds, reset timeouts, and key locking for host circuit breakers.
type CircuitBreakerConfig struct {
	FailureThreshold float64
	MinRequests      int
	Cooldown         time.Duration
	Window           time.Duration
}

// CircuitBreaker manages host-isolated circuit breakers using thread-safe key locks.
type CircuitBreaker struct {
	cfg      CircuitBreakerConfig
	breakers generic.ConcurrentMap[string, *breaker.CircuitBreaker[any]]
	km       keylock.KeyMutex[string]
}

// NewCircuitBreaker initializes a new host-isolated circuit breaker registry.
func NewCircuitBreaker(cfg CircuitBreakerConfig) *CircuitBreaker {
	if math.IsNaN(cfg.FailureThreshold) || cfg.FailureThreshold <= 0 || cfg.FailureThreshold >= 1 {
		cfg.FailureThreshold = 0.5
	}

	if cfg.MinRequests <= 0 {
		cfg.MinRequests = 5
	}

	if cfg.Cooldown <= 0 {
		cfg.Cooldown = 5 * time.Second
	}

	if cfg.Window <= 0 {
		cfg.Window = 10 * time.Second
	}

	return &CircuitBreaker{
		cfg: cfg,
	}
}

// getBreaker retrieves or lazily instantiates a host-isolated [breaker.CircuitBreaker].
func (cb *CircuitBreaker) getBreaker(host string) *breaker.CircuitBreaker[any] {
	if val, ok := cb.breakers.Load(host); ok {
		return val
	}

	cb.km.Lock(host)
	defer cb.km.Unlock(host)

	if val, ok := cb.breakers.Load(host); ok {
		return val
	}

	b := breaker.New[any](breaker.Config{
		FailureThreshold: cb.cfg.FailureThreshold,
		Cooldown:         cb.cfg.Cooldown,
		MinRequests:      cb.cfg.MinRequests,
		Window:           cb.cfg.Window,
	})

	cb.breakers.Store(host, b)

	return b
}

// DefaultCircuitBreakerCondition flags 5xx server errors or connection failures as circuit breaker triggers.
func DefaultCircuitBreakerCondition(resp aoni.Response, err error) bool {
	if err != nil {
		return true
	}

	if resp != nil {
		return resp.StatusCode() >= 500
	}

	return false
}

// CircuitBreak constructs an [aoni.Middleware] protecting downstream services with host-isolated circuit breakers.
func CircuitBreak(cb *CircuitBreaker, isFailure func(aoni.Response, error) bool) aoni.Middleware {
	if isFailure == nil {
		isFailure = DefaultCircuitBreakerCondition
	}

	return func(next aoni.RequestDoer) aoni.RequestDoer {
		return aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
			host := parseHostFromURL(req.URL())
			b := cb.getBreaker(host)

			var resp aoni.Response

			_, execErr := b.Do(req.Context(), func(_ context.Context) (any, error) {
				var err error

				resp, err = next.Do(req)

				if isFailure(resp, err) {
					if err != nil {
						return nil, err
					}

					return nil, ErrCircuitOpen
				}

				return nil, nil
			})
			if execErr != nil {
				if errors.Is(execErr, breaker.ErrCircuitOpen) ||
					strings.Contains(execErr.Error(), "circuit breaker is open") {
					return resp, fmt.Errorf("aoni: circuit breaker open for host %s", host)
				}

				if errors.Is(execErr, ErrCircuitOpen) {
					return resp, nil
				}
			}

			return resp, execErr
		})
	}
}

// Fallback constructs an [aoni.Middleware] returning alternative responses when primary requests fail.
func Fallback() aoni.Middleware {
	return FallbackEx(nil)
}

// FallbackEx constructs an [aoni.Middleware] evaluating fallback conditions using a custom predicate.
func FallbackEx(isFailure func(aoni.Response, error) bool) aoni.Middleware {
	return func(next aoni.RequestDoer) aoni.RequestDoer {
		return aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
			resp, err := next.Do(req)

			failed := false
			if isFailure != nil {
				failed = isFailure(resp, err)
			} else {
				failed = err != nil || (resp != nil && resp.StatusCode() >= 500)
			}

			if !failed {
				return resp, err
			}

			cfg := aoni.GetRequestConfig(req.Context())
			if cfg != nil && cfg.Fallback != nil {
				fbResp, fbErr := cfg.Fallback(req, err)
				if fbErr == nil && fbResp != nil {
					return fbResp, nil
				}
			}

			return resp, err
		})
	}
}

// ChaosConfig configures artificial latency injection and fault generation probabilities for testing.
type ChaosConfig struct {
	LatencyProb float64
	MinLatency  time.Duration
	LatencyMin  time.Duration
	MaxLatency  time.Duration
	LatencyMax  time.Duration
	ErrorProb   float64
	FailureRate float64
}

// Chaos constructs an [aoni.Middleware] injecting synthetic latency and network faults into execution pipelines.
func Chaos(cfg ChaosConfig) aoni.Middleware {
	return func(next aoni.RequestDoer) aoni.RequestDoer {
		return aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
			ctx := req.Context()

			if err := applyChaosDelay(ctx, cfg); err != nil {
				return nil, err
			}

			errProb := cfg.ErrorProb
			if errProb == 0 && cfg.FailureRate > 0 {
				errProb = cfg.FailureRate
			}

			if shouldInjectChaosError(errProb) {
				stdResp := &http.Response{
					StatusCode: http.StatusServiceUnavailable,
					Body:       io.NopCloser(strings.NewReader("503 Service Unavailable (Chaos Injected)")),
				}

				return aoni.NewStdResponse(stdResp), nil
			}

			return next.Do(req)
		})
	}
}

// applyChaosDelay introduces artificial latency into request execution according to [ChaosConfig].
func applyChaosDelay(ctx context.Context, cfg ChaosConfig) error {
	minD := cfg.MinLatency
	if minD == 0 && cfg.LatencyMin > 0 {
		minD = cfg.LatencyMin
	}

	maxD := cfg.MaxLatency
	if maxD == 0 && cfg.LatencyMax > 0 {
		maxD = cfg.LatencyMax
	}

	prob := cfg.LatencyProb
	if prob <= 0 && (minD > 0 || maxD > 0) {
		prob = 1.0
	}

	if prob <= 0 || (minD <= 0 && maxD <= 0) {
		return nil
	}

	n, _ := rand.Int(rand.Reader, big.NewInt(10000))
	if float64(n.Int64())/10000.0 >= prob {
		return nil
	}

	if minD > maxD {
		minD, maxD = maxD, minD
	}

	diff := maxD - minD

	delay := minD
	if diff > 0 {
		randOffset, _ := rand.Int(rand.Reader, big.NewInt(int64(diff)))
		delay += time.Duration(randOffset.Int64())
	}

	t := time.NewTimer(delay)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// shouldInjectChaosError determines whether to inject a simulated HTTP 503 error.
func shouldInjectChaosError(rate float64) bool {
	if rate <= 0 {
		return false
	}

	n, _ := rand.Int(rand.Reader, big.NewInt(10000))

	return float64(n.Int64())/10000.0 < rate
}

// isFatalError determines if err is non-recoverable and should terminate retries immediately.
func isFatalError(err error) bool {
	if errors.Is(err, netdial.ErrSSRFBlocked) ||
		errors.Is(err, netdial.ErrCertificatePinning) ||
		errors.Is(err, netdial.ErrUTLSHandshakeFailed) ||
		errors.Is(err, context.Canceled) {
		return true
	}

	var tlsErr *tls.CertificateVerificationError
	if errors.As(err, &tlsErr) {
		return true
	}

	var unknownAuth *x509.UnknownAuthorityError

	return errors.As(err, &unknownAuth)
}

// parseRetryAfter parses standard HTTP Retry-After headers (seconds or HTTP date).
func parseRetryAfter(resp aoni.Response) (time.Duration, bool) {
	if resp == nil {
		return 0, false
	}

	val := resp.Header("Retry-After")
	if val == "" {
		return 0, false
	}

	if seconds, err := strconv.ParseInt(val, 10, 64); err == nil && seconds >= 0 {
		maxSec := int64(math.MaxInt64 / int64(time.Second))
		if seconds > maxSec {
			return time.Duration(math.MaxInt64), true
		}

		return time.Duration(seconds) * time.Second, true
	} else if err != nil {
		var numErr *strconv.NumError
		if errors.As(err, &numErr) && errors.Is(numErr.Err, strconv.ErrRange) {
			return time.Duration(math.MaxInt64), true
		}
	}

	if t, err := http.ParseTime(val); err == nil {
		dur := max(time.Until(t), 0)

		return dur, true
	}

	return 0, false
}

// parseHostFromURL extracts the target hostname without port from rawURL.
func parseHostFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}

	host, _, err := net.SplitHostPort(u.Host)
	if err != nil {
		return u.Host
	}

	return host
}
