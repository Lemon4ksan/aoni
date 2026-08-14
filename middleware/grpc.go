// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package middleware

import (
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/requestutil"
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

// formatGRPCTimeout formats d into a gRPC-compliant timeout header.
func formatGRPCTimeout(d time.Duration) string {
	return requestutil.FormatGRPCTimeout(d)
}
