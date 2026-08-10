// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod

import (
	"fmt"
	"net/url"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/values"
	"github.com/lemon4ksan/aoni/internal/urlutil"
)

// WithVar constructs an [aoni.RequestModifier] that interpolates a single URI template placeholder (e.g. "{key}") with value.
//
// Preconditions:
//   - Escapes value using [url.PathEscape].
func WithVar(key string, value any) aoni.RequestModifier {
	return func(req aoni.Request) {
		escapedValue := url.PathEscape(fmt.Sprint(value))

		path := req.Path()
		req.SetPath(urlutil.ReplaceVar(path, key, escapedValue))
	}
}

// WithVars constructs an [aoni.RequestModifier] replacing multiple path template placeholders using key-value pairs.
// Requires an even number of arguments (alternating key and value pairs).
func WithVars(pairs ...any) aoni.RequestModifier {
	if len(pairs)%2 != 0 {
		return func(req aoni.Request) {
			aoni.GetOrInitRequestConfig(req).BodyError = ErrInvalidPairCount
		}
	}

	return func(req aoni.Request) {
		for i := 0; i < len(pairs); i += 2 {
			key := fmt.Sprint(pairs[i])
			value := fmt.Sprint(pairs[i+1])
			WithVar(key, value)(req)
		}
	}
}

// WithBaseURL constructs an [aoni.RequestModifier] overriding target request URL base.
func WithBaseURL(baseURL string) aoni.RequestModifier {
	return func(req aoni.Request) {
		req.SetURL(baseURL)
	}
}

// WithoutBaseURL constructs an [aoni.RequestModifier] resetting target request URL base to local path.
func WithoutBaseURL() aoni.RequestModifier {
	return func(req aoni.Request) {
		req.SetURL(req.Path())
	}
}

// WithQuery constructs an [aoni.RequestModifier] appending query parameters to request URL.
// Accepts either (key, value) pairs or a single struct/map query payload.
func WithQuery(args ...any) aoni.RequestModifier {
	if len(args) == 1 {
		return WithQueryParams(args[0])
	}

	if len(args) >= 2 {
		key := fmt.Sprint(args[0])
		valStr := fmt.Sprint(args[1])

		return func(req aoni.Request) {
			req.AddQueryParam(key, valStr)
		}
	}

	return func(req aoni.Request) {}
}

// WithQueryParams constructs an [aoni.RequestModifier] encoding structure or map query into URL query parameters.
func WithQueryParams(query any) aoni.RequestModifier {
	return func(req aoni.Request) {
		if query == nil {
			return
		}

		encodedQuery, err := resolveQueryString(req, query)
		if err != nil {
			aoni.GetOrInitRequestConfig(req).BodyError = err
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
	}
}

func resolveQueryString(req aoni.Request, query any) (string, error) {
	cfg := aoni.GetRequestConfig(req.Context())
	if cfg != nil && cfg.QueryEncoder != nil {
		qVals, err := cfg.QueryEncoder(query)
		if err != nil || len(qVals) == 0 {
			return "", err
		}

		return qVals.Encode(), nil
	}

	return values.StructToQueryString(query)
}
