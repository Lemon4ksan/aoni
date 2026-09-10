// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"net/http"

	"github.com/lemon4ksan/foundation/generic"

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
