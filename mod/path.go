// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod

import (
	"fmt"
	"net/url"

	furl "github.com/lemon4ksan/foundation/net/url"

	"github.com/lemon4ksan/aoni/codec/values"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/internal/pipeline"
)

// WithVar constructs an [RequestModifier] that interpolates a single URI template placeholder
// (e.g. "{key}") with a percent-encoded value according to RFC 6570 Level 1, RFC 3986 §2.1, and RFC 8820 §3.
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

// WithVars constructs an [RequestModifier] replacing multiple path template placeholders using key-value pairs
// per RFC 6570 URI Template variable substitution (RFC 8820 §3) and RFC 3986 §2.1 / §3.3 path segment encoding.
// Requires an even number of arguments (alternating key and value pairs).
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

// WithBaseURL constructs an [RequestModifier] overriding the target request Base URI (RFC 3986 §5.1).
func WithBaseURL(baseURL string) RequestModifier {
	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			req.SetURL(baseURL)
		},
	}
}

// WithoutBaseURL constructs an [RequestModifier] resetting target request URL to the local path (RFC 3986 §5.1.4).
func WithoutBaseURL() RequestModifier {
	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			req.SetURL(req.Path())
		},
	}
}

// WithQuery constructs an [RequestModifier] appending query parameters to the request URL (RFC 3986 §3.4).
// Accepts either (key, value) pairs or a single struct/map query payload.
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

// WithQueryParams constructs an [RequestModifier] encoding structure or map query into URL query parameters (RFC 3986 §3.4).
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
