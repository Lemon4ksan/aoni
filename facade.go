// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/net/http/header"

	"github.com/lemon4ksan/aoni/internal/pipeline"
	"github.com/lemon4ksan/aoni/mod"
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
	return DefaultClient.Request(ctx, http.MethodGet, path, mods...)
}

// Post executes an HTTP POST request against path using the shared [DefaultClient].
//
// Invariants:
//   - Caller MUST close resp.Body.
func Post(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return DefaultClient.Request(ctx, http.MethodPost, path, mods...)
}

// Put executes an HTTP PUT request against path using the shared [DefaultClient].
//
// Invariants:
//   - Caller MUST close resp.Body.
func Put(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return DefaultClient.Request(ctx, http.MethodPut, path, mods...)
}

// Patch executes an HTTP PATCH request against path using the shared [DefaultClient].
//
// Invariants:
//   - Caller MUST close resp.Body.
func Patch(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return DefaultClient.Request(ctx, http.MethodPatch, path, mods...)
}

// Delete executes an HTTP DELETE request against path using the shared [DefaultClient].
//
// Invariants:
//   - Caller MUST close resp.Body.
func Delete(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return DefaultClient.Request(ctx, http.MethodDelete, path, mods...)
}

// Head executes an HTTP HEAD request against path using the shared [DefaultClient] to inspect headers.
//
// Invariants:
//   - Caller MUST close resp.Body.
func Head(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return DefaultClient.Request(ctx, http.MethodHead, path, mods...)
}

// Options executes an HTTP OPTIONS request against path using the shared [DefaultClient].
//
// Invariants:
//   - Caller MUST close resp.Body.
func Options(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return DefaultClient.Request(ctx, http.MethodOptions, path, mods...)
}

// GetTo executes a 1-line typed GET request using [DefaultClient] and decodes the response into a newly allocated T.
//
// Automatically handles decompression (gzip, brotli, zstd) and Content-Type negotiation.
//
// # Resource Management
//
// The response body is automatically drained and closed. Callers do NOT need to call resp.Body.Close().
//
// # Error Handling
//
// Returns an [*APIError] on non-2xx status codes (4xx/5xx). Use single-line predicate helpers
// like [IsNotFound], [IsRateLimited], or standard [errors.Is] to inspect the error.
//
// # Example
//
//	type User struct {
//	    ID   int    `json:"id"`
//	    Name string `json:"name"`
//	}
//
//	user, err := aoni.GetTo[User](ctx, "https://api.github.com/users/octocat")
//	if err != nil {
//	    if aoni.IsNotFound(err) {
//	        log.Fatal("User not found")
//	    }
//	    log.Fatal(err)
//	}
func GetTo[T any](ctx context.Context, path string, mods ...RequestModifier) (*T, error) {
	return DefaultClient.GetTo[T](ctx, path, mods...)
}

// PostTo executes a 1-line typed POST request carrying body using [DefaultClient] and decodes the response into T.
//
// The body argument is automatically serialized based on its type:
//   - Struct / Map / Slice -> JSON with "Content-Type: application/json"
//   - [proto.Message] -> Protobuf with "Content-Type: application/x-protobuf"
//   - [url.Values] -> Form data with "Content-Type: application/x-www-form-urlencoded"
//   - `[]byte` / `string` -> Raw payload
//
// # Resource Management
//
// The response body is automatically drained and closed. Callers do NOT need to call resp.Body.Close().
//
// # Example
//
//	newUser, err := aoni.PostTo[User](ctx, "https://api.example.com/users", CreateUserReq{Name: "Alice"})
func PostTo[T any](ctx context.Context, path string, body any, mods ...RequestModifier) (*T, error) {
	return DefaultClient.PostTo[T](ctx, path, body, mods...)
}

// PutTo executes a 1-line typed PUT request carrying body using [DefaultClient] and decodes the response into T.
//
// See [PostTo] for automatic body detection, serialization rules, and resource management.
func PutTo[T any](ctx context.Context, path string, body any, mods ...RequestModifier) (*T, error) {
	return DefaultClient.PutTo[T](ctx, path, body, mods...)
}

// PatchTo executes a 1-line typed PATCH request carrying body using [DefaultClient] and decodes the response into T.
//
// See [PostTo] for automatic body detection, serialization rules, and resource management.
func PatchTo[T any](ctx context.Context, path string, body any, mods ...RequestModifier) (*T, error) {
	return DefaultClient.PatchTo[T](ctx, path, body, mods...)
}

// DeleteTo executes a 1-line typed DELETE request using [DefaultClient] and decodes any returned payload into T.
//
// See [GetTo] for automatic decompression, content-type negotiation, and resource management.
func DeleteTo[T any](ctx context.Context, path string, mods ...RequestModifier) (*T, error) {
	return DefaultClient.DeleteTo[T](ctx, path, mods...)
}

