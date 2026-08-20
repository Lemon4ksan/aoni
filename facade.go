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
	"strings"
	"time"

	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/internal/pipeline"
)

// DefaultClient is the shared default [Client] used for package-level direct request execution.
var DefaultClient = NewClient(nil)

// New instantiates a new [*Client] configured with the provided functional options.
func New(opts ...ClientOption) *Client {
	return NewClient(nil, opts...)
}

// Get executes a GET request against path using the shared default client.
func Get(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return DefaultClient.Get(ctx, path, mods...)
}

// Post executes a POST request against path using the shared default client.
func Post(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return DefaultClient.Post(ctx, path, mods...)
}

// Put executes a PUT request against path using the shared default client.
func Put(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return DefaultClient.Put(ctx, path, mods...)
}

// Patch executes a PATCH request against path using the shared default client.
func Patch(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return DefaultClient.Patch(ctx, path, mods...)
}

// Delete executes a DELETE request using the shared default client.
func Delete(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return DefaultClient.Delete(ctx, path, mods...)
}

// Head executes a HEAD request using the shared default client.
func Head(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return DefaultClient.Head(ctx, path, mods...)
}

// decodeResponseTo decodes the response body into target using registered content-type decoders.
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

// GetTo executes a GET request using the shared default client and decodes the response payload into T.
func GetTo[T any](ctx context.Context, path string, mods ...RequestModifier) (*T, error) {
	resp, err := DefaultClient.Get(ctx, path, mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	return decodeResponseTo[T](resp)
}

// PostTo executes a POST request with payload using the shared default client and decodes the response into T.
func PostTo[T any](ctx context.Context, path string, body any, mods ...RequestModifier) (*T, error) {
	resp, err := DefaultClient.Post(ctx, path, injectBodyMod(body, mods)...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	return decodeResponseTo[T](resp)
}

// PutTo executes a PUT request with payload using the shared default client and decodes the response into T.
func PutTo[T any](ctx context.Context, path string, body any, mods ...RequestModifier) (*T, error) {
	resp, err := DefaultClient.Put(ctx, path, injectBodyMod(body, mods)...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	return decodeResponseTo[T](resp)
}

// PatchTo executes a PATCH request with payload using the shared default client and decodes the response into T.
func PatchTo[T any](ctx context.Context, path string, body any, mods ...RequestModifier) (*T, error) {
	resp, err := DefaultClient.Patch(ctx, path, injectBodyMod(body, mods)...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	return decodeResponseTo[T](resp)
}

// DeleteTo executes a DELETE request using the shared default client and decodes the response into T.
func DeleteTo[T any](ctx context.Context, path string, mods ...RequestModifier) (*T, error) {
	resp, err := DefaultClient.Delete(ctx, path, mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	return decodeResponseTo[T](resp)
}

// Fetch executes a GET request using the shared default client and returns a [generic.Result] wrapping the unmarshaled response.
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
		var zero T
		return AsTypedResult(zero, err), resp
	}

	target, decodeErr := decodeResponseTo[T](resp)
	if decodeErr != nil {
		var zero T
		return AsTypedResult(zero, decodeErr), resp
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
		var zero T
		return zero, nil
	}

	return fn(scopedClient)
}

// WithHeader constructs an [aoni.RequestModifier] setting a single request header key to value.
func WithHeader(key, value string) RequestModifier {
	return RequestModifier{
		Kind:  core.ModHeader,
		Key:   key,
		Value: value,
	}
}

// WithHeaders constructs an [aoni.RequestModifier] bulk-setting multiple HTTP request headers from a map.
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

// WithBearer constructs an [aoni.RequestModifier] setting an "Authorization: Bearer <token>" header (RFC 6750 §2.1).
func WithBearer(token string) RequestModifier {
	return RequestModifier{
		Kind:  core.ModBearer,
		Value: token,
	}
}

// WithBasicAuth constructs an [aoni.RequestModifier] setting HTTP Basic Authentication credentials (RFC 7617).
func WithBasicAuth(username, password string) RequestModifier {
	return RequestModifier{
		Kind:  core.ModBasicAuth,
		Key:   username,
		Value: password,
	}
}

// WithTimeout constructs an [aoni.RequestModifier] attaching a deadline timeout to the request context.
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

// WithRetry constructs an [aoni.RequestModifier] setting the maximum retry attempts for the request.
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

// WithUserAgent constructs an [aoni.RequestModifier] setting the User-Agent header (RFC 9110 §10.1.5).
func WithUserAgent(ua string) RequestModifier {
	return WithHeader("User-Agent", ua)
}

// WithContentType constructs an [aoni.RequestModifier] overriding the Content-Type header (RFC 9110 §8.3).
func WithContentType(ct string) RequestModifier {
	return WithHeader("Content-Type", ct)
}

// WithAccept constructs an [aoni.RequestModifier] overriding the Accept header (RFC 9110 §12.5.1).
func WithAccept(accept string) RequestModifier {
	return WithHeader("Accept", accept)
}

// WithBaseURL returns an [aoni.ClientOption] configuring the default base URL for all relative requests.
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

// WithClientTimeout returns an [aoni.ClientOption] configuring the default timeout duration for requests.
func WithClientTimeout(d time.Duration) ClientOption {
	return func(cfg *Config) {
		cfg.Engine.Timeout = d
	}
}

// WithClientUserAgent returns an [aoni.ClientOption] setting the default User-Agent header for all requests.
func WithClientUserAgent(ua string) ClientOption {
	return func(cfg *Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		cfg.Defaults.Headers.Set("User-Agent", ua)
	}
}

// WithChrome returns an [aoni.ClientOption] setting the browser profile to Google Chrome.
func WithChrome() ClientOption {
	return func(cfg *Config) {
		cfg.Fingerprint.BrowserID = BrowserChrome
	}
}

// WithFirefox returns an [aoni.ClientOption] setting the browser profile to Mozilla Firefox.
func WithFirefox() ClientOption {
	return func(cfg *Config) {
		cfg.Fingerprint.BrowserID = BrowserFirefox
	}
}

// WithSafari returns an [aoni.ClientOption] setting the browser profile to Apple Safari.
func WithSafari() ClientOption {
	return func(cfg *Config) {
		cfg.Fingerprint.BrowserID = BrowserSafari
	}
}

// WithSmartBody constructs an [aoni.RequestModifier] that automatically detects the payload type:
//   - proto.Message -> Protobuf payload with application/x-protobuf
//   - url.Values -> URL-encoded form payload with application/x-www-form-urlencoded
//   - io.Reader -> Streamed request body
//   - []byte -> Raw byte slice payload
//   - string -> UTF-8 text payload with text/plain; charset=utf-8
//   - Struct / Map / Slice -> JSON-marshaled payload with application/json
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
