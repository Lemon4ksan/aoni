// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"context"
	"net/http"

	"github.com/lemon4ksan/foundation/codec/json"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/mod"
)

// --- Raw Non-Generic HTTP Methods on *Client ---

// Get executes a fast HTTP GET request and returns the raw [aoni.Response].
//
// # Resource Management
//
// Caller MUST call resp.Close() when done to return buffers to the pool.
//
// # Example
//
//	resp, err := fastClient.Get(ctx, "/users/42")
//	if err != nil {
//	    return err
//	}
//	defer resp.Close()
func (c *Client) Get(ctx context.Context, path string, mods ...aoni.RequestModifier) (aoni.Response, error) {
	return c.executeFast(ctx, http.MethodGet, path, nil, mods)
}

// Post executes a fast HTTP POST request carrying body and returns the raw [aoni.Response].
//
// # Resource Management
//
// Caller MUST call resp.Close() when done to return buffers to the pool.
func (c *Client) Post(
	ctx context.Context,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (aoni.Response, error) {
	return c.executeFast(ctx, http.MethodPost, path, body, mods)
}

// Put executes a fast HTTP PUT request carrying body and returns the raw [aoni.Response].
//
// # Resource Management
//
// Caller MUST call resp.Close() when done to return buffers to the pool.
func (c *Client) Put(
	ctx context.Context,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (aoni.Response, error) {
	return c.executeFast(ctx, http.MethodPut, path, body, mods)
}

// Patch executes a fast HTTP PATCH request carrying body and returns the raw [aoni.Response].
//
// # Resource Management
//
// Caller MUST call resp.Close() when done to return buffers to the pool.
func (c *Client) Patch(
	ctx context.Context,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (aoni.Response, error) {
	return c.executeFast(ctx, http.MethodPatch, path, body, mods)
}

// Delete executes a fast HTTP DELETE request and returns the raw [aoni.Response].
//
// # Resource Management
//
// Caller MUST call resp.Close() when done to return buffers to the pool.
func (c *Client) Delete(ctx context.Context, path string, mods ...aoni.RequestModifier) (aoni.Response, error) {
	return c.executeFast(ctx, http.MethodDelete, path, nil, mods)
}

// Fetch executes an arbitrary raw HTTP method request on the fast engine and returns the raw [aoni.Response].
//
// # Resource Management
//
// Caller MUST call resp.Close() when done to return buffers to the pool.
func (c *Client) Fetch(
	ctx context.Context,
	method, path string,
	body any,
	mods ...aoni.RequestModifier,
) (aoni.Response, error) {
	return c.executeFast(ctx, method, path, body, mods)
}

// --- Generic Typed HTTP Methods on *Client ---

// GetTo executes a HTTP GET request on the fast engine and decodes the response into *Resp.
//
// Automatically releases pooled buffers upon completion.
//
// # Example
//
//	user, err := fastClient.GetTo[User](ctx, "/users/42")
func (c *Client) GetTo[Resp any](ctx context.Context, path string, mods ...aoni.RequestModifier) (*Resp, error) {
	return c.FetchTo[Resp](ctx, http.MethodGet, path, nil, mods...)
}

// GetInto executes a fast HTTP GET request and decodes the payload directly into target with 0 heap allocations.
//
// # Example
//
//	var user User
//	err := fastClient.GetInto(ctx, "/users/42", &user)
func (c *Client) GetInto[Resp any](ctx context.Context, path string, target *Resp, mods ...aoni.RequestModifier) error {
	return c.FetchInto[Resp](ctx, http.MethodGet, path, nil, target, mods...)
}

// PostTo executes a fast HTTP POST request carrying body and decodes the response into *Resp.
//
// # Example
//
//	created, err := fastClient.PostTo[User](ctx, "/users", CreateUserReq{Name: "Alice"})
func (c *Client) PostTo[Resp any](
	ctx context.Context,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	return c.FetchTo[Resp](ctx, http.MethodPost, path, body, mods...)
}

// PostInto executes a fast HTTP POST request and decodes response directly into target with 0 heap allocations.
func (c *Client) PostInto[Resp any](
	ctx context.Context,
	path string,
	body any,
	target *Resp,
	mods ...aoni.RequestModifier,
) error {
	return c.FetchInto[Resp](ctx, http.MethodPost, path, body, target, mods...)
}

// PutTo executes a fast HTTP PUT request carrying body and decodes the response into *Resp.
func (c *Client) PutTo[Resp any](
	ctx context.Context,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	return c.FetchTo[Resp](ctx, http.MethodPut, path, body, mods...)
}

// PutInto executes a fast HTTP PUT request and decodes response directly into target with 0 heap allocations.
func (c *Client) PutInto[Resp any](
	ctx context.Context,
	path string,
	body any,
	target *Resp,
	mods ...aoni.RequestModifier,
) error {
	return c.FetchInto[Resp](ctx, http.MethodPut, path, body, target, mods...)
}

// PatchTo executes a fast HTTP PATCH request carrying body and decodes the response into *Resp.
func (c *Client) PatchTo[Resp any](
	ctx context.Context,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	return c.FetchTo[Resp](ctx, http.MethodPatch, path, body, mods...)
}

// PatchInto executes a fast HTTP PATCH request and decodes response directly into target with 0 heap allocations.
func (c *Client) PatchInto[Resp any](
	ctx context.Context,
	path string,
	body any,
	target *Resp,
	mods ...aoni.RequestModifier,
) error {
	return c.FetchInto[Resp](ctx, http.MethodPatch, path, body, target, mods...)
}

// DeleteTo executes a fast HTTP DELETE request and decodes any returned payload into *Resp.
func (c *Client) DeleteTo[Resp any](ctx context.Context, path string, mods ...aoni.RequestModifier) (*Resp, error) {
	return c.FetchTo[Resp](ctx, http.MethodDelete, path, nil, mods...)
}

// DeleteInto executes a fast HTTP DELETE request and decodes response directly into target with 0 heap allocations.
func (c *Client) DeleteInto[Resp any](
	ctx context.Context,
	path string,
	target *Resp,
	mods ...aoni.RequestModifier,
) error {
	return c.FetchInto[Resp](ctx, http.MethodDelete, path, nil, target, mods...)
}

// FetchTo executes an arbitrary HTTP method request on the fast engine and decodes the response into *Resp.
func (c *Client) FetchTo[Resp any](
	ctx context.Context,
	method, path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	resp, err := c.executeFast(ctx, method, path, body, mods)
	if err != nil {
		return nil, err
	}
	defer resp.Close()

	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return nil, &aoni.APIError{StatusCode: resp.StatusCode(), Body: resp.BodyBytes()}
	}

	result := new(Resp)
	if resp.StatusCode() == http.StatusNoContent || resp.StatusCode() == http.StatusResetContent ||
		len(resp.UnsafeBodyBytes()) == 0 {
		return result, nil
	}

	if err := decode.Payload(resp.Header("Content-Type"), resp.UnsafeBodyBytes(), result); err != nil {
		return nil, err
	}

	return result, nil
}

// FetchInto executes an arbitrary HTTP method request and decodes response directly into target with 0 heap allocations.
func (c *Client) FetchInto[Resp any](
	ctx context.Context,
	method, path string,
	body any,
	target *Resp,
	mods ...aoni.RequestModifier,
) error {
	resp, err := c.executeFast(ctx, method, path, body, mods)
	if err != nil {
		return err
	}
	defer resp.Close()

	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return &aoni.APIError{StatusCode: resp.StatusCode(), Body: resp.BodyBytes()}
	}

	if target == nil || resp.StatusCode() == http.StatusNoContent || resp.StatusCode() == http.StatusResetContent ||
		len(resp.UnsafeBodyBytes()) == 0 {
		return nil
	}

	return decode.Payload(resp.Header("Content-Type"), resp.UnsafeBodyBytes(), target)
}

// DoInto is an alias for [Client.FetchInto].
func (c *Client) DoInto[Resp any](
	ctx context.Context,
	method, path string,
	body any,
	target *Resp,
	mods ...aoni.RequestModifier,
) error {
	return c.FetchInto[Resp](ctx, method, path, body, target, mods...)
}

func (c *Client) executeFast(
	ctx context.Context,
	method, path string,
	body any,
	mods []aoni.RequestModifier,
) (aoni.Response, error) {
	if body == nil {
		return c.requestInternal(ctx, method, path, nil, mods)
	}

	var bodyMod aoni.RequestModifier
	switch b := body.(type) {
	case aoni.RequestModifier:
		bodyMod = b
	case []byte:
		bodyMod = mod.WithBodyBytes(b)
	case string:
		bodyMod = mod.WithBodyBytes([]byte(b))
	default:
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyMod = mod.WithBodyBytes(data)
	}

	if bodyMod.Kind != 0 || bodyMod.Fn != nil {
		return c.requestInternal(ctx, method, path, &bodyMod, mods)
	}

	return c.requestInternal(ctx, method, path, nil, mods)
}