// GetInto executes a typed GET request using [DefaultClient] and decodes the response directly into target without allocations.
//
// # Example
//
//	var user User
//	err := aoni.GetInto(ctx, "https://api.example.com/users/42", &user)
func GetInto[T any](ctx context.Context, path string, target *T, mods ...RequestModifier) error {
	return DefaultClient.GetInto(ctx, path, target, mods...)
}

// PostInto executes a typed POST request carrying body using [DefaultClient] and decodes the response directly into target.
//
// See [PostTo] for body serialization rules and [GetInto] for zero-allocation target decoding.
func PostInto[T any](ctx context.Context, path string, body any, target *T, mods ...RequestModifier) error {
	return DefaultClient.PostInto(ctx, path, body, target, mods...)
}

// PutInto executes a typed PUT request carrying body using [DefaultClient] and decodes the response directly into target.
//
// See [PostInto] for details.
func PutInto[T any](ctx context.Context, path string, body any, target *T, mods ...RequestModifier) error {
	return DefaultClient.PutInto(ctx, path, body, target, mods...)
}

// PatchInto executes a typed PATCH request carrying body using [DefaultClient] and decodes the response directly into target.
//
// See [PostInto] for details.
func PatchInto[T any](ctx context.Context, path string, body any, target *T, mods ...RequestModifier) error {
	return DefaultClient.PatchInto(ctx, path, body, target, mods...)
}

// DeleteInto executes a typed DELETE request using [DefaultClient] and decodes the response directly into target.
//
// See [GetInto] for details.
func DeleteInto[T any](ctx context.Context, path string, target *T, mods ...RequestModifier) error {
	return DefaultClient.DeleteInto(ctx, path, target, mods...)
}

// FetchInto executes an arbitrary HTTP method request using [DefaultClient] and decodes the response directly into target.
func FetchInto[T any](
	ctx context.Context,
	method, path string,
	body any,
	target *T,
	mods ...RequestModifier,
) error {
	return DefaultClient.FetchInto(ctx, method, path, body, target, mods...)
}

// GetEx executes a typed GET request using [DefaultClient] and returns both the unmarshaled *T and raw [*http.Response].
func GetEx[T any](ctx context.Context, path string, mods ...RequestModifier) (*T, *http.Response, error) {
	return DefaultClient.GetEx[T](ctx, path, mods...)
}

// PostEx executes a typed POST request carrying body using [DefaultClient] and returns both *T and raw [*http.Response].
func PostEx[T any](
	ctx context.Context,
	path string,
	body any,
	mods ...RequestModifier,
) (*T, *http.Response, error) {
	return DefaultClient.PostEx[T](ctx, path, body, mods...)
}

// PutEx executes a typed PUT request carrying body using [DefaultClient] and returns both *T and raw [*http.Response].
func PutEx[T any](
	ctx context.Context,
	path string,
	body any,
	mods ...RequestModifier,
) (*T, *http.Response, error) {
	return DefaultClient.PutEx[T](ctx, path, body, mods...)
}

// PatchEx executes a typed PATCH request carrying body using [DefaultClient] and returns both *T and raw [*http.Response].
func PatchEx[T any](
	ctx context.Context,
	path string,
	body any,
	mods ...RequestModifier,
) (*T, *http.Response, error) {
	return DefaultClient.PatchEx[T](ctx, path, body, mods...)
}

// DeleteEx executes a typed DELETE request using [DefaultClient] and returns both *T and raw [*http.Response].
func DeleteEx[T any](ctx context.Context, path string, mods ...RequestModifier) (*T, *http.Response, error) {
	return DefaultClient.DeleteEx[T](ctx, path, mods...)
}

// FetchEx executes an arbitrary HTTP method request using [DefaultClient] and returns both *T and raw [*http.Response].
func FetchEx[T any](
	ctx context.Context,
	method, path string,
	body any,
	mods ...RequestModifier,
) (*T, *http.Response, error) {
	return DefaultClient.FetchEx[T](ctx, method, path, body, mods...)
}

// Fetch executes a GET request using [DefaultClient] and returns a functional [generic.Result] containing the parsed T.
//
// Enables Railway-Oriented Programming (ROP) and functional error handling without repetitive if-err checks.
//
// # Example
//
//	result, resp := aoni.Fetch[User](ctx, "https://api.github.com/users/octocat")
//	if result.IsSuccess() {
//	    fmt.Printf("User: %s\n", result.Value().Name)
//	}
func Fetch[T any](ctx context.Context, path string, mods ...RequestModifier) (generic.Result[T], *http.Response) {
	val, resp, err := DefaultClient.GetEx[T](ctx, path, mods...)
	if err != nil {
		return generic.Failure[T](err), resp
	}

	return generic.Success(generic.Deref(val)), resp
}

