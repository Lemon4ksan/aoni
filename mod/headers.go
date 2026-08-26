// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	fheader "github.com/lemon4ksan/foundation/net/http/header"
	fpkce "github.com/lemon4ksan/foundation/net/pkce"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"github.com/lemon4ksan/foundation/timekit"

	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/internal/requestutil"
	"github.com/lemon4ksan/aoni/netutil/priority"
	"github.com/lemon4ksan/aoni/netutil/privacypass"
	"github.com/lemon4ksan/aoni/netutil/secret"
)

// WithPriority constructs an [RequestModifier] setting the RFC 9218 "Priority" header (e.g. "u=1, i" or "u=0").
func WithPriority(urgency int, incremental bool) RequestModifier {
	return WithHeader(priority.HeaderPriority, priority.New(urgency, incremental).Format())
}

// WithPriorityPreset constructs an [RequestModifier] setting the RFC 9218 "Priority" header using a predefined [priority.Priority] preset.
func WithPriorityPreset(p priority.Priority) RequestModifier {
	return WithHeader(priority.HeaderPriority, p.Format())
}

// WithGRPCWebTimeout constructs an [RequestModifier] setting standard gRPC-Web timeout headers ("grpc-timeout").
func WithGRPCWebTimeout(d time.Duration) RequestModifier {
	return WithHeader(fheader.GRPCTimeout, formatGRPCTimeout(d))
}

// WithGRPCMetadata constructs an [RequestModifier] injecting gRPC-Web binary metadata headers.
func WithGRPCMetadata(md map[string]string) RequestModifier {
	return WithHeaders(md)
}

// formatGRPCTimeout formats d into a gRPC-compliant timeout header.
func formatGRPCTimeout(d time.Duration) string {
	return requestutil.FormatGRPCTimeout(d)
}

// WithHeader constructs an [RequestModifier] setting a single request header key to value.
func WithHeader(key, value string) RequestModifier {
	return RequestModifier{
		Kind:  core.ModHeader,
		Key:   key,
		Value: value,
	}
}

// WithHeaderBytes constructs an [RequestModifier] setting a request header using byte slices for zero-allocation setup.
func WithHeaderBytes(key, value []byte) RequestModifier {
	return RequestModifier{
		Kind:  core.ModHeader,
		Key:   bytesconv.B2S(key),
		Value: bytesconv.B2S(value),
	}
}

// WithHeaderFunc constructs an [RequestModifier] evaluating provider dynamically at request execution time to set key header.
func WithHeaderFunc(key string, provider func() string) RequestModifier {
	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			if provider != nil && key != "" {
				if val := provider(); val != "" {
					req.SetHeader(key, val)
				}
			}
		},
	}
}

// WithDynamicHeader is an alias for [WithHeaderFunc].
func WithDynamicHeader(key string, provider func() string) RequestModifier {
	return WithHeaderFunc(key, provider)
}

// WithHeaders constructs an [RequestModifier] bulk-setting multiple HTTP request headers from a map.
func WithHeaders(headers map[string]string) RequestModifier {
	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			for k, v := range headers {
				req.SetHeader(k, v)
			}
		},
	}
}

// ResetHeaders constructs an [RequestModifier] clearing all headers from the request.
func ResetHeaders() RequestModifier {
	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			req.ResetHeaders()
		},
	}
}

// WithBearer constructs an [RequestModifier] setting an "Authorization: Bearer <token>" header
// per RFC 6750 §2.1 (The OAuth 2.0 Authorization Framework: Bearer Token Usage).
func WithBearer(token string) RequestModifier {
	return RequestModifier{
		Kind:  core.ModBearer,
		Value: token,
	}
}

// WithSecretBearer constructs an [RequestModifier] setting a Bearer token from a protected [secret.Secret].
func WithSecretBearer(token secret.Secret[string]) RequestModifier {
	return WithBearer(token.Value())
}

// WithBasicAuth constructs an [RequestModifier] setting HTTP Basic Authentication credentials
// per RFC 7617 (The 'Basic' HTTP Authentication Scheme).
func WithBasicAuth(username, password string) RequestModifier {
	return RequestModifier{
		Kind:  core.ModBasicAuth,
		Key:   username,
		Value: password,
	}
}

// WithSecretBasicAuth constructs an [RequestModifier] setting Basic Authentication credentials with a protected password [secret.Secret].
func WithSecretBasicAuth(username string, password secret.Secret[string]) RequestModifier {
	return WithBasicAuth(username, password.Value())
}

