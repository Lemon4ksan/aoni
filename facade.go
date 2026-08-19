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
