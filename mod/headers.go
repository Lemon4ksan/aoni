// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/internal/bytesconv"
)

// WithGRPCWebTimeout constructs an [aoni.RequestModifier] setting standard gRPC-Web timeout headers ("grpc-timeout").
func WithGRPCWebTimeout(d time.Duration) aoni.RequestModifier {
	return func(req aoni.Request) {
		req.SetHeader("grpc-timeout", formatGRPCTimeout(d))
	}
}

// WithGRPCMetadata constructs an [aoni.RequestModifier] injecting gRPC-Web binary metadata headers.
func WithGRPCMetadata(md map[string]string) aoni.RequestModifier {
	return func(req aoni.Request) {
		for k, v := range md {
			req.SetHeader(k, v)
		}
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

// WithHeader constructs an [aoni.RequestModifier] setting a single request header key to value.
func WithHeader(key, value string) aoni.RequestModifier {
	return func(req aoni.Request) {
		req.SetHeader(key, value)
	}
}

// WithHeaderBytes constructs an [aoni.RequestModifier] setting a request header using byte slices for zero-allocation setup.
func WithHeaderBytes(key, value []byte) aoni.RequestModifier {
	return func(req aoni.Request) {
		req.SetHeader(bytesconv.B2S(key), bytesconv.B2S(value))
	}
}

// WithHeaderFunc constructs an [aoni.RequestModifier] evaluating provider dynamically at request execution time to set key header.
func WithHeaderFunc(key string, provider func() string) aoni.RequestModifier {
	return func(req aoni.Request) {
		if provider != nil && key != "" {
			if val := provider(); val != "" {
				req.SetHeader(key, val)
			}
		}
	}
}

// WithDynamicHeader is an alias for [WithHeaderFunc].
func WithDynamicHeader(key string, provider func() string) aoni.RequestModifier {
	return WithHeaderFunc(key, provider)
}

// WithHeaders constructs an [aoni.RequestModifier] bulk-setting multiple HTTP request headers from a map.
func WithHeaders(headers map[string]string) aoni.RequestModifier {
	return func(req aoni.Request) {
		for k, v := range headers {
			req.SetHeader(k, v)
		}
	}
}

// ResetHeaders constructs an [aoni.RequestModifier] clearing all headers from the request.
func ResetHeaders() aoni.RequestModifier {
	return func(req aoni.Request) {
		req.ResetHeaders()
	}
}

// WithBearer constructs an [aoni.RequestModifier] setting an "Authorization: Bearer <token>" header.
func WithBearer(token string) aoni.RequestModifier {
	return func(req aoni.Request) {
		req.SetHeader("Authorization", "Bearer "+token)
	}
}

// WithBearerAuth is an alias for [WithBearer].
func WithBearerAuth(token string) aoni.RequestModifier {
	return WithBearer(token)
}

// WithBasicAuth constructs an [aoni.RequestModifier] setting HTTP Basic Authentication credentials.
func WithBasicAuth(username, password string) aoni.RequestModifier {
	return func(req aoni.Request) {
		auth := username + ":" + password
		req.SetHeader("Authorization", "Basic "+base64.StdEncoding.EncodeToString(bytesconv.S2B(auth)))
	}
}

// WithUserAgent constructs an [aoni.RequestModifier] overriding the standard User-Agent header.
func WithUserAgent(ua string) aoni.RequestModifier {
	return func(req aoni.Request) {
		req.SetHeader("User-Agent", ua)
	}
}

// WithContentType constructs an [aoni.RequestModifier] overriding the standard Content-Type header.
func WithContentType(ct string) aoni.RequestModifier {
	return func(req aoni.Request) {
		req.SetHeader("Content-Type", ct)
	}
}

// WithAccept constructs an [aoni.RequestModifier] overriding the standard Accept header.
func WithAccept(accept string) aoni.RequestModifier {
	return func(req aoni.Request) {
		req.SetHeader("Accept", accept)
	}
}

// WithOrigin constructs an [aoni.RequestModifier] overriding the standard Origin header.
func WithOrigin(origin string) aoni.RequestModifier {
	return func(req aoni.Request) {
		req.SetHeader("Origin", origin)
	}
}

// WithIfNoneMatch constructs an [aoni.RequestModifier] setting the If-None-Match conditional header.
func WithIfNoneMatch(etag string) aoni.RequestModifier {
	return func(req aoni.Request) {
		req.SetHeader("If-None-Match", etag)
	}
}

// WithIfMatch constructs an [aoni.RequestModifier] setting the If-Match conditional header.
func WithIfMatch(etag string) aoni.RequestModifier {
	return func(req aoni.Request) {
		req.SetHeader("If-Match", etag)
	}
}

// WithIfModifiedSince constructs an [aoni.RequestModifier] setting the If-Modified-Since conditional header in HTTP-time format.
func WithIfModifiedSince(t time.Time) aoni.RequestModifier {
	return func(req aoni.Request) {
		req.SetHeader("If-Modified-Since", t.UTC().Format(http.TimeFormat))
	}
}

// ============================================================================
// COOKIE MODIFIERS
// ============================================================================

// WithCookie constructs an [aoni.RequestModifier] attaching a single [*http.Cookie] to the request.
func WithCookie(c *http.Cookie) aoni.RequestModifier {
	return func(req aoni.Request) {
		if c != nil {
			req.AddHeader("Cookie", c.String())
		}
	}
}

// WithCookies constructs an [aoni.RequestModifier] attaching multiple cookie key-value pairs to the request.
func WithCookies(kv map[string]string) aoni.RequestModifier {
	return func(req aoni.Request) {
		for k, v := range kv {
			req.AddHeader("Cookie", k+"="+v)
		}
	}
}

// WithPartitionKey constructs an [aoni.RequestModifier] attaching a CHIPS (RFC 6265bis) partition key for iFrame/widget context.
func WithPartitionKey(key string) aoni.RequestModifier {
	return func(req aoni.Request) {
		req.SetContext(cookie.WithPartitionKey(req.Context(), key))
	}
}
