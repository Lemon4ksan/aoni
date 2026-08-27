// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lemon4ksan/foundation/net/http/header"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"github.com/lemon4ksan/foundation/timekit"

	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/internal/requestutil"
	"github.com/lemon4ksan/aoni/netutil/pkce"
	"github.com/lemon4ksan/aoni/netutil/priority"
	"github.com/lemon4ksan/aoni/netutil/privacypass"
	"github.com/lemon4ksan/aoni/netutil/secret"
)

// WithPriority configures RFC 9218 extensible request priority headers and HTTP/2-3 stream scheduling.
//
// Urgency defines the relative processing precedence (0 = highest/critical, 7 = lowest/background).
// When incremental is true, the server is instructed to stream partial chunks of the response
// concurrently rather than buffering the entire payload before delivery.
//
// # Wire Representation
//
//	Priority: u=1, i
//
// # Example
//
//	resp, err := client.Get(ctx, "/hero-banner.webp",
//	    mod.WithPriority(1, true),
//	)
//
// # Invariants & Allocations
//
//   - Zero-Allocation: Operates on pre-allocated static strings for standard urgencies.
//   - Clamping: Values outside [0..7] are clamped automatically without returning an error.
//
// # RFC Compliance
//
// Conforms to RFC 9218 (Extensible Prioritization Scheme for HTTP), Section 3.1.
func WithPriority(urgency int, incremental bool) RequestModifier {
	return WithHeader(priority.HeaderPriority, priority.New(urgency, incremental).Format())
}

// WithPriorityPreset configures RFC 9218 request priority using a structured [priority.Priority] preset.
func WithPriorityPreset(p priority.Priority) RequestModifier {
	return WithHeader(priority.HeaderPriority, p.Format())
}

// WithGRPCWebTimeout sets standard gRPC-Web timeout headers ("grpc-timeout").
//
// # Wire Representation
//
//	grpc-timeout: 5000m
func WithGRPCWebTimeout(d time.Duration) RequestModifier {
	return WithHeader(header.GRPCTimeout, formatGRPCTimeout(d))
}

// WithGRPCMetadata injects custom gRPC-Web binary and text metadata headers.
func WithGRPCMetadata(md map[string]string) RequestModifier {
	return WithHeaders(md)
}

// formatGRPCTimeout formats d into a gRPC-compliant timeout header.
func formatGRPCTimeout(d time.Duration) string {
	return requestutil.FormatGRPCTimeout(d)
}

// WithHeader sets or overwrites a single HTTP header key-value pair on the request.
//
// # Example
//
//	resp, err := client.Get(ctx, "/api/data",
//	    mod.WithHeader("X-Request-ID", "req_9f81a"),
//	)
func WithHeader(key, value string) RequestModifier {
	return RequestModifier{
		Kind:  core.ModHeader,
		Key:   key,
		Value: value,
	}
}

// WithHeaderBytes sets a request header using byte slices along the zero-allocation fast path.
func WithHeaderBytes(key, value []byte) RequestModifier {
	return RequestModifier{
		Kind:  core.ModHeader,
		Key:   bytesconv.B2S(key),
		Value: bytesconv.B2S(value),
	}
}

// WithHeaderFunc dynamically evaluates a provider function at request dispatch time to populate key.
//
// # Example
//
//	resp, err := client.Get(ctx, "/status",
//	    mod.WithHeaderFunc("X-Trace-Time", func() string {
//	        return time.Now().UTC().Format(time.RFC3339Nano)
//	    }),
//	)
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

// WithHeaders bulk-sets multiple HTTP headers from a key-value map.
//
// # Example
//
//	resp, err := client.Get(ctx, "/v1/items",
//	    mod.WithHeaders(map[string]string{
//	        "Accept": "application/json",
//	        "X-Tenant-ID": "acme_corp",
//	    }),
//	)
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

// ResetHeaders clears all previously configured headers from the request.
func ResetHeaders() RequestModifier {
	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			req.ResetHeaders()
		},
	}
}

