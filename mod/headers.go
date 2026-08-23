// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	fpkce "github.com/lemon4ksan/foundation/net/pkce"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/internal/requestutil"
	"github.com/lemon4ksan/aoni/netutil/hpkp"
)

// WithGRPCWebTimeout constructs an [aoni.RequestModifier] setting standard gRPC-Web timeout headers ("grpc-timeout").
func WithGRPCWebTimeout(d time.Duration) aoni.RequestModifier {
	return WithHeader("grpc-timeout", formatGRPCTimeout(d))
}

// WithGRPCMetadata constructs an [aoni.RequestModifier] injecting gRPC-Web binary metadata headers.
func WithGRPCMetadata(md map[string]string) aoni.RequestModifier {
	return WithHeaders(md)
}

// formatGRPCTimeout formats d into a gRPC-compliant timeout header.
func formatGRPCTimeout(d time.Duration) string {
	return requestutil.FormatGRPCTimeout(d)
}

// WithHeader constructs an [aoni.RequestModifier] setting a single request header key to value.
func WithHeader(key, value string) aoni.RequestModifier {
	return aoni.RequestModifier{
		Kind:  core.ModHeader,
		Key:   key,
		Value: value,
	}
}

// WithHeaderBytes constructs an [aoni.RequestModifier] setting a request header using byte slices for zero-allocation setup.
func WithHeaderBytes(key, value []byte) aoni.RequestModifier {
	return aoni.RequestModifier{
		Kind:  core.ModHeader,
		Key:   bytesconv.B2S(key),
		Value: bytesconv.B2S(value),
	}
}

// WithHeaderFunc constructs an [aoni.RequestModifier] evaluating provider dynamically at request execution time to set key header.
func WithHeaderFunc(key string, provider func() string) aoni.RequestModifier {
	return aoni.RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req aoni.Request) {
			if provider != nil && key != "" {
				if val := provider(); val != "" {
					req.SetHeader(key, val)
				}
			}
		},
	}
}

// WithDynamicHeader is an alias for [WithHeaderFunc].
func WithDynamicHeader(key string, provider func() string) aoni.RequestModifier {
	return WithHeaderFunc(key, provider)
}

// WithHeaders constructs an [aoni.RequestModifier] bulk-setting multiple HTTP request headers from a map.
func WithHeaders(headers map[string]string) aoni.RequestModifier {
	return aoni.RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req aoni.Request) {
			for k, v := range headers {
				req.SetHeader(k, v)
			}
		},
	}
}

// ResetHeaders constructs an [aoni.RequestModifier] clearing all headers from the request.
func ResetHeaders() aoni.RequestModifier {
	return aoni.RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req aoni.Request) {
			req.ResetHeaders()
		},
	}
}

// WithBearer constructs an [aoni.RequestModifier] setting an "Authorization: Bearer <token>" header
// per RFC 6750 §2.1 (The OAuth 2.0 Authorization Framework: Bearer Token Usage).
func WithBearer(token string) aoni.RequestModifier {
	return aoni.RequestModifier{
		Kind:  core.ModBearer,
		Value: token,
	}
}

// WithBasicAuth constructs an [aoni.RequestModifier] setting HTTP Basic Authentication credentials
// per RFC 7617 (The 'Basic' HTTP Authentication Scheme).
func WithBasicAuth(username, password string) aoni.RequestModifier {
	return aoni.RequestModifier{
		Kind:  core.ModBasicAuth,
		Key:   username,
		Value: password,
	}
}

// WithPKCE constructs an [aoni.RequestModifier] adding PKCE code_challenge and code_challenge_method
// parameters for OAuth 2.0 authorization requests per RFC 7636 §4.3 and RFC 9700 §2.1.
// If method is omitted or empty, S256 is used by default.
func WithPKCE(verifier string, method ...string) aoni.RequestModifier {
	m := fpkce.MethodS256
	if len(method) > 0 && method[0] != "" {
		m = method[0]
	}

	challenge, err := fpkce.ComputeChallenge(verifier, m)
	if err != nil {
		challenge = verifier
	}

	return aoni.RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req aoni.Request) {
			req.AddQueryParam("code_challenge", challenge)
			req.AddQueryParam("code_challenge_method", m)
		},
	}
}

