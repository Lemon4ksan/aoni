// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lemon4ksan/foundation/generic"
	fpkce "github.com/lemon4ksan/foundation/net/pkce"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/internal/pipeline"
)

// DefaultClient is the shared, package-level [Client] instance used for direct single-line calls.
// It is initialized with production-hardened defaults (15s timeout, automatic gzip/brotli/zstd decompression,
// 10-hop redirect bounds, and a 10MB response size guard).
//
// Thread-Safety: Safe for concurrent invocation across arbitrary goroutines.
var DefaultClient = NewClient(nil)

// New instantiates a new [*Client] contract configured with the provided functional options.
// It acts as the canonical entry point for constructing custom-configured client instances.
func New(opts ...ClientOption) *Client {
	return NewClient(nil, opts...)
}

// Get executes an HTTP GET request against path using the shared [DefaultClient].
//
// Invariants:
//   - Relative paths are resolved against DefaultClient's BaseURL.
//   - Caller MUST close resp.Body to prevent TCP socket descriptor leaks.
func Get(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return DefaultClient.Get(ctx, path, mods...)
}

// Post executes an HTTP POST request against path using the shared [DefaultClient].
//
// Invariants:
//   - Caller MUST close resp.Body.
func Post(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return DefaultClient.Post(ctx, path, mods...)
}

// Put executes an HTTP PUT request against path using the shared [DefaultClient].
//
// Invariants:
//   - Caller MUST close resp.Body.
func Put(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return DefaultClient.Put(ctx, path, mods...)
}

// Patch executes an HTTP PATCH request against path using the shared [DefaultClient].
//
// Invariants:
//   - Caller MUST close resp.Body.
func Patch(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return DefaultClient.Patch(ctx, path, mods...)
}

// Delete executes an HTTP DELETE request against path using the shared [DefaultClient].
//
// Invariants:
//   - Caller MUST close resp.Body.
func Delete(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return DefaultClient.Delete(ctx, path, mods...)
}

// Head executes an HTTP HEAD request against path using the shared [DefaultClient] to inspect headers.
//
// Invariants:
//   - Caller MUST close resp.Body.
func Head(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return DefaultClient.Head(ctx, path, mods...)
}

// decodeResponseTo drains, decodes, and releases an HTTP response stream into a newly allocated target of type T.
//
// Resource Management Invariant:
// decodeResponseTo GUARANTEES that resp.Body is fully drained and closed ([DrainAndClose])
// before returning, ensuring zero TCP socket leaks under both success and error conditions.
//
// Error Semantics:
//   - If the HTTP status code is >= 400, returns an [*APIError] containing the status code
//     and a bounded preview snippet (up to 1KB) of the error response body.
//   - If body decoding fails, returns the underlying parser error.
func decodeResponseTo[T any](resp *http.Response) (*T, error) {
	if resp == nil {
		return nil, ErrNilRequest
	}

	defer DrainAndClose(resp)

	if resp.StatusCode >= http.StatusBadRequest {
		bodySnippet, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))

		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Body:       bodySnippet,
		}
	}

	var target T

	contentType := resp.Header.Get("Content-Type")

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := decode.DecodePayload(contentType, bodyBytes, &target); err != nil {
		return nil, err
	}

	return &target, nil
}

func injectBodyMod(body any, mods []RequestModifier) []RequestModifier {
	if body == nil {
		return mods
	}

	bodyMod := WithSmartBody(body)
	if bodyMod.Kind == 0 && bodyMod.Fn == nil {
		return mods
	}

	allMods := make([]RequestModifier, 0, len(mods)+1)
	allMods = append(allMods, bodyMod)
	allMods = append(allMods, mods...)

	return allMods
}

