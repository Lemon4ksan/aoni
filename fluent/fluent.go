// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fluent

import (
	"context"
	"net/http"

	"github.com/lemon4ksan/aoni"
)

// New initializes a new pooled [Request] builder bound to the target client.
// The returned [Request] is retrieved from an internal sync.Pool and is automatically
// recycled back to the pool upon execution.
func New(client *aoni.Client) *Request {
	r := requestPool.Get().(*Request)
	r.client = client
	return r
}

// R is a convenient short alias for [New].
func R(client *aoni.Client) *Request {
	return New(client)
}

// GetJSON dispatches a GET request and unmarshals a 2xx JSON response directly into T.
func GetJSON[T any](ctx context.Context, c *aoni.Client, path string) (T, *http.Response, error) {
	var target T

	resp, err := R(c).SetContext(ctx).SetResult(&target).Get(path)

	return target, resp, err
}

// PostJSON dispatches a POST request with body and unmarshals a 2xx JSON response into T.
func PostJSON[T any](ctx context.Context, c *aoni.Client, path string, body any) (T, *http.Response, error) {
	var target T

	resp, err := R(c).SetContext(ctx).SetBody(body).SetResult(&target).Post(path)

	return target, resp, err
}

// PutJSON dispatches a PUT request with body and unmarshals a 2xx JSON response into T.
func PutJSON[T any](ctx context.Context, c *aoni.Client, path string, body any) (T, *http.Response, error) {
	var target T

	resp, err := R(c).SetContext(ctx).SetBody(body).SetResult(&target).Put(path)

	return target, resp, err
}

// PatchJSON dispatches a PATCH request with body and unmarshals a 2xx JSON response into T.
func PatchJSON[T any](ctx context.Context, c *aoni.Client, path string, body any) (T, *http.Response, error) {
	var target T

	resp, err := R(c).SetContext(ctx).SetBody(body).SetResult(&target).Patch(path)

	return target, resp, err
}

// DeleteJSON dispatches a DELETE request and unmarshals a 2xx JSON response into T.
func DeleteJSON[T any](ctx context.Context, c *aoni.Client, path string) (T, *http.Response, error) {
	var target T

	resp, err := R(c).SetContext(ctx).SetResult(&target).Delete(path)

	return target, resp, err
}

// DoJSON dispatches a request with any custom method and optional body, unmarshaling a 2xx JSON response into T.
func DoJSON[T any](ctx context.Context, c *aoni.Client, method, path string, body any) (T, *http.Response, error) {
	var target T

	req := R(c).SetContext(ctx).SetResult(&target)
	if body != nil {
		req.SetBody(body)
	}

	resp, err := req.Do(method, path)

	return target, resp, err
}