// WithPKCE constructs an [RequestModifier] adding PKCE code_challenge and code_challenge_method
// parameters for OAuth 2.0 authorization requests per RFC 7636 §4.3 and RFC 9700 §2.1.
// If method is omitted or empty, S256 is used by default.
func WithPKCE(verifier string, method ...string) RequestModifier {
	m := fpkce.MethodS256
	if len(method) > 0 && method[0] != "" {
		m = method[0]
	}

	challenge, err := fpkce.ComputeChallenge(verifier, m)
	if err != nil {
		challenge = verifier
	}

	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			req.AddQueryParam("code_challenge", challenge)
			req.AddQueryParam("code_challenge_method", m)
		},
	}
}

// WithPKCEVerifier constructs an [RequestModifier] adding the code_verifier parameter
// for OAuth 2.0 token endpoint requests per RFC 7636 §4.5 and RFC 9700 §2.1.
func WithPKCEVerifier(verifier string) RequestModifier {
	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			req.AddQueryParam("code_verifier", verifier)
		},
	}
}

// WithUserAgent constructs an [RequestModifier] overriding the standard User-Agent header (RFC 9110 §10.1.5).
func WithUserAgent(ua string) RequestModifier {
	return WithHeader(fheader.UserAgent, ua)
}

// WithContentType constructs an [RequestModifier] overriding the standard Content-Type header (RFC 9110 §8.3).
func WithContentType(ct string) RequestModifier {
	return WithHeader(fheader.ContentType, ct)
}

// WithAccept constructs an [RequestModifier] overriding the standard Accept header (RFC 9110 §12.5.1).
func WithAccept(accept string) RequestModifier {
	return WithHeader(fheader.Accept, accept)
}

// WithOrigin constructs an [RequestModifier] overriding the standard Origin header (RFC 6454 §7).
func WithOrigin(origin string) RequestModifier {
	return WithHeader(fheader.Origin, origin)
}

// WithIfNoneMatch constructs an [RequestModifier] setting the If-None-Match conditional header (RFC 9110 §13.1.2).
func WithIfNoneMatch(etag string) RequestModifier {
	return WithHeader(fheader.IfNoneMatch, etag)
}

// WithIfMatch constructs an [RequestModifier] setting the If-Match conditional header (RFC 9110 §13.1.1).
func WithIfMatch(etag string) RequestModifier {
	return WithHeader(fheader.IfMatch, etag)
}

// WithIfModifiedSince constructs an [RequestModifier] setting the If-Modified-Since conditional header (RFC 9110 §5.6.7 & §13.1.3).
func WithIfModifiedSince(t time.Time) RequestModifier {
	return WithHeader(fheader.IfModifiedSince, timekit.FormatHTTPDate(t))
}

// WithIfUnmodifiedSince constructs an [RequestModifier] setting the If-Unmodified-Since conditional header (RFC 9110 §5.6.7 & §13.1.4).
func WithIfUnmodifiedSince(t time.Time) RequestModifier {
	return WithHeader(fheader.IfUnmodifiedSince, timekit.FormatHTTPDate(t))
}

// WithRange constructs an [RequestModifier] setting the Range header for byte-range requests (RFC 9110 §14.2).
func WithRange(start, end int64) RequestModifier {
	if start < 0 {
		return WithHeader(fheader.Range, fheader.ValueBytes+"="+strconv.FormatInt(start, 10))
	}

	if end < 0 {
		return WithHeader(fheader.Range, fheader.ValueBytes+"="+strconv.FormatInt(start, 10)+"-")
	}

	return WithHeader(fheader.Range, fheader.ValueBytes+"="+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10))
}

// WithIfRangeETag constructs an [RequestModifier] setting the If-Range header with an entity tag (RFC 9110 §13.1.5).
func WithIfRangeETag(etag string) RequestModifier {
	return WithHeader(fheader.IfRange, etag)
}

// WithIfRangeDate constructs an [RequestModifier] setting the If-Range header with an HTTP date (RFC 9110 §5.6.7 & §13.1.5).
func WithIfRangeDate(t time.Time) RequestModifier {
	return WithHeader(fheader.IfRange, timekit.FormatHTTPDate(t))
}

// WithCacheControl constructs an [RequestModifier] setting Cache-Control request directives (RFC 9111 §5.2.1).
func WithCacheControl(directives ...string) RequestModifier {
	return WithHeader(fheader.CacheControl, strings.Join(directives, ", "))
}

// WithNoCache constructs an [RequestModifier] forcing cache revalidation via "Cache-Control: no-cache" (RFC 9111 §5.2.1.4).
func WithNoCache() RequestModifier {
	return WithHeader(fheader.CacheControl, fheader.ValueNoCache)
}

// WithNoStore constructs an [RequestModifier] preventing response caching via "Cache-Control: no-store" (RFC 9111 §5.2.1.5).
func WithNoStore() RequestModifier {
	return WithHeader(fheader.CacheControl, fheader.ValueNoStore)
}

// ============================================================================
// WEBSOCKET MODIFIERS (RFC 6455, RFC 7692, RFC 7936, RFC 8441)
// ============================================================================

