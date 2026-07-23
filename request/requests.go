// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package request provides type-safe, generic-first helper functions for executing HTTP transactions.
//
// It abstracts away boilerplate request construction, response decoding, and status validation:
//   - [GetTo], [PostTo], [PutTo], [PatchTo], [DeleteTo]: Execute requests and decode JSON/XML/YAML
//     response bodies into strongly-typed target structs [T].
//   - [Concurrent], [ConcurrentWithMods]: Execute fan-out requests concurrently across multiple paths,
//     preserving original slice ordering and context deadlines.
package request

import (
	"context"
	"net/http"
	"reflect"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/mod"
)

// DefaultClient is the shared default client instance used by global helper functions.
var DefaultClient = aoni.NewClient(nil)

// NoResponse is a sentinel type used to indicate a request that does not return a response body.
// When used as the response type in generic request helpers like [GetTo],
// the helper automatically drains and closes the response body to prevent resource leaks.
type NoResponse struct{}

// Unwrapper allows nested decorators to be peeled away to reach the
// underlying [Requester]. [Client] does not implement this interface;
// wrapper types returned by [NewStdClient] or [Chain] do.
type Unwrapper interface {
	Unwrap() Requester
}

// UnwrapClient strips all [Unwrapper] layers from r and returns the
// innermost [Client]. Returns nil if r is not a *Client and no
// Unwrapper chain leads to one.
func UnwrapClient(r Requester) (c *aoni.Client) {
	for {
		if client, ok := r.(*aoni.Client); ok {
			return client
		}

		u, ok := r.(Unwrapper)
		if !ok {
			break
		}

		r = u.Unwrap()
	}

	return nil
}

// Requester specifies a high-level API client capable of executing parameterized requests.
//
// The pipeline handles base URL resolution, request parameter mapping, WAF challenge
// solving, and automatic decompression.
type Requester interface {
	Request(
		ctx context.Context,
		method, path string,
		mods ...aoni.RequestModifier,
	) (*http.Response, error)
}

// Get performs a GET request through the specified [Requester] and returns the raw [http.Response].
func Get(ctx context.Context, c Requester, path string, mods ...aoni.RequestModifier) (*http.Response, error) {
	return c.Request(ctx, http.MethodGet, path, mods...)
}