// WithBearer injects an OAuth 2.0 Bearer authentication token (RFC 6750 §2.1).
//
// # Wire Representation
//
//	Authorization: Bearer <token>
//
// # Example
//
//	resp, err := client.Get(ctx, "/user/profile",
//	    mod.WithBearer("eyJh...token"),
//	)
//
// # RFC Compliance
//
// Conforms to RFC 6750 (OAuth 2.0 Bearer Token Usage).
func WithBearer(token string) RequestModifier {
	return RequestModifier{
		Kind:  core.ModBearer,
		Value: token,
	}
}

// WithSecretBearer injects a Bearer token stored in a protected [secret.Secret] memory container.
func WithSecretBearer(token secret.Secret[string]) RequestModifier {
	return WithBearer(token.Value())
}

// WithBasicAuth injects HTTP Basic Authentication credentials (RFC 7617).
//
// Automatically encodes username and password into base64 format.
//
// # Wire Representation
//
//	Authorization: Basic dXNlcm5hbWU6cGFzc3dvcmQ=
//
// # Example
//
//	resp, err := client.Get(ctx, "/admin",
//	    mod.WithBasicAuth("admin", "secret123"),
//	)
//
// # RFC Compliance
//
// Conforms to RFC 7617 (The 'Basic' HTTP Authentication Scheme).
func WithBasicAuth(username, password string) RequestModifier {
	return RequestModifier{
		Kind:  core.ModBasicAuth,
		Key:   username,
		Value: password,
	}
}

// WithSecretBasicAuth injects Basic Authentication credentials with password protected by [secret.Secret].
func WithSecretBasicAuth(username string, password secret.Secret[string]) RequestModifier {
	return WithBasicAuth(username, password.Value())
}

// WithPKCE adds OAuth 2.0 PKCE code_challenge and code_challenge_method query parameters (RFC 7636).
//
// If method is omitted or empty, SHA-256 ("S256") is used by default.
//
// # RFC Compliance
//
// Conforms to RFC 7636 (Proof Key for Code Exchange by OAuth Public Clients).
func WithPKCE(verifier string, method ...string) RequestModifier {
	m := pkce.MethodS256
	if len(method) > 0 && method[0] != "" {
		m = method[0]
	}

	challenge, err := pkce.ComputeChallenge(verifier, m)
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

// WithPKCEVerifier adds the OAuth 2.0 code_verifier parameter for token endpoint requests (RFC 7636 §4.5).
func WithPKCEVerifier(verifier string) RequestModifier {
	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			req.AddQueryParam("code_verifier", verifier)
		},
	}
}

// WithUserAgent overrides the standard User-Agent header field (RFC 9110 §10.1.5).
//
// # Example
//
//	resp, err := client.Get(ctx, "/feed",
//	    mod.WithUserAgent("FeedFetcher/2.0"),
//	)
func WithUserAgent(ua string) RequestModifier {
	return WithHeader(header.UserAgent, ua)
}

// WithContentType sets the Content-Type entity header (RFC 9110 §8.3).
//
// # Example
//
//	resp, err := client.Post(ctx, "/graphql",
//	    mod.WithContentType("application/graphql"),
//	)
func WithContentType(ct string) RequestModifier {
	return WithHeader(header.ContentType, ct)
}

// WithAccept sets the Accept content negotiation header (RFC 9110 §12.5.1).
//
// # Example
//
//	resp, err := client.Get(ctx, "/data",
//	    mod.WithAccept("application/vnd.api+json"),
//	)
func WithAccept(accept string) RequestModifier {
	return WithHeader(header.Accept, accept)
}

// WithOrigin sets the Origin CORS header (RFC 6454 §7).
func WithOrigin(origin string) RequestModifier {
	return WithHeader(header.Origin, origin)
}

// WithIfNoneMatch sets the If-None-Match conditional validator header (RFC 9110 §13.1.2) for ETag caching.
func WithIfNoneMatch(etag string) RequestModifier {
	return WithHeader(header.IfNoneMatch, etag)
}