// WithSecWebSocketProtocol constructs an [RequestModifier] requesting one or more WebSocket subprotocols
// per RFC 6455 §11.3.4, RFC 7936 §2, and RFC 8441 §5.
func WithSecWebSocketProtocol(protocols ...string) RequestModifier {
	return WithHeader(fheader.SecWebSocketProtocol, strings.Join(protocols, ", "))
}

// WithSecWebSocketExtensions constructs an [RequestModifier] requesting WebSocket extensions
// per RFC 6455 §11.3.2, RFC 7692 §5, and RFC 8441 §5.
func WithSecWebSocketExtensions(extensions ...string) RequestModifier {
	return WithHeader(fheader.SecWebSocketExtensions, strings.Join(extensions, ", "))
}

// WithSecWebSocketVersion constructs an [RequestModifier] setting the Sec-WebSocket-Version header (RFC 6455 §11.3.5 & RFC 8441 §5).
func WithSecWebSocketVersion(version string) RequestModifier {
	return WithHeader(fheader.SecWebSocketVersion, version)
}

// WithPermessageDeflate constructs an [RequestModifier] requesting the permessage-deflate compression extension
// with optional parameters (RFC 7692 §7 & RFC 8441 §5).
func WithPermessageDeflate(params ...string) RequestModifier {
	if len(params) == 0 {
		return WithHeader(fheader.SecWebSocketExtensions, "permessage-deflate; client_max_window_bits")
	}

	return WithHeader(fheader.SecWebSocketExtensions, "permessage-deflate; "+strings.Join(params, "; "))
}

// ============================================================================
// COOKIE MODIFIERS
// ============================================================================

// WithCookie constructs an [RequestModifier] attaching a single [*http.Cookie] to the request.
func WithCookie(c *http.Cookie) RequestModifier {
	if c == nil {
		return RequestModifier{}
	}

	return RequestModifier{
		Kind:  core.ModHeaderAdd,
		Key:   fheader.Cookie,
		Value: c.String(),
	}
}

// WithCookies constructs an [RequestModifier] attaching multiple cookie key-value pairs to the request.
func WithCookies(kv map[string]string) RequestModifier {
	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			for k, v := range kv {
				req.AddHeader(fheader.Cookie, k+"="+v)
			}
		},
	}
}

// WithPartitionKey constructs an [RequestModifier] attaching a CHIPS (RFC 6265bis) partition key for iFrame/widget context.
func WithPartitionKey(key string) RequestModifier {
	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			req.SetContext(cookie.WithPartitionKey(req.Context(), key))
		},
	}
}

// WithHeaderIf conditionally constructs an [RequestModifier] if condition is true.
func WithHeaderIf(condition bool, key, value string) RequestModifier {
	if !condition {
		return RequestModifier{}
	}

	return WithHeader(key, value)
}

// WithHeadersIf conditionally constructs an [RequestModifier] if condition is true.
func WithHeadersIf(condition bool, headers map[string]string) RequestModifier {
	if !condition || len(headers) == 0 {
		return RequestModifier{}
	}

	return WithHeaders(headers)
}

// WithPrivateToken constructs an [RequestModifier] injecting an RFC 9577 Privacy Pass redemption header ("Authorization: PrivateToken token=...").
func WithPrivateToken(token string) RequestModifier {
	if !strings.HasPrefix(token, privacypass.SchemePrivateToken) {
		token = privacypass.SchemePrivateToken + " token=\"" + token + "\""
	}

	return WithHeader(privacypass.HeaderAuthorization, token)
}

// WithPrivateStateToken constructs an [RequestModifier] injecting a W3C Private State Token ("Sec-Private-State-Token").
func WithPrivateStateToken(token string) RequestModifier {
	return WithHeader(privacypass.HeaderSecPrivateStateToken, token)
}

// WithWebPushTTL constructs an [RequestModifier] setting RFC 8030 WebPush TTL retention header in seconds.
func WithWebPushTTL(d time.Duration) RequestModifier {
	seconds := int64(d.Seconds())
	if seconds < 0 {
		seconds = 0
	}

	return WithHeader("TTL", strconv.FormatInt(seconds, 10))
}

// WithWebPushUrgency constructs an [RequestModifier] setting RFC 8030 WebPush Urgency header.
func WithWebPushUrgency(urgency string) RequestModifier {
	return WithHeader("Urgency", urgency)
}

// WithWebPushTopic constructs an [RequestModifier] setting RFC 8030 WebPush correlation Topic header.
func WithWebPushTopic(topic string) RequestModifier {
	return WithHeader("Topic", topic)
}

// WithVAPIDAuth constructs an [RequestModifier] injecting an RFC 8292 VAPID Authorization header ("Authorization: vapid t=..., k=...").
func WithVAPIDAuth(vapidAuth string) RequestModifier {
	return WithHeader(fheader.Authorization, vapidAuth)
}
