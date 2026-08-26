// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"google.golang.org/protobuf/proto"

	fheader "github.com/lemon4ksan/foundation/net/http/header"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/grpc"
	"github.com/lemon4ksan/aoni/mod"
)

// FastGRPCClient provides native, high-performance gRPC operations directly on [Client].
type FastGRPCClient struct {
	client *Client
}

// GRPC yields the gRPC sub-client bound to this fast client instance.
func (c *Client) GRPC() *FastGRPCClient {
	return &FastGRPCClient{client: c}
}

// R returns a pooled [aoni.RequestBuilder] bound to the fast engine adapter.
func (c *Client) R() *aoni.RequestBuilder {
	return aoni.NewClient(c).R()
}

// NewRequest returns a pooled [aoni.RequestBuilder] bound to the fast engine adapter.
func (c *Client) NewRequest() *aoni.RequestBuilder {
	return c.R()
}

// Invoke executes a high-performance gRPC unary call using the fast client engine without allocating *http.Response.
func (g *FastGRPCClient) Invoke[Resp any](
	ctx context.Context,
	fullMethod string,
	reqMsg proto.Message,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	frameBytes, err := grpc.MarshalFrame(reqMsg, false)
	if err != nil {
		return nil, err
	}

	path := fullMethod
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	req := NewRequest(nil)
	defer req.Release()

	req.SetContext(ctx)
	req.SetMethod(http.MethodPost)
	req.SetURL(path)
	req.SetHeader(fheader.ContentType, fheader.MIMEApplicationGRPC)
	req.SetHeader(fheader.TE, fheader.ValueTrailers)
	req.SetBodyBytes(frameBytes)

	mod.Apply(req, mods...)

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Close()

	result := new(Resp)
	msg, ok := any(result).(proto.Message)
	if !ok {
		return nil, fmt.Errorf("aoni/fast: response type %T does not implement proto.Message", result)
	}

	if _, err := grpc.UnmarshalFrame(bytes.NewReader(resp.BodyBytes()), msg); err != nil {
		return nil, err
	}

	return result, nil
}

// Get performs a fast HTTP GET request and decodes the response payload directly into *Resp.
func (c *Client) Get[Resp any](ctx context.Context, path string, mods ...aoni.RequestModifier) (*Resp, error) {
	return c.Fetch[Resp](ctx, http.MethodGet, path, nil, mods...)
}

// GetInto performs a fast HTTP GET request and decodes the response payload directly into target without allocations.
func (c *Client) GetInto[Resp any](ctx context.Context, path string, target *Resp, mods ...aoni.RequestModifier) error {
	return c.FetchInto[Resp](ctx, http.MethodGet, path, nil, target, mods...)
}

// Post performs a fast HTTP POST request with payload body and decodes response into *Resp.
func (c *Client) Post[Resp any](ctx context.Context, path string, body any, mods ...aoni.RequestModifier) (*Resp, error) {
	return c.Fetch[Resp](ctx, http.MethodPost, path, body, mods...)
}

// PostInto performs a fast HTTP POST request with payload body and decodes response directly into target without allocations.
func (c *Client) PostInto[Resp any](ctx context.Context, path string, body any, target *Resp, mods ...aoni.RequestModifier) error {
	return c.FetchInto[Resp](ctx, http.MethodPost, path, body, target, mods...)
}

// Put performs a fast HTTP PUT request with payload body and decodes response into *Resp.
func (c *Client) Put[Resp any](ctx context.Context, path string, body any, mods ...aoni.RequestModifier) (*Resp, error) {
	return c.Fetch[Resp](ctx, http.MethodPut, path, body, mods...)
}

// PutInto performs a fast HTTP PUT request with payload body and decodes response directly into target without allocations.
func (c *Client) PutInto[Resp any](ctx context.Context, path string, body any, target *Resp, mods ...aoni.RequestModifier) error {
	return c.FetchInto[Resp](ctx, http.MethodPut, path, body, target, mods...)
}

// Patch performs a fast HTTP PATCH request with payload body and decodes response into *Resp.
func (c *Client) Patch[Resp any](ctx context.Context, path string, body any, mods ...aoni.RequestModifier) (*Resp, error) {
	return c.Fetch[Resp](ctx, http.MethodPatch, path, body, mods...)
}

// PatchInto performs a fast HTTP PATCH request with payload body and decodes response directly into target without allocations.
func (c *Client) PatchInto[Resp any](ctx context.Context, path string, body any, target *Resp, mods ...aoni.RequestModifier) error {
	return c.FetchInto[Resp](ctx, http.MethodPatch, path, body, target, mods...)
}

// Delete performs a fast HTTP DELETE request and decodes response into *Resp.
func (c *Client) Delete[Resp any](ctx context.Context, path string, mods ...aoni.RequestModifier) (*Resp, error) {
	return c.Fetch[Resp](ctx, http.MethodDelete, path, nil, mods...)
}

// DeleteInto performs a fast HTTP DELETE request and decodes response directly into target without allocations.
func (c *Client) DeleteInto[Resp any](ctx context.Context, path string, target *Resp, mods ...aoni.RequestModifier) error {
	return c.FetchInto[Resp](ctx, http.MethodDelete, path, nil, target, mods...)
}

// Fetch performs an HTTP request on the fast engine and decodes response into *Resp.
func (c *Client) Fetch[Resp any](
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
	if err := decode.Payload(resp.Header("Content-Type"), resp.UnsafeBodyBytes(), result); err != nil {
		return nil, err
	}

	return result, nil
}

// FetchInto performs an HTTP request on the fast engine and decodes response directly into target without allocations.
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

	return decode.Payload(resp.Header("Content-Type"), resp.UnsafeBodyBytes(), target)
}

// DoInto is an alias for FetchInto.
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
	if body != nil {
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
			allMods := make([]aoni.RequestModifier, 0, len(mods)+1)
			allMods = append(allMods, bodyMod)
			allMods = append(allMods, mods...)
			return c.Request(ctx, method, path, allMods...)
		}
	}

	return c.Request(ctx, method, path, mods...)
}