// WithIfMatch sets the If-Match conditional header (RFC 9110 §13.1.1) to prevent mid-air collision updates.
func WithIfMatch(etag string) RequestModifier {
	return WithHeader(header.IfMatch, etag)
}

// WithIfModifiedSince sets the If-Modified-Since conditional validator header (RFC 9110 §13.1.3).
func WithIfModifiedSince(t time.Time) RequestModifier {
	return WithHeader(header.IfModifiedSince, timekit.FormatHTTPDate(t))
}

// WithIfUnmodifiedSince sets the If-Unmodified-Since conditional header (RFC 9110 §13.1.4).
func WithIfUnmodifiedSince(t time.Time) RequestModifier {
	return WithHeader(header.IfUnmodifiedSince, timekit.FormatHTTPDate(t))
}

// WithRange sets the Range header for partial byte-range requests (RFC 9110 §14.2).
//
// If start < 0, requests the suffix range (e.g. "bytes=-500").
// If end < 0, requests from start to EOF (e.g. "bytes=1000-").
//
// # Example
//
//	resp, err := client.Get(ctx, "/large-file.iso",
//	    mod.WithRange(0, 1048575), // First 1 MB
//	)
func WithRange(start, end int64) RequestModifier {
	if start < 0 {
		return WithHeader(header.Range, header.ValueBytes+"="+strconv.FormatInt(start, 10))
	}

	if end < 0 {
		return WithHeader(header.Range, header.ValueBytes+"="+strconv.FormatInt(start, 10)+"-")
	}

	return WithHeader(header.Range, header.ValueBytes+"="+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10))
}

// WithIfRangeETag sets the If-Range conditional header with an entity tag validator (RFC 9110 §13.1.5).
func WithIfRangeETag(etag string) RequestModifier {
	return WithHeader(header.IfRange, etag)
}

// WithIfRangeDate sets the If-Range conditional header with an HTTP timestamp date (RFC 9110 §13.1.5).
func WithIfRangeDate(t time.Time) RequestModifier {
	return WithHeader(header.IfRange, timekit.FormatHTTPDate(t))
}

// WithCacheControl sets Cache-Control request directives (RFC 9111 §5.2.1).
//
// # Example
//
//	resp, err := client.Get(ctx, "/resource",
//	    mod.WithCacheControl("no-cache", "max-age=0"),
//	)
func WithCacheControl(directives ...string) RequestModifier {
	return WithHeader(header.CacheControl, strings.Join(directives, ", "))
}

// WithNoCache forces intermediate proxies and origin servers to revalidate cached content via "Cache-Control: no-cache".
func WithNoCache() RequestModifier {
	return WithHeader(header.CacheControl, header.ValueNoCache)
}

// WithNoStore directs caches never to persist request or response data via "Cache-Control: no-store".
func WithNoStore() RequestModifier {
	return WithHeader(header.CacheControl, header.ValueNoStore)
}

// ============================================================================
// WEBSOCKET MODIFIERS (RFC 6455, RFC 7692, RFC 7936, RFC 8441)
// ============================================================================

// WithSecWebSocketProtocol requests one or more WebSocket subprotocols during upgrade (RFC 6455 §11.3.4).
func WithSecWebSocketProtocol(protocols ...string) RequestModifier {
	return WithHeader(header.SecWebSocketProtocol, strings.Join(protocols, ", "))
}

// WithSecWebSocketExtensions requests WebSocket framing extensions (RFC 6455 §11.3.2).
func WithSecWebSocketExtensions(extensions ...string) RequestModifier {
	return WithHeader(header.SecWebSocketExtensions, strings.Join(extensions, ", "))
}

// WithSecWebSocketVersion sets the Sec-WebSocket-Version header (RFC 6455 §11.3.5).
func WithSecWebSocketVersion(version string) RequestModifier {
	return WithHeader(header.SecWebSocketVersion, version)
}

