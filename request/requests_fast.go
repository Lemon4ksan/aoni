// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package request

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/decode"
)

// DoFast executes a fast HTTP request over doer, bypassing *http.Response and http.Header allocations
// and returning a pooled aoni.Response directly.
//
// Postconditions:
//   - Callers MUST close the returned response via resp.Close() to release pooled memory.
func DoFast(
	ctx context.Context,
	doer aoni.RequestDoer,
	method, path string,
	body any,
	mods ...aoni.RequestModifier,
) (aoni.Response, error) {
	if doer == nil {
		doer = DefaultClient
	}

	req, release := acquireRequestFromDoer(doer)
	defer release()

	req.SetContext(ctx)
	req.SetMethod(method)
	req.SetURL(path)

	if body != nil {
		if err := applyFastBody(req, body); err != nil {
			return nil, err
		}
	}

	for _, m := range mods {
		if m != nil {
			m(req)
		}
	}

	return doer.Do(req)
}

// DoToFast executes a fast HTTP request and decodes the response payload directly into Resp
// without creating intermediate *http.Response structures.
func DoToFast[Resp any](
	ctx context.Context,
	doer aoni.RequestDoer,
	method, path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	resp, err := DoFast(ctx, doer, method, path, body, mods...)
	if err != nil {
		return nil, err
	}
	defer resp.Close()

	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return nil, &aoni.APIError{StatusCode: resp.StatusCode(), Body: resp.BodyBytes()}
	}

	result := new(Resp)
	contentType := resp.Header("Content-Type")
	decoder := decode.LookupDecoder(contentType)

	if err := decoder.Decode(bytes.NewReader(resp.UnsafeBodyBytes()), result); err != nil {
		return nil, err
	}

	return result, nil
}

// DoIntoFast executes a fast HTTP request and decodes the response payload directly into target without allocations.
func DoIntoFast[T any](
	ctx context.Context,
	doer aoni.RequestDoer,
	method, path string,
	body any,
	target *T,
	mods ...aoni.RequestModifier,
) error {
	resp, err := DoFast(ctx, doer, method, path, body, mods...)
	if err != nil {
		return err
	}
	defer resp.Close()

	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return &aoni.APIError{StatusCode: resp.StatusCode(), Body: resp.BodyBytes()}
	}

	contentType := resp.Header("Content-Type")
	decoder := decode.LookupDecoder(contentType)

	return decoder.Decode(bytes.NewReader(resp.UnsafeBodyBytes()), target)
}

// GetFast executes a fast GET request, returning a pooled aoni.Response.
func GetFast(
	ctx context.Context,
	doer aoni.RequestDoer,
	path string,
	mods ...aoni.RequestModifier,
) (aoni.Response, error) {
	return DoFast(ctx, doer, http.MethodGet, path, nil, mods...)
}

// GetToFast executes a fast GET request and decodes the response into a newly allocated Resp structure.
func GetToFast[Resp any](
	ctx context.Context,
	doer aoni.RequestDoer,
	path string,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	return DoToFast[Resp](ctx, doer, http.MethodGet, path, nil, mods...)
}

// GetIntoFast executes a fast GET request and decodes the response directly into target.
func GetIntoFast[T any](
	ctx context.Context,
	doer aoni.RequestDoer,
	path string,
	target *T,
	mods ...aoni.RequestModifier,
) error {
	return DoIntoFast[T](ctx, doer, http.MethodGet, path, nil, target, mods...)
}