// WithPKCEVerifier constructs an [aoni.RequestModifier] adding the code_verifier parameter
// for OAuth 2.0 token endpoint requests per RFC 7636 §4.5 and RFC 9700 §2.1.
func WithPKCEVerifier(verifier string) aoni.RequestModifier {
	return aoni.RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req aoni.Request) {
			req.AddQueryParam("code_verifier", verifier)
		},
	}
}

// WithUserAgent constructs an [aoni.RequestModifier] overriding the standard User-Agent header (RFC 9110 §10.1.5).
func WithUserAgent(ua string) aoni.RequestModifier {
	return WithHeader("User-Agent", ua)
}

// WithContentType constructs an [aoni.RequestModifier] overriding the standard Content-Type header (RFC 9110 §8.3).
func WithContentType(ct string) aoni.RequestModifier {
	return WithHeader("Content-Type", ct)
}

// WithAccept constructs an [aoni.RequestModifier] overriding the standard Accept header (RFC 9110 §12.5.1).
func WithAccept(accept string) aoni.RequestModifier {
	return WithHeader("Accept", accept)
}

// WithOrigin constructs an [aoni.RequestModifier] overriding the standard Origin header (RFC 6454 §7).
func WithOrigin(origin string) aoni.RequestModifier {
	return WithHeader("Origin", origin)
}

// WithIfNoneMatch constructs an [aoni.RequestModifier] setting the If-None-Match conditional header (RFC 9110 §13.1.2).
func WithIfNoneMatch(etag string) aoni.RequestModifier {
	return WithHeader("If-None-Match", etag)
}

// WithIfMatch constructs an [aoni.RequestModifier] setting the If-Match conditional header (RFC 9110 §13.1.1).
func WithIfMatch(etag string) aoni.RequestModifier {
	return WithHeader("If-Match", etag)
}

// WithIfModifiedSince constructs an [aoni.RequestModifier] setting the If-Modified-Since conditional header (RFC 9110 §5.6.7 & §13.1.3).
func WithIfModifiedSince(t time.Time) aoni.RequestModifier {
	return WithHeader("If-Modified-Since", t.UTC().Format(http.TimeFormat))
}

// WithIfUnmodifiedSince constructs an [aoni.RequestModifier] setting the If-Unmodified-Since conditional header (RFC 9110 §5.6.7 & §13.1.4).
func WithIfUnmodifiedSince(t time.Time) aoni.RequestModifier {
	return WithHeader("If-Unmodified-Since", t.UTC().Format(http.TimeFormat))
}

// WithRange constructs an [aoni.RequestModifier] setting the Range header for byte-range requests (RFC 9110 §14.2).
func WithRange(start, end int64) aoni.RequestModifier {
	if start < 0 {
		return WithHeader("Range", "bytes="+strconv.FormatInt(start, 10))
	}

	if end < 0 {
		return WithHeader("Range", "bytes="+strconv.FormatInt(start, 10)+"-")
	}

	return WithHeader("Range", "bytes="+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10))
}

// WithIfRangeETag constructs an [aoni.RequestModifier] setting the If-Range header with an entity tag (RFC 9110 §13.1.5).
func WithIfRangeETag(etag string) aoni.RequestModifier {
	return WithHeader("If-Range", etag)
}

// WithIfRangeDate constructs an [aoni.RequestModifier] setting the If-Range header with an HTTP date (RFC 9110 §5.6.7 & §13.1.5).
func WithIfRangeDate(t time.Time) aoni.RequestModifier {
	return WithHeader("If-Range", t.UTC().Format(http.TimeFormat))
}

// WithCacheControl constructs an [aoni.RequestModifier] setting Cache-Control request directives (RFC 9111 §5.2.1).
func WithCacheControl(directives ...string) aoni.RequestModifier {
	return WithHeader("Cache-Control", strings.Join(directives, ", "))
}

// WithNoCache constructs an [aoni.RequestModifier] forcing cache revalidation via "Cache-Control: no-cache" (RFC 9111 §5.2.1.4).
func WithNoCache() aoni.RequestModifier {
	return WithHeader("Cache-Control", "no-cache")
}

// WithNoStore constructs an [aoni.RequestModifier] preventing response caching via "Cache-Control: no-store" (RFC 9111 §5.2.1.5).
func WithNoStore() aoni.RequestModifier {
	return WithHeader("Cache-Control", "no-store")
}