// FetchTyped executes a GET request and returns a strongly-typed [generic.TypedResult] wrapping [*APIError],
// enabling explicit, type-safe error handling without untyped errors.
//
// # Example
//
//	result, _ := aoni.FetchTyped[User](ctx, "https://api.example.com/users/42")
//	if result.IsFailure() {
//	    apiErr := result.Error()
//	    log.Printf("API Error %d: %s", apiErr.StatusCode, apiErr.BodyString())
//	}
func FetchTyped[T any](
	ctx context.Context,
	path string,
	mods ...RequestModifier,
) (generic.TypedResult[T, *APIError], *http.Response) {
	val, resp, err := DefaultClient.GetEx[T](ctx, path, mods...)
	if err != nil {
		return AsTypedResult(generic.Zero[T](), err), resp
	}

	return generic.SuccessTyped[T, *APIError](generic.Deref(val)), resp
}

// Scoped executes fn within an isolated, ephemeral [Client] instance configured with opts.
//
// The ephemeral client is deep-copied from client (or [DefaultClient] if nil) and automatically closed after fn finishes.
//
// # Example
//
//	user, err := aoni.Scoped(nil, func(c *aoni.Client) (*User, error) {
//	    return c.GetTo[User](ctx, "/users/1")
//	}, option.WithChrome(), option.WithTimeout(5*time.Second))
func Scoped[T any](client *Client, fn func(*Client) (T, error), opts ...ClientOption) (T, error) {
	base := generic.Coalesce(client, DefaultClient)

	scopedClient := base.With(opts...)
	defer scopedClient.Close()

	if fn == nil {
		return generic.Zero[T](), nil
	}

	return fn(scopedClient)
}

// PeekResponse peeks up to n bytes from resp.Body without consuming or draining the stream.
// It wraps resp.Body in a buffered reader if not already peekable, preserving full readability.
func PeekResponse(resp *http.Response, n int) ([]byte, error) {
	if resp == nil || resp.Body == nil {
		return nil, nil
	}

	return pipeline.PeekResponseBody(resp, n)
}

// ============================================================================
// Facade Request Modifiers
// ============================================================================

// WithHeader constructs a [RequestModifier] setting a single request header key to value.
func WithHeader(key, value string) RequestModifier {
	return mod.WithHeader(key, value)
}

// WithHeaders constructs a [RequestModifier] bulk-setting multiple HTTP request headers from a map.
func WithHeaders(headers map[string]string) RequestModifier {
	return mod.WithHeaders(headers)
}

// WithBearer constructs a [RequestModifier] setting an "Authorization: Bearer <token>" header (RFC 6750 §2.1).
func WithBearer(token string) RequestModifier {
	return mod.WithBearer(token)
}

// WithBasicAuth constructs a [RequestModifier] setting HTTP Basic Authentication credentials (RFC 7617).
func WithBasicAuth(username, password string) RequestModifier {
	return mod.WithBasicAuth(username, password)
}

// WithTimeout constructs a [RequestModifier] attaching a deadline timeout to the request context.
func WithTimeout(d time.Duration) RequestModifier {
	return mod.WithTimeout(d)
}

// WithRetry constructs a [RequestModifier] setting the maximum retry attempts for the request.
func WithRetry(attempts int) RequestModifier {
	return mod.WithRetry(attempts)
}

// WithUserAgent constructs a [RequestModifier] setting the User-Agent header (RFC 9110 §10.1.5).
func WithUserAgent(ua string) RequestModifier {
	return mod.WithUserAgent(ua)
}

// WithContentType constructs a [RequestModifier] overriding the Content-Type header (RFC 9110 §8.3).
func WithContentType(ct string) RequestModifier {
	return mod.WithContentType(ct)
}

// WithAccept constructs a [RequestModifier] overriding the Accept header (RFC 9110 §12.5.1).
func WithAccept(accept string) RequestModifier {
	return mod.WithAccept(accept)
}

// WithIfModifiedSince constructs a [RequestModifier] setting the If-Modified-Since header (RFC 9110 §5.6.7 & §13.1.3).
func WithIfModifiedSince(t time.Time) RequestModifier {
	return mod.WithIfModifiedSince(t)
}

// WithIfUnmodifiedSince constructs a [RequestModifier] setting the If-Unmodified-Since header (RFC 9110 §5.6.7 & §13.1.4).
func WithIfUnmodifiedSince(t time.Time) RequestModifier {
	return mod.WithIfUnmodifiedSince(t)
}