// PostFast executes a fast POST request with body payload, returning a pooled aoni.Response.
func PostFast(
	ctx context.Context,
	doer aoni.RequestDoer,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (aoni.Response, error) {
	return DoFast(ctx, doer, http.MethodPost, path, body, mods...)
}

// PostToFast executes a fast POST request with body payload and decodes the response into Resp.
func PostToFast[Resp any](
	ctx context.Context,
	doer aoni.RequestDoer,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	return DoToFast[Resp](ctx, doer, http.MethodPost, path, body, mods...)
}

// PostIntoFast executes a fast POST request with body payload and decodes the response directly into target.
func PostIntoFast[T any](
	ctx context.Context,
	doer aoni.RequestDoer,
	path string,
	body any,
	target *T,
	mods ...aoni.RequestModifier,
) error {
	return DoIntoFast[T](ctx, doer, http.MethodPost, path, body, target, mods...)
}

// PutFast executes a fast PUT request with body payload, returning a pooled aoni.Response.
func PutFast(
	ctx context.Context,
	doer aoni.RequestDoer,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (aoni.Response, error) {
	return DoFast(ctx, doer, http.MethodPut, path, body, mods...)
}

// PutToFast executes a fast PUT request with body payload and decodes the response into Resp.
func PutToFast[Resp any](
	ctx context.Context,
	doer aoni.RequestDoer,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	return DoToFast[Resp](ctx, doer, http.MethodPut, path, body, mods...)
}

// PutIntoFast executes a fast PUT request with body payload and decodes the response directly into target.
func PutIntoFast[T any](
	ctx context.Context,
	doer aoni.RequestDoer,
	path string,
	body any,
	target *T,
	mods ...aoni.RequestModifier,
) error {
	return DoIntoFast[T](ctx, doer, http.MethodPut, path, body, target, mods...)
}

// PatchFast executes a fast PATCH request with body payload, returning a pooled aoni.Response.
func PatchFast(
	ctx context.Context,
	doer aoni.RequestDoer,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (aoni.Response, error) {
	return DoFast(ctx, doer, http.MethodPatch, path, body, mods...)
}

// PatchToFast executes a fast PATCH request with body payload and decodes the response into Resp.
func PatchToFast[Resp any](
	ctx context.Context,
	doer aoni.RequestDoer,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	return DoToFast[Resp](ctx, doer, http.MethodPatch, path, body, mods...)
}

// PatchIntoFast executes a fast PATCH request with body payload and decodes the response directly into target.
func PatchIntoFast[T any](
	ctx context.Context,
	doer aoni.RequestDoer,
	path string,
	body any,
	target *T,
	mods ...aoni.RequestModifier,
) error {
	return DoIntoFast[T](ctx, doer, http.MethodPatch, path, body, target, mods...)
}

// DeleteFast executes a fast DELETE request, returning a pooled aoni.Response.
func DeleteFast(
	ctx context.Context,
	doer aoni.RequestDoer,
	path string,
	mods ...aoni.RequestModifier,
) (aoni.Response, error) {
	return DoFast(ctx, doer, http.MethodDelete, path, nil, mods...)
}

// DeleteToFast executes a fast DELETE request and decodes the response into Resp.
func DeleteToFast[Resp any](
	ctx context.Context,
	doer aoni.RequestDoer,
	path string,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	return DoToFast[Resp](ctx, doer, http.MethodDelete, path, nil, mods...)
}

// DeleteIntoFast executes a fast DELETE request and decodes the response directly into target.
func DeleteIntoFast[T any](
	ctx context.Context,
	doer aoni.RequestDoer,
	path string,
	target *T,
	mods ...aoni.RequestModifier,
) error {
	return DoIntoFast[T](ctx, doer, http.MethodDelete, path, nil, target, mods...)
}

// HeadFast executes a fast HEAD request, returning a pooled aoni.Response.
func HeadFast(
	ctx context.Context,
	doer aoni.RequestDoer,
	path string,
	mods ...aoni.RequestModifier,
) (aoni.Response, error) {
	return DoFast(ctx, doer, http.MethodHead, path, nil, mods...)
}

// OptionsFast executes a fast OPTIONS request, returning a pooled aoni.Response.
func OptionsFast(
	ctx context.Context,
	doer aoni.RequestDoer,
	path string,
	mods ...aoni.RequestModifier,
) (aoni.Response, error) {
	return DoFast(ctx, doer, http.MethodOptions, path, nil, mods...)
}

func acquireRequestFromDoer(doer aoni.RequestDoer) (aoni.Request, func()) {
	if factory, ok := doer.(aoni.RequestFactory); ok {
		r := factory.AcquireRequest()
		return r, func() { factory.ReleaseRequest(r) }
	}

	stdReq := aoni.NewStdRequest(nil)

	return stdReq, func() {}
}

func applyFastBody(req aoni.Request, body any) error {
	if b, ok := body.([]byte); ok {
		req.SetBodyBytes(b)
		return nil
	}

	if s, ok := body.(string); ok {
		req.SetBodyBytes([]byte(s))
		return nil
	}

	if msg, ok := body.(proto.Message); ok {
		bodyBytes, err := proto.Marshal(msg)
		if err != nil {
			return fmt.Errorf("aoni request: failed to marshal proto payload: %w", err)
		}

		req.SetBodyBytes(bodyBytes)

		if req.Header("Content-Type") == "" {
			req.SetHeader("Content-Type", "application/x-protobuf")
		}

		return nil
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("aoni request: failed to marshal payload: %w", err)
	}

	req.SetBodyBytes(bodyBytes)

	if req.Header("Content-Type") == "" {
		req.SetHeader("Content-Type", "application/json")
	}

	return nil
}