// GetTo performs a GET request and decodes the response body into a new instance of Resp.
// It returns an [APIError] if the server responds with a non-2xx status code.
//
// By default, the response is parsed as JSON. To decode other response formats (such as XML
// or YAML), pass a corresponding decoder modifier, e.g. [WithXMLDecoder] or [WithYAMLDecoder].
func GetTo[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	resp, err := c.Request(ctx, http.MethodGet, path, mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		return nil, HandleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := HandleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// GetToEx is like [GetTo] but returns both the parsed response payload and the raw *http.Response.
func GetToEx[Resp any](
	ctx context.Context,
	c Requester,
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

// GetProtoTo executes a GET request expecting a binary Protobuf response and decodes it into Resp.
func GetProtoTo[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	mods = append([]aoni.RequestModifier{
		mod.WithHeader("Accept", "application/x-protobuf"),
		mod.WithDecoder(decode.ProtoDecoder),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodGet, path, mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		return nil, HandleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := HandleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
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
	c Requester,
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
	c Requester,
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
		return nil, HandleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := HandleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// PostToEx is like [PostTo] but returns both the parsed response payload and the raw *http.Response.
func PostToEx[Resp any](
	ctx context.Context,
	c Requester,
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

// PostProto executes a POST request containing a Protobuf payload and returns the raw *http.Response.
func PostProto(
	ctx context.Context,
	c Requester,
	path string,
	msg proto.Message,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	mods = append([]aoni.RequestModifier{mod.WithProtoBody(msg)}, mods...)
	return c.Request(ctx, http.MethodPost, path, mods...)
}

// PostProtoTo executes a POST request with a Protobuf payload and decodes the response into Resp.
func PostProtoTo[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	msg proto.Message,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	mods = append([]aoni.RequestModifier{
		mod.WithProtoBody(msg),
		mod.WithDecoder(decode.ProtoDecoder),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodPost, path, mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		return nil, HandleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := HandleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// PostGRPCWeb executes a POST request with a gRPC-Web framed Protobuf payload and returns the raw *http.Response.
func PostGRPCWeb(
	ctx context.Context,
	c Requester,
	path string,
	msg proto.Message,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	mods = append([]aoni.RequestModifier{mod.WithGRPCWebBody(msg)}, mods...)
	return c.Request(ctx, http.MethodPost, path, mods...)
}

// PostGRPCWebTo executes a POST request with a gRPC-Web framed payload and decodes the response frame into Resp.
func PostGRPCWebTo[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	msg proto.Message,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	mods = append([]aoni.RequestModifier{
		mod.WithGRPCWebBody(msg),
		mod.WithDecoder(decode.GRPCWebDecoder),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodPost, path, mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		return nil, HandleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := HandleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
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
	c Requester,
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
	c Requester,
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
		return nil, HandleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := HandleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// PutToEx is like [PutTo] but returns both the parsed response payload and the raw *http.Response.
func PutToEx[Resp any](
	ctx context.Context,
	c Requester,
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

// PutProtoTo executes a PUT request with a Protobuf payload and decodes the response into Resp.
func PutProtoTo[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	msg proto.Message,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	return DoProtoTo[Resp](ctx, c, http.MethodPut, path, msg, mods...)
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
	c Requester,
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
	c Requester,
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
		return nil, HandleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := HandleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// PatchToEx is like [PatchTo] but returns both the parsed response payload and the raw *http.Response.
func PatchToEx[Resp any](
	ctx context.Context,
	c Requester,
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

// PatchProtoTo executes a PATCH request with a Protobuf payload and decodes the response into Resp.
func PatchProtoTo[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	msg proto.Message,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	return DoProtoTo[Resp](ctx, c, http.MethodPatch, path, msg, mods...)
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
	c Requester,
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
	c Requester,
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
		return nil, HandleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := HandleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// DeleteToEx is like [DeleteTo] but returns both the parsed response payload and the raw *http.Response.
func DeleteToEx[Resp any](
	ctx context.Context,
	c Requester,
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

// DeleteProtoTo executes a DELETE request with an optional Protobuf payload and decodes the response into Resp.
func DeleteProtoTo[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	msg proto.Message,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	return DoProtoTo[Resp](ctx, c, http.MethodDelete, path, msg, mods...)
}

// Head performs a HEAD request through the specified [Requester] and returns the raw [http.Response].
func Head(
	ctx context.Context,
	c Requester,
	path string,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	return c.Request(ctx, http.MethodHead, path, mods...)
}

// Options performs an OPTIONS request through the specified [Requester] and returns the raw [http.Response].
func Options(
	ctx context.Context,
	c Requester,
	path string,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	return c.Request(ctx, http.MethodOptions, path, mods...)
}

// OptionsTo performs an OPTIONS request and decodes the response body into Resp.
func OptionsTo[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	resp, err := c.Request(ctx, http.MethodOptions, path, mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		return nil, HandleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := HandleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// Trace performs a TRACE request through the specified [Requester] and returns the raw [http.Response].
func Trace(
	ctx context.Context,
	c Requester,
	path string,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	return c.Request(ctx, http.MethodTrace, path, mods...)
}

// Connect performs a CONNECT request through the specified [Requester] and returns the raw [http.Response].
func Connect(
	ctx context.Context,
	c Requester,
	path string,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	return c.Request(ctx, http.MethodConnect, path, mods...)
}

// Do performs an arbitrary HTTP request using method and optional body, returning the raw [http.Response].
func Do(
	ctx context.Context,
	c Requester,
	method, path string,
	body any,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	if body != nil {
		bodyReader, err := validateAndMarshal(body)
		if err != nil {
			return nil, err
		}

		mods = append([]aoni.RequestModifier{
			mod.WithContentType("application/json"),
			mod.WithAccept("application/json"),
			mod.WithBody(bodyReader),
		}, mods...)
	}

	return c.Request(ctx, method, path, mods...)
}

// DoTo performs an arbitrary HTTP request using method, marshals optional body, and decodes response into Resp.
func DoTo[Resp any](
	ctx context.Context,
	c Requester,
	method, path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	if body != nil {
		bodyReader, err := validateAndMarshal(body)
		if err != nil {
			return nil, err
		}

		mods = append([]aoni.RequestModifier{
			mod.WithContentType("application/json"),
			mod.WithAccept("application/json"),
			mod.WithBody(bodyReader),
		}, mods...)
	}

	resp, err := c.Request(ctx, method, path, mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		return nil, HandleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := HandleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// DoToEx is like [DoTo] but returns both the parsed response payload and the raw *http.Response.
func DoToEx[Resp any](
	ctx context.Context,
	c Requester,
	method, path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, *http.Response, error) {
	var raw *http.Response

	mods = append(mods, mod.WithCaptureResponse(&raw))

	result, err := DoTo[Resp](ctx, c, method, path, body, mods...)
	if err != nil {
		if raw != nil && raw.Body != nil {
			_ = raw.Body.Close()
		}

		return nil, raw, err
	}

	return result, raw, nil
}

// DoProtoTo executes an HTTP request using any method with a Protobuf payload and decodes the response into Resp.
func DoProtoTo[Resp any](
	ctx context.Context,
	c Requester,
	method, path string,
	msg proto.Message,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	if msg != nil {
		mods = append([]aoni.RequestModifier{mod.WithProtoBody(msg)}, mods...)
	} else {
		mods = append([]aoni.RequestModifier{mod.WithHeader("Accept", "application/x-protobuf")}, mods...)
	}

	mods = append(mods, mod.WithDecoder(decode.ProtoDecoder))

	resp, err := c.Request(ctx, method, path, mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		return nil, HandleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := HandleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}