// ============================================================================
// WEBSOCKET MODIFIERS (RFC 6455, RFC 7692, RFC 7936, RFC 8441)
// ============================================================================

// WithSecWebSocketProtocol constructs an [aoni.RequestModifier] requesting one or more WebSocket subprotocols
// per RFC 6455 §11.3.4, RFC 7936 §2, and RFC 8441 §5.
func WithSecWebSocketProtocol(protocols ...string) aoni.RequestModifier {
	return WithHeader("Sec-WebSocket-Protocol", strings.Join(protocols, ", "))
}

// WithSecWebSocketExtensions constructs an [aoni.RequestModifier] requesting WebSocket extensions
// per RFC 6455 §11.3.2, RFC 7692 §5, and RFC 8441 §5.
func WithSecWebSocketExtensions(extensions ...string) aoni.RequestModifier {
	return WithHeader("Sec-WebSocket-Extensions", strings.Join(extensions, ", "))
}

// WithSecWebSocketVersion constructs an [aoni.RequestModifier] setting the Sec-WebSocket-Version header (RFC 6455 §11.3.5 & RFC 8441 §5).
func WithSecWebSocketVersion(version string) aoni.RequestModifier {
	return WithHeader("Sec-WebSocket-Version", version)
}

// WithPermessageDeflate constructs an [aoni.RequestModifier] requesting the permessage-deflate compression extension
// with optional parameters (RFC 7692 §7 & RFC 8441 §5).
func WithPermessageDeflate(params ...string) aoni.RequestModifier {
	if len(params) == 0 {
		return WithHeader("Sec-WebSocket-Extensions", "permessage-deflate; client_max_window_bits")
	}

	return WithHeader("Sec-WebSocket-Extensions", "permessage-deflate; "+strings.Join(params, "; "))
}

// ============================================================================
// PUBLIC KEY PINNING (HPKP) MODIFIERS (RFC 7469)
// ============================================================================

// WithPublicKeyPins constructs an [aoni.RequestModifier] attaching a Public-Key-Pins header value (RFC 7469 §2.1).
func WithPublicKeyPins(value string) aoni.RequestModifier {
	return WithHeader(hpkp.HeaderPublicKeyPins, value)
}

// WithPublicKeyPinsReportOnly constructs an [aoni.RequestModifier] attaching a Public-Key-Pins-Report-Only header value (RFC 7469 §2.1).
func WithPublicKeyPinsReportOnly(value string) aoni.RequestModifier {
	return WithHeader(hpkp.HeaderPublicKeyPinsReportOnly, value)
}

// ============================================================================
// COOKIE MODIFIERS
// ============================================================================

// WithCookie constructs an [aoni.RequestModifier] attaching a single [*http.Cookie] to the request.
func WithCookie(c *http.Cookie) aoni.RequestModifier {
	if c == nil {
		return aoni.RequestModifier{}
	}

	return aoni.RequestModifier{
		Kind:  core.ModHeaderAdd,
		Key:   "Cookie",
		Value: c.String(),
	}
}

// WithCookies constructs an [aoni.RequestModifier] attaching multiple cookie key-value pairs to the request.
func WithCookies(kv map[string]string) aoni.RequestModifier {
	return aoni.RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req aoni.Request) {
			for k, v := range kv {
				req.AddHeader("Cookie", k+"="+v)
			}
		},
	}
}

// WithPartitionKey constructs an [aoni.RequestModifier] attaching a CHIPS (RFC 6265bis) partition key for iFrame/widget context.
func WithPartitionKey(key string) aoni.RequestModifier {
	return aoni.RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req aoni.Request) {
			req.SetContext(cookie.WithPartitionKey(req.Context(), key))
		},
	}
}

// WithHeaderIf conditionally constructs an [aoni.RequestModifier] if condition is true.
func WithHeaderIf(condition bool, key, value string) aoni.RequestModifier {
	if !condition {
		return aoni.RequestModifier{}
	}

	return WithHeader(key, value)
}

// WithHeadersIf conditionally constructs an [aoni.RequestModifier] if condition is true.
func WithHeadersIf(condition bool, headers map[string]string) aoni.RequestModifier {
	if !condition || len(headers) == 0 {
		return aoni.RequestModifier{}
	}

	return WithHeaders(headers)
}
