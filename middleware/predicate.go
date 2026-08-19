// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package middleware

import (
	"bytes"
	"encoding/json"
	stdio "io"
	"net/http"
	"strings"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/internal/std"
)

// Or combines multiple [core.RetryCondition] predicates, returning true if ANY condition is satisfied.
func Or(conditions ...core.RetryCondition) core.RetryCondition {
	return func(resp aoni.Response, err error) bool {
		for _, cond := range conditions {
			if cond != nil && cond(resp, err) {
				return true
			}
		}

		return false
	}
}

// And combines multiple [core.RetryCondition] predicates, returning true if ALL conditions are satisfied.
func And(conditions ...core.RetryCondition) core.RetryCondition {
	return func(resp aoni.Response, err error) bool {
		for _, cond := range conditions {
			if cond == nil || !cond(resp, err) {
				return false
			}
		}

		return true
	}
}

func newSyntheticResponse(
	statusCode int,
	contentType string,
	bodyReader stdio.Reader,
	contentLength int64,
	req aoni.Request,
) aoni.Response {
	header := make(http.Header)
	header.Set("Content-Type", contentType)

	var httpReq *http.Request
	if req != nil {
		httpReq = req.HTTPRequest()
	}

	return std.NewResponse(&http.Response{
		StatusCode:    statusCode,
		Header:        header,
		Body:          stdio.NopCloser(bodyReader),
		ContentLength: contentLength,
		Request:       httpReq,
	})
}

// FallbackString returns an [core.FallbackFunc] producing a static plaintext HTTP response.
func FallbackString(statusCode int, body string) core.FallbackFunc {
	return func(req aoni.Request, _ error) (aoni.Response, error) {
		return newSyntheticResponse(
			statusCode,
			"text/plain; charset=utf-8",
			strings.NewReader(body),
			int64(len(body)),
			req,
		), nil
	}
}

// FallbackJSON returns an [core.FallbackFunc] serializing payload as JSON in a synthetic HTTP response.
func FallbackJSON(statusCode int, payload any) core.FallbackFunc {
	return func(req aoni.Request, _ error) (aoni.Response, error) {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}

		return newSyntheticResponse(
			statusCode,
			"application/json",
			bytes.NewReader(data),
			int64(len(data)),
			req,
		), nil
	}
}
