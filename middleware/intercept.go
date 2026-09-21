// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package middleware

import (
	"github.com/lemon4ksan/aoni"
)

// Interceptor is a high-level closure that intercepts and decorates an HTTP transaction.
// It receives the active [aoni.Request] and a [aoni.RequestDoer] representing the next
// handler in the execution pipeline.
type Interceptor func(req aoni.Request, next aoni.RequestDoer) (aoni.Response, error)

type interceptDoer struct {
	fn   Interceptor
	next aoni.RequestDoer
}

func (d *interceptDoer) Do(req aoni.Request) (aoni.Response, error) {
	if d.fn == nil {
		if d.next != nil {
			return d.next.Do(req)
		}

		return nil, nil
	}

	return d.fn(req, d.next)
}

// Unwrap returns the next [aoni.RequestDoer] in the chain, enabling onion-peeling traversal.
func (d *interceptDoer) Unwrap() any {
	return d.next
}

// Intercept converts an [Interceptor] closure into a standard [aoni.Middleware] decorator.
// It simplifies custom middleware creation to a single inline function.
//
// Example:
//
//	mw := middleware.Intercept(func(req aoni.Request, next aoni.RequestDoer) (aoni.Response, error) {
//	    req.SetHeader("X-Correlation-ID", id)
//	    return next.Do(req)
//	})
//	client.Use(mw)
func Intercept(fn Interceptor) aoni.Middleware {
	return func(next aoni.RequestDoer) aoni.RequestDoer {
		return &interceptDoer{
			fn:   fn,
			next: next,
		}
	}
}
