// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package request

import (
	"context"
	"net/http"
	"reflect"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
)

// DefaultClient is the shared default client instance used by global helper functions.
var DefaultClient = aoni.NewClient(nil)

// NoResponse is a sentinel type used to indicate a request that does not return a response body.
// When used as the response type in generic request helpers like [GetTo],
// the helper automatically drains and closes the response body to prevent resource leaks.
type NoResponse struct{}

// Get performs a GET request through the specified [Requester] and returns the raw [http.Response].
func Get(ctx context.Context, c aoni.Requester, path string, mods ...aoni.RequestModifier) (*http.Response, error) {
	return c.Request(ctx, http.MethodGet, path, mods...)
}

// GetTo performs a GET request and decodes the response body into a new instance of Resp.
// It returns an [APIError] if the server responds with a non-2xx status code.
//
// By default, the response is parsed as JSON. To decode other response formats (such as XML
// or YAML), pass a corresponding decoder modifier, e.g. [WithXMLDecoder] or [WithYAMLDecoder].
func GetTo[Resp any](
	ctx context.Context,
	c aoni.Requester,
	path string,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	resp, err := c.Request(ctx, http.MethodGet, path, mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		return nil, handleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := handleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// GetToEx is like [GetTo] but returns both the parsed response payload and the raw *http.Response.
func GetToEx[Resp any](
	ctx context.Context,
	c aoni.Requester,
	path string,
	mods ...aoni.RequestModifier,
) (*Resp, *http.Response, error) {
	var raw *http.Response

	mods = append(mods, mod.WithCaptureResponse(&raw))

	result, err := GetTo[Resp](ctx, c, path, mods...)
	if err != nil {
		if raw != nil && raw.Body != nil {
			_ = raw.Body.Close()
		}

		return nil, raw, err
	}

	return result, raw, nil
}

// Post executes a POST request through the specified [Requester] and returns the raw [http.Response].
//
// By default, if the body is a struct or map, it is marshaled to JSON and the request headers
// "Content-Type" and "Accept" are set to "application/json".
//
// To send other body formats (e.g. XML, YAML, or plain text), pre-serialize the payload and
// pass it as an [io.Reader] (e.g. using [strings.NewReader] or [bytes.NewReader]), then override the Content-Type
// header using request modifiers like [WithContentType] (e.g. WithContentType("application/xml")).
//
// Use [WithFormBody] or [WithFormValues] to create PostForm requests.
func Post(
	ctx context.Context,
	c aoni.Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return nil, err
	}

	mods = append([]aoni.RequestModifier{
		mod.WithContentType("application/json"),
		mod.WithAccept("application/json"),
		mod.WithBody(bodyReader),
	}, mods...)

	return c.Request(ctx, http.MethodPost, path, mods...)
}

// PostTo executes a POST request, marshals the body, and decodes the response body into Resp.
// It returns an [APIError] if the server responds with a non-2xx status code.
//
// By default, the request body is marshaled to JSON and the response is parsed as JSON.
//
// To send other body formats, pre-serialize the payload and pass it as an [io.Reader] (e.g. [strings.NewReader]),
// then override the Content-Type header using [WithContentType].
// To decode other response formats (such as XML or YAML), pass a decoder modifier, e.g. [WithXMLDecoder] or [WithYAMLDecoder].
//
// Use [WithFormBody] or [WithFormValues] to create PostForm requests.
func PostTo[Resp any](
	ctx context.Context,
	c aoni.Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return nil, err
	}

	mods = append([]aoni.RequestModifier{
		mod.WithContentType("application/json"),
		mod.WithAccept("application/json"),
		mod.WithBody(bodyReader),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodPost, path, mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		return nil, handleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := handleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// PostToEx is like [PostTo] but returns both the parsed response payload and the raw *http.Response.
func PostToEx[Resp any](
	ctx context.Context,
	c aoni.Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, *http.Response, error) {
	var raw *http.Response

	mods = append(mods, mod.WithCaptureResponse(&raw))

	result, err := PostTo[Resp](ctx, c, path, body, mods...)
	if err != nil {
		if raw != nil && raw.Body != nil {
			_ = raw.Body.Close()
		}

		return nil, raw, err
	}

	return result, raw, nil
}

// Put executes a PUT request through the specified [Requester] and returns the raw [http.Response].
//
// By default, if the body is a struct or map, it is marshaled to JSON and the request headers
// "Content-Type" and "Accept" are set to "application/json".
//
// To send other body formats (e.g. XML, YAML, or plain text), pre-serialize the payload and
// pass it as an [io.Reader] (e.g. using [strings.NewReader] or [bytes.NewReader]), then override the Content-Type
// header using request modifiers like [WithContentType] (e.g. WithContentType("application/xml")).
func Put(
	ctx context.Context,
	c aoni.Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return nil, err
	}

	mods = append([]aoni.RequestModifier{
		mod.WithContentType("application/json"),
		mod.WithAccept("application/json"),
		mod.WithBody(bodyReader),
	}, mods...)

	return c.Request(ctx, http.MethodPut, path, mods...)
}

// PutTo executes a PUT request through the specified [Requester],
// marshals the body, and decodes the response body into Resp.
//
// It returns an [APIError] if the server responds with a non-2xx status code.
//
// By default, the request body is marshaled to JSON and the response is parsed as JSON.
//
// To send other body formats, pre-serialize the payload and pass it as an [io.Reader] (e.g. [strings.NewReader]),
// then override the Content-Type header using [WithContentType].
// To decode other response formats (such as XML or YAML), pass a decoder modifier, e.g. [WithXMLDecoder] or [WithYAMLDecoder].
func PutTo[Resp any](
	ctx context.Context,
	c aoni.Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return nil, err
	}

	mods = append([]aoni.RequestModifier{
		mod.WithContentType("application/json"),
		mod.WithAccept("application/json"),
		mod.WithBody(bodyReader),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodPut, path, mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		return nil, handleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := handleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// PutToEx is like [PutTo] but returns both the parsed response payload and the raw *http.Response.
func PutToEx[Resp any](
	ctx context.Context,
	c aoni.Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, *http.Response, error) {
	var raw *http.Response

	mods = append(mods, mod.WithCaptureResponse(&raw))

	result, err := PutTo[Resp](ctx, c, path, body, mods...)
	if err != nil {
		if raw != nil && raw.Body != nil {
			_ = raw.Body.Close()
		}

		return nil, raw, err
	}

	return result, raw, nil
}

// Patch executes a PATCH request through the specified [Requester] and returns the raw [http.Response].
//
// By default, if the body is a struct or map, it is marshaled to JSON and the request headers
// "Content-Type" and "Accept" are set to "application/json".
//
// To send other body formats (e.g. XML, YAML, or plain text), pre-serialize the payload and
// pass it as an [io.Reader] (e.g. using [strings.NewReader] or [bytes.NewReader]), then override the Content-Type
// header using request modifiers like [WithContentType] (e.g. WithContentType("application/xml")).
func Patch(
	ctx context.Context,
	c aoni.Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return nil, err
	}

	mods = append([]aoni.RequestModifier{
		mod.WithContentType("application/json"),
		mod.WithAccept("application/json"),
		mod.WithBody(bodyReader),
	}, mods...)

	return c.Request(ctx, http.MethodPatch, path, mods...)
}

// PatchTo executes a PATCH request through the specified [Requester],
// marshals the body, and decodes the response body into Resp.
//
// It returns an [APIError] if the server responds with a non-2xx status code.
//
// By default, the request body is marshaled to JSON and the response is parsed as JSON.
//
// To send other body formats, pre-serialize the payload and pass it as an [io.Reader] (e.g. [strings.NewReader]),
// then override the Content-Type header using [WithContentType].
// To decode other response formats (such as XML or YAML), pass a decoder modifier, e.g. [WithXMLDecoder] or [WithYAMLDecoder].
func PatchTo[Resp any](
	ctx context.Context,
	c aoni.Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return nil, err
	}

	mods = append([]aoni.RequestModifier{
		mod.WithContentType("application/json"),
		mod.WithAccept("application/json"),
		mod.WithBody(bodyReader),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodPatch, path, mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		return nil, handleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := handleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// PatchToEx is like [PatchTo] but returns both the parsed response payload and the raw *http.Response.
func PatchToEx[Resp any](
	ctx context.Context,
	c aoni.Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, *http.Response, error) {
	var raw *http.Response

	mods = append(mods, mod.WithCaptureResponse(&raw))

	result, err := PatchTo[Resp](ctx, c, path, body, mods...)
	if err != nil {
		if raw != nil && raw.Body != nil {
			_ = raw.Body.Close()
		}

		return nil, raw, err
	}

	return result, raw, nil
}

// Delete executes a DELETE request through the specified [Requester] and returns the raw [http.Response].
//
// By default, if the body is a struct or map, it is marshaled to JSON and the request headers
// "Content-Type" and "Accept" are set to "application/json".
//
// To send other body formats (e.g. XML, YAML, or plain text), pre-serialize the payload and
// pass it as an [io.Reader] (e.g. using [strings.NewReader] or [bytes.NewReader]), then override the Content-Type
// header using request modifiers like [WithContentType] (e.g. WithContentType("application/xml")).
func Delete(
	ctx context.Context,
	c aoni.Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return nil, err
	}

	mods = append([]aoni.RequestModifier{
		mod.WithContentType("application/json"),
		mod.WithAccept("application/json"),
		mod.WithBody(bodyReader),
	}, mods...)

	return c.Request(ctx, http.MethodDelete, path, mods...)
}

// DeleteTo executes a DELETE request through the specified [Requester],
// marshals the body, and decodes the response body into Resp.
//
// It returns an [APIError] if the server responds with a non-2xx status code.
//
// By default, the request body is marshaled to JSON and the response is parsed as JSON.
//
// To send other body formats, pre-serialize the payload and pass it as an [io.Reader] (e.g. [strings.NewReader]),
// then override the Content-Type header using [WithContentType].
// To decode other response formats (such as XML or YAML), pass a decoder modifier, e.g. [WithXMLDecoder] or [WithYAMLDecoder].
func DeleteTo[Resp any](
	ctx context.Context,
	c aoni.Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return nil, err
	}

	mods = append([]aoni.RequestModifier{
		mod.WithContentType("application/json"),
		mod.WithAccept("application/json"),
		mod.WithBody(bodyReader),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodDelete, path, mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		return nil, handleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := handleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// DeleteToEx is like [DeleteTo] but returns both the parsed response payload and the raw *http.Response.
func DeleteToEx[Resp any](
	ctx context.Context,
	c aoni.Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, *http.Response, error) {
	var raw *http.Response

	mods = append(mods, mod.WithCaptureResponse(&raw))

	result, err := DeleteTo[Resp](ctx, c, path, body, mods...)
	if err != nil {
		if raw != nil && raw.Body != nil {
			_ = raw.Body.Close()
		}

		return nil, raw, err
	}

	return result, raw, nil
}
