// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package middleware provides composable HTTP request and response execution interceptors.
package middleware

import (
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/telemetry"
)

// Chain composes an execution engine with an ordered sequence of [aoni.Middleware] layers.
//
// Execution Order:
// Interceptors execute in left-to-right order (the first middleware wraps the second, and so on).
func Chain(doer any, middlewares ...aoni.Middleware) aoni.RequestDoer {
	var rd aoni.RequestDoer
	switch d := doer.(type) {
	case aoni.RequestDoer:
		rd = d
	case aoni.HTTPDoer:
		rd = aoni.NewHTTPDoerAdapter(d)
	default:
		panic("middleware: invalid doer type passed to Chain")
	}

	for i := len(middlewares) - 1; i >= 0; i-- {
		if middlewares[i] != nil {
			rd = middlewares[i](rd)
		}
	}

	return rd
}

// Log records structured execution telemetry for HTTP transactions using logger.
// Sensitive query parameters ("key", "token", "access_token") are automatically masked.
func Log(logger telemetry.Logger) aoni.Middleware {
	return func(next aoni.RequestDoer) aoni.RequestDoer {
		return aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
			startTime := time.Now()
			resp, err := next.Do(req)

			logger.Log(req.Context(), telemetry.LevelInfo, "http request",
				"method", req.Method(),
				"url", maskURLString(req.URL()),
				"duration", time.Since(startTime),
				"error", err,
			)

			return resp, err
		})
	}
}

var ErrPanic = errors.New("aoni: panic recovered during request execution")

// Recover constructs an [aoni.Middleware] intercepting and capturing runtime panics.
func Recover(onPanic func(any)) aoni.Middleware {
	return func(next aoni.RequestDoer) aoni.RequestDoer {
		return aoni.DoerFunc(func(req aoni.Request) (resp aoni.Response, err error) {
			defer func() {
				if r := recover(); r != nil {
					if onPanic != nil {
						onPanic(r)
					}

					err = fmt.Errorf("%w: %v", ErrPanic, r)
				}
			}()

			return next.Do(req)
		})
	}
}

// maskURLString redacts sensitive query parameters from rawURL.
func maskURLString(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	return maskQueryParams(u)
}

// maskQueryParams redacts values of sensitive query parameters (key, token, password, etc.).
func maskQueryParams(u *url.URL) string {
	if u == nil {
		return ""
	}

	q := u.Query()
	sensitive := []string{"key", "token", "access_token", "api_key", "secret", "password"}

	masked := false
	for _, key := range sensitive {
		if q.Has(key) {
			q.Set(key, "***")

			masked = true
		}
	}

	if !masked {
		return u.String()
	}

	cloned := *u
	cloned.RawQuery = q.Encode()

	return cloned.String()
}