// WithRange constructs a [RequestModifier] setting the Range header for byte-range requests (RFC 9110 §14.2).
func WithRange(start, end int64) RequestModifier {
	return mod.WithRange(start, end)
}

// WithCacheControl constructs a [RequestModifier] setting Cache-Control request directives (RFC 9111 §5.2.1).
func WithCacheControl(directives ...string) RequestModifier {
	return mod.WithCacheControl(directives...)
}

// WithNoCache constructs a [RequestModifier] forcing cache revalidation via "Cache-Control: no-cache" (RFC 9111 §5.2.1.4).
func WithNoCache() RequestModifier {
	return mod.WithNoCache()
}

// WithNoStore constructs a [RequestModifier] preventing response caching via "Cache-Control: no-store" (RFC 9111 §5.2.1.5).
func WithNoStore() RequestModifier {
	return mod.WithNoStore()
}

// WithSmartBody constructs a [RequestModifier] that dynamically inspects and serializes arbitrary payloads.
func WithSmartBody(body any) RequestModifier {
	return mod.WithSmartBody(body)
}

// WithVar replaces a URI template variable placeholder (e.g. "{id}") in the request path (RFC 6570 Level 1).
func WithVar(key string, value any) RequestModifier {
	return mod.WithVar(key, value)
}

// WithVars replaces multiple URI template placeholders using alternating key-value pairs.
func WithVars(pairs ...any) RequestModifier {
	return mod.WithVars(pairs...)
}

// WithQuery appends key-value query parameters to the request URL.
func WithQuery(args ...any) RequestModifier {
	return mod.WithQuery(args...)
}

// WithQueryParams encodes a struct, map, or url.Values into request query parameters.
func WithQueryParams(query any) RequestModifier {
	return mod.WithQueryParams(query)
}

// Custom constructs a custom [RequestModifier] wrapping an arbitrary closure function.
func Custom(fn func(Request)) RequestModifier {
	return mod.Custom(fn)
}

// ============================================================================
// Facade Client Options
// ============================================================================

// WithBaseURL returns a [ClientOption] configuring the default Base URI for relative requests (RFC 3986 §5.1).
//
// # RFC 3986 Resolution & Slash Normalization
//
// Ensures a trailing slash per RFC 3986 §5.2.3 to preserve hierarchical base path segments during relative path resolution.
// Safely normalizes both leading and trailing slashes so combinations like BaseURL "https://api.com/v1/" + Path "/users"
// resolve seamlessly to "https://api.com/v1/users" without resetting to root or creating double slashes.
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

// WithClientTimeout returns a [ClientOption] configuring the default timeout duration for requests.
func WithClientTimeout(d time.Duration) ClientOption {
	return func(cfg *Config) {
		cfg.Engine.Timeout = d
	}
}

// WithClientUserAgent returns a [ClientOption] setting the default User-Agent header for all requests.
func WithClientUserAgent(ua string) ClientOption {
	return func(cfg *Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		cfg.Defaults.Headers.Set(header.UserAgent, ua)
	}
}

// WithSoftErrorDetector returns a [ClientOption] registering callbacks that sniff initial
// response body bytes to catch application-level soft errors without draining or consuming the body stream.
func WithSoftErrorDetector(detectors ...SoftErrorDetector) ClientOption {
	return func(cfg *Config) {
		cfg.Defaults.SoftErrorDetectors = append(cfg.Defaults.SoftErrorDetectors, detectors...)
	}
}

// WithBlockRedirectTo returns a [ClientOption] that halts redirects to matching URLs (e.g. "/login").
func WithBlockRedirectTo(patterns ...string) ClientOption {
	return func(cfg *Config) {
		cfg.Engine.CheckRedirect = BlockPathRedirectPolicy(patterns...)
	}
}

// WithModifier registers a default [RequestModifier] or custom modifier function executed on every outbound request.
//
// Supported types for fn:
//   - [RequestModifier]
//   - func([Request])
//   - func(*http.Request)
func WithModifier(fn any) ClientOption {
	return func(cfg *Config) {
		if fn == nil {
			return
		}

		switch m := fn.(type) {
		case RequestModifier:
			cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, m)
		case func(Request):
			if m != nil {
				cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, Custom(m))
			}
		case func(*http.Request):
			if m != nil {
				cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, Custom(func(r Request) {
					if req := r.HTTPRequest(); req != nil {
						m(req)
					}
				}))
			}
		}
	}
}

// WithModifiers registers one or more default [RequestModifier] functions executed on every outbound request.
func WithModifiers(mods ...RequestModifier) ClientOption {
	return func(cfg *Config) {
		cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, mods...)
	}
}
