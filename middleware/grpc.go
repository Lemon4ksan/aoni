// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package middleware

import (
	"strconv"
	"time"

	"github.com/lemon4ksan/aoni"
)

// GRPCWebTimeout constructs an [aoni.Middleware] setting standard gRPC-Web timeout headers ("grpc-timeout").
func GRPCWebTimeout(d time.Duration) aoni.Middleware {
	return func(next aoni.RequestDoer) aoni.RequestDoer {
		return aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
			req.SetHeader("grpc-timeout", formatGRPCTimeout(d))
			return next.Do(req)
		})
	}
}

// GRPCMetadata constructs an [aoni.Middleware] injecting gRPC-Web binary metadata headers.
func GRPCMetadata(md map[string]string) aoni.Middleware {
	return func(next aoni.RequestDoer) aoni.RequestDoer {
		return aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
			for k, v := range md {
				req.SetHeader(k, v)
			}

			return next.Do(req)
		})
	}
}

func formatGRPCTimeout(d time.Duration) string {
	if d <= 0 {
		return "0m"
	}

	if d >= time.Hour && d%time.Hour == 0 {
		return strconv.FormatInt(int64(d/time.Hour), 10) + "H"
	}

	if d >= time.Minute && d%time.Minute == 0 {
		return strconv.FormatInt(int64(d/time.Minute), 10) + "M"
	}

	if d >= time.Second && d%time.Second == 0 {
		return strconv.FormatInt(int64(d/time.Second), 10) + "S"
	}

	ms := d.Milliseconds()
	if ms == 0 {
		ms = 1
	}

	return strconv.FormatInt(ms, 10) + "m"
}