// GetTo executes a GET request using [DefaultClient] and decodes the response payload into a new instance of T.
//
// Resource Management:
// The response body is automatically drained and closed. Callers do NOT need to call resp.Body.Close().
//
// Error Handling:
// Returns an [*APIError] on non-2xx status codes (4xx/5xx). Use [IsNotFound], [IsRateLimited],
// or standard [errors.Is] to inspect the returned error.
func GetTo[T any](ctx context.Context, path string, mods ...RequestModifier) (*T, error) {
	resp, err := DefaultClient.Get(ctx, path, mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	return decodeResponseTo[T](resp)
}

// PostTo executes a POST request with payload using [DefaultClient] and decodes the response payload into T.
//
// Smart Body Handling:
// The body argument is automatically detected and serialized via [WithSmartBody] (struct/map to JSON,
// proto.Message to protobuf, url.Values to form-urlencoded, string/bytes as raw payload).
//
// Resource Management:
// The response body is automatically drained and closed. Callers do NOT need to call resp.Body.Close().
func PostTo[T any](ctx context.Context, path string, body any, mods ...RequestModifier) (*T, error) {
	resp, err := DefaultClient.Post(ctx, path, injectBodyMod(body, mods)...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	return decodeResponseTo[T](resp)
}

// PutTo executes a PUT request with payload using [DefaultClient] and decodes the response payload into T.
//
// Resource Management:
// The response body is automatically drained and closed. Callers do NOT need to call resp.Body.Close().
func PutTo[T any](ctx context.Context, path string, body any, mods ...RequestModifier) (*T, error) {
	resp, err := DefaultClient.Put(ctx, path, injectBodyMod(body, mods)...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	return decodeResponseTo[T](resp)
}

// PatchTo executes a PATCH request with payload using [DefaultClient] and decodes the response payload into T.
//
// Resource Management:
// The response body is automatically drained and closed. Callers do NOT need to call resp.Body.Close().
func PatchTo[T any](ctx context.Context, path string, body any, mods ...RequestModifier) (*T, error) {
	resp, err := DefaultClient.Patch(ctx, path, injectBodyMod(body, mods)...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	return decodeResponseTo[T](resp)
}

// DeleteTo executes a DELETE request using [DefaultClient] and decodes any returned response payload into T.
//
// Resource Management:
// The response body is automatically drained and closed. Callers do NOT need to call resp.Body.Close().
func DeleteTo[T any](ctx context.Context, path string, mods ...RequestModifier) (*T, error) {
	resp, err := DefaultClient.Delete(ctx, path, mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	return decodeResponseTo[T](resp)
}

// Fetch executes a GET request using [DefaultClient] and returns a functional [generic.Result] containing the parsed T.
//
// This is particularly useful in functional error-handling pipelines (e.g. railway-oriented programming)
// where callers want to inspect Success/Failure states without multiple if-err guards.
func Fetch[T any](ctx context.Context, path string, mods ...RequestModifier) (generic.Result[T], *http.Response) {
	resp, err := DefaultClient.Get(ctx, path, mods...) //nolint:bodyclose
	if err != nil {
		return generic.Failure[T](err), resp
	}

	target, decodeErr := decodeResponseTo[T](resp)
	if decodeErr != nil {
		return generic.Failure[T](decodeErr), resp
	}

	return generic.Success(*target), resp
}

// FetchTyped executes a GET request using the shared default client and returns a [generic.TypedResult]
// wrapping the unmarshaled response or a structured [*APIError], conforming to Swift-style Typed Throws.
func FetchTyped[T any](
	ctx context.Context,
	path string,
	mods ...RequestModifier,
) (generic.TypedResult[T, *APIError], *http.Response) {
	resp, err := DefaultClient.Get(ctx, path, mods...) //nolint:bodyclose
	if err != nil {
		return AsTypedResult(generic.Zero[T](), err), resp
	}

	target, decodeErr := decodeResponseTo[T](resp)
	if decodeErr != nil {
		return AsTypedResult(generic.Zero[T](), decodeErr), resp
	}

	return generic.SuccessTyped[T, *APIError](*target), resp
}

// Scoped executes fn within an isolated, ephemeral [Client] scope configured with opts.
// The ephemeral client is deep-copied from client (or [DefaultClient] if nil) and cleanly closed after execution.
func Scoped[T any](client *Client, fn func(*Client) (T, error), opts ...ClientOption) (T, error) {
	base := client
	if base == nil {
		base = DefaultClient
	}

	scopedClient := base.With(opts...)
	defer scopedClient.Close()

	if fn == nil {
		return generic.Zero[T](), nil
	}

	return fn(scopedClient)
}

// WithHeader constructs an [RequestModifier] setting a single request header key to value.
func WithHeader(key, value string) RequestModifier {
	return RequestModifier{
		Kind:  core.ModHeader,
		Key:   key,
		Value: value,
	}
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

// WithBearer constructs an [RequestModifier] setting an "Authorization: Bearer <token>" header (RFC 6750 §2.1).
func WithBearer(token string) RequestModifier {
	return RequestModifier{
		Kind:  core.ModBearer,
		Value: token,
	}
}

// WithBasicAuth constructs an [RequestModifier] setting HTTP Basic Authentication credentials (RFC 7617).
func WithBasicAuth(username, password string) RequestModifier {
	return RequestModifier{
		Kind:  core.ModBasicAuth,
		Key:   username,
		Value: password,
	}
}

// WithPKCE constructs an [RequestModifier] adding PKCE code_challenge and code_challenge_method
// parameters for OAuth 2.0 authorization requests per RFC 7636 §4.3 and RFC 9700 §2.1.
// If method is omitted or empty, S256 is used by default.
func WithPKCE(verifier string, method ...string) RequestModifier {
	m := fpkce.MethodS256
	if len(method) > 0 && method[0] != "" {
		m = method[0]
	}

	if challenge, err := fpkce.ComputeChallenge(verifier, m); err == nil {
		verifier = challenge
	}

	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			req.AddQueryParam("code_challenge", verifier)
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

// WithTimeout constructs an [RequestModifier] attaching a deadline timeout to the request context.
func WithTimeout(d time.Duration) RequestModifier {
	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			ctx, cancel := context.WithTimeout(req.Context(), d) //nolint:gosec
			req.SetContext(ctx)
			pipeline.GetOrInitRequestConfig(req).RequestTimeoutCancel = cancel
		},
	}
}

// WithRetry constructs an [RequestModifier] setting the maximum retry attempts for the request.
func WithRetry(attempts int) RequestModifier {
	policy := core.RetryOverride{MaxAttempts: attempts}
	if policy.MaxAttempts < 1 {
		policy.MaxAttempts = 1
	}

	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			pipeline.GetOrInitRequestConfig(req).RetryPolicy = &policy
		},
	}
}

// WithUserAgent constructs an [RequestModifier] setting the User-Agent header (RFC 9110 §10.1.5).
func WithUserAgent(ua string) RequestModifier {
	return WithHeader("User-Agent", ua)
}

// WithContentType constructs an [RequestModifier] overriding the Content-Type header (RFC 9110 §8.3).
func WithContentType(ct string) RequestModifier {
	return WithHeader("Content-Type", ct)
}

// WithAccept constructs an [RequestModifier] overriding the Accept header (RFC 9110 §12.5.1).
func WithAccept(accept string) RequestModifier {
	return WithHeader("Accept", accept)
}

// WithIfModifiedSince constructs an [RequestModifier] setting the If-Modified-Since header (RFC 9110 §5.6.7 & §13.1.3).
func WithIfModifiedSince(t time.Time) RequestModifier {
	return WithHeader("If-Modified-Since", t.UTC().Format(http.TimeFormat))
}

// WithIfUnmodifiedSince constructs an [RequestModifier] setting the If-Unmodified-Since header (RFC 9110 §5.6.7 & §13.1.4).
func WithIfUnmodifiedSince(t time.Time) RequestModifier {
	return WithHeader("If-Unmodified-Since", t.UTC().Format(http.TimeFormat))
}

// WithRange constructs an [RequestModifier] setting the Range header for byte-range requests (RFC 9110 §14.2).
func WithRange(start, end int64) RequestModifier {
	if start < 0 {
		return WithHeader("Range", "bytes="+strconv.FormatInt(start, 10))
	}

	if end < 0 {
		return WithHeader("Range", "bytes="+strconv.FormatInt(start, 10)+"-")
	}

	return WithHeader("Range", "bytes="+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10))
}

// WithCacheControl constructs an [RequestModifier] setting Cache-Control request directives (RFC 9111 §5.2.1).
func WithCacheControl(directives ...string) RequestModifier {
	return WithHeader("Cache-Control", strings.Join(directives, ", "))
}

// WithNoCache constructs an [RequestModifier] forcing cache revalidation via "Cache-Control: no-cache" (RFC 9111 §5.2.1.4).
func WithNoCache() RequestModifier {
	return WithHeader("Cache-Control", "no-cache")
}

// WithNoStore constructs an [RequestModifier] preventing response caching via "Cache-Control: no-store" (RFC 9111 §5.2.1.5).
func WithNoStore() RequestModifier {
	return WithHeader("Cache-Control", "no-store")
}

// WithBaseURL returns an [ClientOption] configuring the default Base URI for relative requests (RFC 3986 §5.1).
// Ensures a trailing slash per RFC 3986 §5.2.3 to preserve hierarchical base path segments during relative path resolution.
func WithBaseURL(raw string) ClientOption {
	return func(cfg *Config) {
		if raw == "" {
			cfg.Defaults.BaseURL = &url.URL{}
			return
		}

		formatted := raw
		if !strings.HasSuffix(formatted, "/") {
			formatted += "/"
		}

		baseURL, err := url.Parse(formatted)
		if err != nil {
			return
		}

		cfg.Defaults.BaseURL = baseURL
	}
}

// WithClientTimeout returns an [ClientOption] configuring the default timeout duration for requests.
func WithClientTimeout(d time.Duration) ClientOption {
	return func(cfg *Config) {
		cfg.Engine.Timeout = d
	}
}

// WithClientUserAgent returns an [ClientOption] setting the default User-Agent header for all requests.
func WithClientUserAgent(ua string) ClientOption {
	return func(cfg *Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		cfg.Defaults.Headers.Set("User-Agent", ua)
	}
}

// WithChrome returns an [ClientOption] setting the browser profile to Google Chrome.
func WithChrome() ClientOption {
	return func(cfg *Config) {
		cfg.Fingerprint.BrowserID = BrowserChrome
	}
}

// WithFirefox returns an [ClientOption] setting the browser profile to Mozilla Firefox.
func WithFirefox() ClientOption {
	return func(cfg *Config) {
		cfg.Fingerprint.BrowserID = BrowserFirefox
	}
}

// WithSafari returns an [ClientOption] setting the browser profile to Apple Safari.
func WithSafari() ClientOption {
	return func(cfg *Config) {
		cfg.Fingerprint.BrowserID = BrowserSafari
	}
}

// WithSoftErrorDetector returns an [ClientOption] registering callbacks that sniff initial
// response body bytes to catch application-level soft errors without draining or consuming the body stream.
func WithSoftErrorDetector(detectors ...SoftErrorDetector) ClientOption {
	return func(cfg *Config) {
		cfg.Defaults.SoftErrorDetectors = append(cfg.Defaults.SoftErrorDetectors, detectors...)
	}
}

// WithBlockRedirectTo returns an [ClientOption] that halts redirects to matching URLs (e.g. "/login").
func WithBlockRedirectTo(patterns ...string) ClientOption {
	return func(cfg *Config) {
		cfg.Engine.CheckRedirect = BlockPathRedirectPolicy(patterns...)
	}
}

// WithSmartBody constructs an [RequestModifier] that dynamically inspects and serializes arbitrary payloads.
//
// # Serialization Matrix & Content-Type Resolution
//
// WithSmartBody eliminates the need for manual marshaling or header declaration by applying
// the following zero-reflection type-switch matrix:
//   - [RequestModifier]: Passed through directly as an existing modifier atom.
//   - [proto.Message]: Serialized via [proto.Marshal] with Content-Type "application/x-protobuf".
//   - [url.Values]: URL-encoded form data with Content-Type "application/x-www-form-urlencoded".
//   - [io.Reader]: Configured as a direct streaming body ([core.ModBodyStream]).
//   - []byte: Transmitted as raw binary bytes ([core.ModBodyBytes]).
//   - string: Transmitted as UTF-8 plaintext with Content-Type "text/plain; charset=utf-8".
//   - Struct / Map / Slice / any other: Serialized via [json.Marshal] with Content-Type "application/json".
//
// # Error Handling & Pipeline Interception
//
// If serialization fails (e.g. JSON marshaling encountering unsupported channels/functions),
// WithSmartBody does NOT panic. Instead, it embeds the serialization error into a deferred modifier
// ([pipeline.RequestConfig.BodyError]), aborting execution cleanly before any data is sent over the network.
func WithSmartBody(body any) RequestModifier {
	if body == nil {
		return RequestModifier{}
	}

	switch b := body.(type) {
	case RequestModifier:
		return b

	case proto.Message:
		bodyBytes, err := proto.Marshal(b)
		if err != nil {
			return RequestModifier{
				Kind: core.ModCustom,
				Fn: func(req Request) {
					pipeline.GetOrInitRequestConfig(req).BodyError = err
				},
			}
		}

		return RequestModifier{
			Kind:        core.ModBodyBytes,
			ContentType: "application/x-protobuf",
			Bytes:       bodyBytes,
		}

	case url.Values:
		return RequestModifier{
			Kind:        core.ModBodyBytes,
			ContentType: "application/x-www-form-urlencoded",
			Bytes:       []byte(b.Encode()),
		}

	case io.Reader:
		return RequestModifier{
			Kind:   core.ModBodyStream,
			Stream: b,
		}

	case []byte:
		return RequestModifier{
			Kind:  core.ModBodyBytes,
			Bytes: b,
		}

	case string:
		return RequestModifier{
			Kind:        core.ModBodyBytes,
			ContentType: "text/plain; charset=utf-8",
			Bytes:       bytesconv.S2B(b),
		}

	default:
		bodyBytes, err := json.Marshal(b)
		if err != nil {
			return RequestModifier{
				Kind: core.ModCustom,
				Fn: func(req Request) {
					pipeline.GetOrInitRequestConfig(req).BodyError = err
				},
			}
		}

		return RequestModifier{
			Kind:        core.ModBodyBytes,
			ContentType: "application/json",
			Bytes:       bodyBytes,
		}
	}
}

// PeekResponse peeks up to n bytes from resp.Body without consuming or draining the stream.
// It wraps resp.Body in a buffered reader if not already peekable, preserving full readability.
func PeekResponse(resp *http.Response, n int) ([]byte, error) {
	if resp == nil || resp.Body == nil {
		return nil, nil
	}

	return pipeline.PeekResponseBody(resp, n)
}
