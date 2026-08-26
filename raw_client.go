// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"net/http"
)

// RawClient provides direct access to raw HTTP stream methods returning (*http.Response, error).
// It enables low-level byte streaming, header inspection, and custom response body handling.
type RawClient struct {
	client *Client
}

// Raw returns a [RawClient] facade for executing raw HTTP stream requests.
func (c *Client) Raw() *RawClient {
	return &RawClient{client: c}
}

// Request executes an HTTP request using method, path, and modifiers, returning the raw [*http.Response].
// Caller MUST close resp.Body.
func (r *RawClient) Request(
	ctx context.Context,
	method, path string,
	mods ...RequestModifier,
) (*http.Response, error) {
	return r.client.Request(ctx, method, path, mods...)
}

// Get executes an HTTP GET request and returns the raw [*http.Response].
// Caller MUST close resp.Body.
func (r *RawClient) Get(
	ctx context.Context,
	path string,
	mods ...RequestModifier,
) (*http.Response, error) {
	return r.client.Request(ctx, http.MethodGet, path, mods...)
}

// Post executes an HTTP POST request and returns the raw [*http.Response].
// Caller MUST close resp.Body.
func (r *RawClient) Post(
	ctx context.Context,
	path string,
	mods ...RequestModifier,
) (*http.Response, error) {
	return r.client.Request(ctx, http.MethodPost, path, mods...)
}

// Put executes an HTTP PUT request and returns the raw [*http.Response].
// Caller MUST close resp.Body.
func (r *RawClient) Put(
	ctx context.Context,
	path string,
	mods ...RequestModifier,
) (*http.Response, error) {
	return r.client.Request(ctx, http.MethodPut, path, mods...)
}

// Patch executes an HTTP PATCH request and returns the raw [*http.Response].
// Caller MUST close resp.Body.
func (r *RawClient) Patch(
	ctx context.Context,
	path string,
	mods ...RequestModifier,
) (*http.Response, error) {
	return r.client.Request(ctx, http.MethodPatch, path, mods...)
}

// Delete executes an HTTP DELETE request and returns the raw [*http.Response].
// Caller MUST close resp.Body.
func (r *RawClient) Delete(
	ctx context.Context,
	path string,
	mods ...RequestModifier,
) (*http.Response, error) {
	return r.client.Request(ctx, http.MethodDelete, path, mods...)
}

// Head executes an HTTP HEAD request and returns the raw [*http.Response].
// Caller MUST close resp.Body.
func (r *RawClient) Head(
	ctx context.Context,
	path string,
	mods ...RequestModifier,
) (*http.Response, error) {
	return r.client.Request(ctx, http.MethodHead, path, mods...)
}

// Options executes an HTTP OPTIONS request and returns the raw [*http.Response].
// Caller MUST close resp.Body.
func (r *RawClient) Options(
	ctx context.Context,
	path string,
	mods ...RequestModifier,
) (*http.Response, error) {
	return r.client.Request(ctx, http.MethodOptions, path, mods...)
}

// Trace executes an HTTP TRACE request and returns the raw [*http.Response].
// Caller MUST close resp.Body.
func (r *RawClient) Trace(
	ctx context.Context,
	path string,
	mods ...RequestModifier,
) (*http.Response, error) {
	return r.client.Request(ctx, http.MethodTrace, path, mods...)
}

// Connect executes an HTTP CONNECT request and returns the raw [*http.Response].
// Caller MUST close resp.Body.
func (r *RawClient) Connect(
	ctx context.Context,
	path string,
	mods ...RequestModifier,
) (*http.Response, error) {
	return r.client.Request(ctx, http.MethodConnect, path, mods...)
}