// WithPermessageDeflate requests the permessage-deflate WebSocket compression extension (RFC 7692 §7).
func WithPermessageDeflate(params ...string) RequestModifier {
	if len(params) == 0 {
		return WithHeader(header.SecWebSocketExtensions, "permessage-deflate; client_max_window_bits")
	}

	return WithHeader(header.SecWebSocketExtensions, "permessage-deflate; "+strings.Join(params, "; "))
}

// ============================================================================
// COOKIE MODIFIERS
// ============================================================================

// WithCookie attaches a single [*http.Cookie] to the request's "Cookie" header.
//
// # Example
//
//	resp, err := client.Get(ctx, "/dashboard",
//	    mod.WithCookie(&http.Cookie{Name: "session", Value: "xyz123"}),
//	)
func WithCookie(c *http.Cookie) RequestModifier {
	if c == nil {
		return RequestModifier{}
	}

	return RequestModifier{
		Kind:  core.ModHeaderAdd,
		Key:   header.Cookie,
		Value: c.String(),
	}
}

// WithCookies attaches multiple cookie key-value pairs to the request.
func WithCookies(kv map[string]string) RequestModifier {
	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			for k, v := range kv {
				req.AddHeader(header.Cookie, k+"="+v)
			}
		},
	}
}

// WithPartitionKey sets a CHIPS (RFC 6265bis) top-level site partition key on the request context.
//
// Conforms to RFC 6265bis CHIPS (Cookies Having Independent Partitioned State).
func WithPartitionKey(key string) RequestModifier {
	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			req.SetContext(cookie.WithPartitionKey(req.Context(), key))
		},
	}
}

// WithHeaderIf conditionally attaches a header only when condition evaluates to true.
func WithHeaderIf(condition bool, key, value string) RequestModifier {
	if !condition {
		return RequestModifier{}
	}

	return WithHeader(key, value)
}

// WithHeadersIf conditionally attaches a map of headers only when condition evaluates to true.
func WithHeadersIf(condition bool, headers map[string]string) RequestModifier {
	if !condition || len(headers) == 0 {
		return RequestModifier{}
	}

	return WithHeaders(headers)
}

// WithPrivateToken injects an RFC 9577 Privacy Pass authentication redemption header ("Authorization: PrivateToken token=...").
//
// # RFC Compliance
//
// Conforms to RFC 9577 (Privacy Pass HTTP Authentication Scheme).
func WithPrivateToken(token string) RequestModifier {
	if !strings.HasPrefix(token, privacypass.SchemePrivateToken) {
		token = privacypass.SchemePrivateToken + " token=\"" + token + "\""
	}

	return WithHeader(privacypass.HeaderAuthorization, token)
}

// WithPrivateStateToken injects a W3C Private State Token header ("Sec-Private-State-Token").
func WithPrivateStateToken(token string) RequestModifier {
	return WithHeader(privacypass.HeaderSecPrivateStateToken, token)
}

// WithWebPushTTL sets the RFC 8030 WebPush message time-to-live retention duration header.
func WithWebPushTTL(d time.Duration) RequestModifier {
	seconds := int64(d.Seconds())
	if seconds < 0 {
		seconds = 0
	}

	return WithHeader("TTL", strconv.FormatInt(seconds, 10))
}

// WithWebPushUrgency sets the RFC 8030 WebPush message Urgency header ("very-low", "low", "normal", "high").
func WithWebPushUrgency(urgency string) RequestModifier {
	return WithHeader("Urgency", urgency)
}

// WithWebPushTopic sets the RFC 8030 WebPush correlation Topic header for message replacement.
func WithWebPushTopic(topic string) RequestModifier {
	return WithHeader("Topic", topic)
}

// WithVAPIDAuth injects an RFC 8292 Voluntary Application Server Identification (VAPID) Authorization header.
func WithVAPIDAuth(vapidAuth string) RequestModifier {
	return WithHeader(header.Authorization, vapidAuth)
}
