// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lemon4ksan/foundation/codec/json"
	"github.com/lemon4ksan/foundation/generic"
	fheader "github.com/lemon4ksan/foundation/net/http/header"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"github.com/lemon4ksan/foundation/timekit"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/internal/pipeline"
	"github.com/lemon4ksan/aoni/netutil/pkce"
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
	return DefaultClient.Get[T](ctx, path, mods...)
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
	return DefaultClient.Post[T](ctx, path, body, mods...)
}

// PutTo executes a 1-line typed PUT request carrying body using [DefaultClient] and decodes the response into T.
//
// # Example
//
//	updated, err := aoni.PutTo[User](ctx, "https://api.example.com/users/42", UpdateUserReq{Name: "Alice B."})
func PutTo[T any](ctx context.Context, path string, body any, mods ...RequestModifier) (*T, error) {
	return DefaultClient.Put[T](ctx, path, body, mods...)
}

// PatchTo executes a 1-line typed PATCH request carrying body using [DefaultClient] and decodes the response into T.
//
// # Example
//
//	patched, err := aoni.PatchTo[User](ctx, "https://api.example.com/users/42", map[string]string{"status": "online"})
func PatchTo[T any](ctx context.Context, path string, body any, mods ...RequestModifier) (*T, error) {
	return DefaultClient.Patch[T](ctx, path, body, mods...)
}

// DeleteTo executes a 1-line typed DELETE request using [DefaultClient] and decodes any returned payload into T.
//
// # Example
//
//	status, err := aoni.DeleteTo[DeleteStatus](ctx, "https://api.example.com/users/42")
func DeleteTo[T any](ctx context.Context, path string, mods ...RequestModifier) (*T, error) {
	return DefaultClient.Delete[T](ctx, path, mods...)
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

	return generic.Success(*val), resp
}

// FetchTyped executes a GET request and returns a strongly-typed [generic.TypedResult] wrapping [*APIError],
// conforming to Swift-style Typed Throws error models.
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

	return generic.SuccessTyped[T, *APIError](*val), resp
}

// Scoped executes fn within an isolated, ephemeral [Client] instance configured with opts.
//
// The ephemeral client is deep-copied from client (or [DefaultClient] if nil) and automatically closed after fn finishes.
//
// # Example
//
//	user, err := aoni.Scoped(nil, func(c *aoni.Client) (*User, error) {
//	    return c.Get[User](ctx, "/users/1")
//	}, option.WithChrome(), option.WithTimeout(5*time.Second))
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
	m := pkce.MethodS256
	if len(method) > 0 && method[0] != "" {
		m = method[0]
	}

	if challenge, err := pkce.ComputeChallenge(verifier, m); err == nil {
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
	return WithHeader(fheader.UserAgent, ua)
}

// WithContentType constructs an [RequestModifier] overriding the Content-Type header (RFC 9110 §8.3).
func WithContentType(ct string) RequestModifier {
	return WithHeader(fheader.ContentType, ct)
}

// WithAccept constructs an [RequestModifier] overriding the Accept header (RFC 9110 §12.5.1).
func WithAccept(accept string) RequestModifier {
	return WithHeader(fheader.Accept, accept)
}

// WithIfModifiedSince constructs an [RequestModifier] setting the If-Modified-Since header (RFC 9110 §5.6.7 & §13.1.3).
func WithIfModifiedSince(t time.Time) RequestModifier {
	return WithHeader(fheader.IfModifiedSince, timekit.FormatHTTPDate(t))
}

// WithIfUnmodifiedSince constructs an [RequestModifier] setting the If-Unmodified-Since header (RFC 9110 §5.6.7 & §13.1.4).
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

// WithBaseURL returns an [ClientOption] configuring the default Base URI for relative requests (RFC 3986 §5.1).
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

		cfg.Defaults.Headers.Set(fheader.UserAgent, ua)
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
			ContentType: fheader.MIMEApplicationProtobuf,
			Bytes:       bodyBytes,
		}

	case url.Values:
		return RequestModifier{
			Kind:        core.ModBodyBytes,
			ContentType: fheader.MIMEApplicationForm,
			Bytes:       bytesconv.S2B(b.Encode()),
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
			ContentType: fheader.MIMETextPlainCharsetUTF8,
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
			ContentType: fheader.MIMEApplicationJSON,
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
