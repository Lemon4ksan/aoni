// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod

import (
	"fmt"
	"net/url"

	furl "github.com/lemon4ksan/foundation/net/urlkit"

	"github.com/lemon4ksan/aoni/codec/values"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/internal/pipeline"
)

// WithVar replaces a single URI template variable placeholder (e.g. "{id}") in the request path (RFC 6570 Level 1).
//
// Automatically applies URL path percent-encoding to the value.
//
// # Example
//
//	// Performs GET to "/users/42/details"
//	resp, err := client.Get(ctx, "/users/{id}/details",
//	    mod.WithVar("id", 42),
//	)
//
// # RFC Compliance
//
// Conforms to RFC 6570 (URI Template), RFC 3986 (Uniform Resource Identifier: Generic Syntax), and RFC 8820.
func WithVar(key string, value any) RequestModifier {
	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			escapedValue := url.PathEscape(fmt.Sprint(value))
			path := req.Path()
			req.SetPath(furl.ReplaceVar(path, key, escapedValue))
		},
	}
}

// WithVars replaces multiple URI template placeholders using alternating key-value pairs.
//
// # Example
//
//	// Performs GET to "/orgs/golang/repos/go/issues"
//	resp, err := client.Get(ctx, "/orgs/{org}/repos/{repo}/issues",
//	    mod.WithVars("org", "golang", "repo", "go"),
//	)
//
// # Invariants
//
// Requires an even number of arguments; otherwise records an error on the request config.
func WithVars(pairs ...any) RequestModifier {
	if len(pairs)%2 != 0 {
		return RequestModifier{
			Kind: core.ModCustom,
			Fn: func(req Request) {
				getOrInitRequestConfig(req).BodyError = ErrInvalidPairCount
			},
		}
	}

	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			for i := 0; i < len(pairs); i += 2 {
				key := fmt.Sprint(pairs[i])
				value := fmt.Sprint(pairs[i+1])
				WithVar(key, value).Apply(req)
			}
		},
	}
}

// WithBaseURL overrides the client's default Base URI for this specific request (RFC 3986 §5.1).
//
// # Example
//
//	resp, err := client.Get(ctx, "/metrics",
//	    mod.WithBaseURL("https://telemetry.internal.net"),
//	)
func WithBaseURL(baseURL string) RequestModifier {
	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			req.SetURL(baseURL)
		},
	}
}

// WithoutBaseURL resets the request target URL to the raw relative path, bypassing client BaseURL prepending.
func WithoutBaseURL() RequestModifier {
	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			req.SetURL(req.Path())
		},
	}
}

// WithQuery appends URL query parameters to the request URL (RFC 3986 §3.4).
//
// Supports two calling conventions:
//  1. Single struct or map: `mod.WithQuery(SearchParams{Query: "go", Page: 1})`
//  2. Key-value pair: `mod.WithQuery("limit", 20)`
//
// # Example
//
//	resp, err := client.Get(ctx, "/items",
//	    mod.WithQuery("sort", "desc"),
//	    mod.WithQuery("limit", 50),
//	)
func WithQuery(args ...any) RequestModifier {
	if len(args) == 1 {
		return WithQueryParams(args[0])
	}

	if len(args) >= 2 {
		key := fmt.Sprint(args[0])
		valStr := fmt.Sprint(args[1])

		return RequestModifier{
			Kind:  core.ModQueryAdd,
			Key:   key,
			Value: valStr,
		}
	}

	return RequestModifier{}
}

// WithQueryParams encodes a struct, map, or raw query string into URL query parameters.
//
// # Example
//
//	type Filter struct {
//	    Category string   `query:"category"`
//	    Tags     []string `query:"tags"`
//	}
//
//	resp, err := client.Get(ctx, "/products",
//	    mod.WithQueryParams(Filter{Category: "books", Tags: []string{"go", "tech"}}),
//	)
func WithQueryParams(query any) RequestModifier {
	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			if query == nil {
				return
			}

			encodedQuery, err := resolveQueryString(req, query)
			if err != nil {
				getOrInitRequestConfig(req).BodyError = err
				return
			}

			if encodedQuery == "" {
				return
			}

			if raw := req.RawQuery(); raw != "" {
				req.SetRawQuery(raw + "&" + encodedQuery)
			} else {
				req.SetRawQuery(encodedQuery)
			}
		},
	}
}

// resolveQueryString encodes query into URL query parameters using configured query encoder or default codec (RFC 3986 §3.4).
func resolveQueryString(req Request, query any) (string, error) {
	if s, ok := query.(string); ok {
		return s, nil
	}

	if b, ok := query.([]byte); ok {
		return string(b), nil
	}

	cfg := pipeline.GetRequestConfig(req.Context())
	if cfg != nil && cfg.QueryEncoder != nil {
		qVals, err := cfg.QueryEncoder(query)
		if err != nil || len(qVals) == 0 {
			return "", err
		}

		return qVals.Encode(), nil
	}

	return values.StructToQueryString(query)
}
