// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httputil"
	"os"
	"reflect"
	"strings"
)

var sensitiveHeaders = map[string]bool{
	"authorization":       true,
	"cookie":              true,
	"set-cookie":          true,
	"proxy-authorization": true,
}

func redactHeaders(raw []byte) []byte {
	lines := strings.Split(string(raw), "\r\n")
	for i, line := range lines {
		for header := range sensitiveHeaders {
			prefix := header + ":"
			if strings.HasPrefix(strings.ToLower(line), prefix) {
				lines[i] = header + ": <redacted>"
				break
			}
		}
	}

	return []byte(strings.Join(lines, "\r\n"))
}

// DefaultClient is the shared default client instance used by global helper functions.
var DefaultClient = NewClient(nil)

// NoResponse is a sentinel type used to indicate a request that does not return a response body.
// When used as the response type in generic request helpers like [GetTo],
// the helper automatically drains and closes the response body to prevent resource leaks.
type NoResponse struct{}

// Put executes a PUT request through the specified Requester and returns the raw [http.Response].
//
// By default, if the body is a struct or map, it is marshaled to JSON and the request headers
// "Content-Type" and "Accept" are set to "application/json".
//
// To send other body formats (e.g. XML, YAML, or plain text), pre-serialize the payload and
// pass it as an [io.Reader] (e.g. using [strings.NewReader] or [bytes.NewReader]), then override the Content-Type
// header using request modifiers like [WithContentType] (e.g. WithContentType("application/xml")).
func Put(ctx context.Context, c Requester, path string, body any, mods ...RequestModifier) (*http.Response, error) {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return nil, err
	}

	mods = append([]RequestModifier{
		WithContentType("application/json"),
		WithAccept("application/json"),
		WithBody(bodyReader),
	}, mods...)

	return c.Request(ctx, http.MethodPut, path, mods...)
}

// Patch executes a PATCH request through the specified Requester and returns the raw [http.Response].
//
// By default, if the body is a struct or map, it is marshaled to JSON and the request headers
// "Content-Type" and "Accept" are set to "application/json".
//
// To send other body formats (e.g. XML, YAML, or plain text), pre-serialize the payload and
// pass it as an [io.Reader] (e.g. using [strings.NewReader] or [bytes.NewReader]), then override the Content-Type
// header using request modifiers like [WithContentType] (e.g. WithContentType("application/xml")).
func Patch(ctx context.Context, c Requester, path string, body any, mods ...RequestModifier) (*http.Response, error) {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return nil, err
	}

	mods = append([]RequestModifier{
		WithContentType("application/json"),
		WithAccept("application/json"),
		WithBody(bodyReader),
	}, mods...)

	return c.Request(ctx, http.MethodPatch, path, mods...)
}

// Delete executes a DELETE request through the specified Requester and returns the raw [http.Response].
//
// By default, if the body is a struct or map, it is marshaled to JSON and the request headers
// "Content-Type" and "Accept" are set to "application/json".
//
// To send other body formats (e.g. XML, YAML, or plain text), pre-serialize the payload and
// pass it as an [io.Reader] (e.g. using [strings.NewReader] or [bytes.NewReader]), then override the Content-Type
// header using request modifiers like [WithContentType] (e.g. WithContentType("application/xml")).
func Delete(ctx context.Context, c Requester, path string, body any, mods ...RequestModifier) (*http.Response, error) {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return nil, err
	}

	mods = append([]RequestModifier{
		WithContentType("application/json"),
		WithAccept("application/json"),
		WithBody(bodyReader),
	}, mods...)

	return c.Request(ctx, http.MethodDelete, path, mods...)
}

// Get performs a GET request through the specified Requester and returns the raw [http.Response].
func Get(ctx context.Context, c Requester, path string, mods ...RequestModifier) (*http.Response, error) {
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
	mods ...RequestModifier,
) (*Resp, error) {
	return requestTo[Resp](ctx, c, http.MethodGet, path, mods...)
}

// GetToEx is like [GetTo] but returns both the parsed response payload and the raw *http.Response.
func GetToEx[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	mods ...RequestModifier,
) (*Resp, *http.Response, error) {
	var raw *http.Response

	mods = append(mods, CaptureResponse(&raw))

	result, err := GetTo[Resp](ctx, c, path, mods...)
	if err != nil {
		return nil, raw, err
	}

	return result, raw, nil
}

// Post executes a POST request through the specified Requester and returns the raw [http.Response].
//
// By default, if the body is a struct or map, it is marshaled to JSON and the request headers
// "Content-Type" and "Accept" are set to "application/json".
//
// To send other body formats (e.g. XML, YAML, or plain text), pre-serialize the payload and
// pass it as an [io.Reader] (e.g. using [strings.NewReader] or [bytes.NewReader]), then override the Content-Type
// header using request modifiers like [WithContentType] (e.g. WithContentType("application/xml")).
//
// Use [WithFormBody] or [WithFormValues] to create PostForm requests.
func Post(ctx context.Context, c Requester, path string, body any, mods ...RequestModifier) (*http.Response, error) {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return nil, err
	}

	mods = append([]RequestModifier{
		WithContentType("application/json"),
		WithAccept("application/json"),
		WithBody(bodyReader),
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
	mods ...RequestModifier,
) (*Resp, error) {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return nil, err
	}

	mods = append([]RequestModifier{
		WithContentType("application/json"),
		WithAccept("application/json"),
		WithBody(bodyReader),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodPost, path, mods...)
	if err != nil {
		return nil, err
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		closeResponse(resp)

		return nil, err
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
	c Requester,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, *http.Response, error) {
	var raw *http.Response

	mods = append(mods, CaptureResponse(&raw))

	result, err := PostTo[Resp](ctx, c, path, body, mods...)
	if err != nil {
		return nil, raw, err
	}

	return result, raw, nil
}

// PutTo executes a PUT request, marshals the body, and decodes the response body into Resp.
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
	mods ...RequestModifier,
) (*Resp, error) {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return nil, err
	}

	mods = append([]RequestModifier{
		WithContentType("application/json"),
		WithAccept("application/json"),
		WithBody(bodyReader),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodPut, path, mods...)
	if err != nil {
		return nil, err
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		closeResponse(resp)
		return nil, err
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
	c Requester,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, *http.Response, error) {
	var raw *http.Response

	mods = append(mods, CaptureResponse(&raw))

	result, err := PutTo[Resp](ctx, c, path, body, mods...)
	if err != nil {
		return nil, raw, err
	}

	return result, raw, nil
}

// PatchTo executes a PATCH request, marshals the body, and decodes the response body into Resp.
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
	mods ...RequestModifier,
) (*Resp, error) {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return nil, err
	}

	mods = append([]RequestModifier{
		WithContentType("application/json"),
		WithAccept("application/json"),
		WithBody(bodyReader),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodPatch, path, mods...)
	if err != nil {
		return nil, err
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		closeResponse(resp)
		return nil, err
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
	c Requester,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, *http.Response, error) {
	var raw *http.Response

	mods = append(mods, CaptureResponse(&raw))

	result, err := PatchTo[Resp](ctx, c, path, body, mods...)
	if err != nil {
		return nil, raw, err
	}

	return result, raw, nil
}

// DeleteTo executes a DELETE request, marshals the body, and decodes the response body into Resp.
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
	mods ...RequestModifier,
) (*Resp, error) {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return nil, err
	}

	mods = append([]RequestModifier{
		WithContentType("application/json"),
		WithAccept("application/json"),
		WithBody(bodyReader),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodDelete, path, mods...)
	if err != nil {
		return nil, err
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		closeResponse(resp)
		return nil, err
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
	c Requester,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, *http.Response, error) {
	var raw *http.Response

	mods = append(mods, CaptureResponse(&raw))

	result, err := DeleteTo[Resp](ctx, c, path, body, mods...)
	if err != nil {
		return nil, raw, err
	}

	return result, raw, nil
}

func validateAndMarshal(payload any) (io.Reader, error) {
	if r, ok := payload.(io.Reader); ok {
		return r, nil
	}

	if payload == nil {
		return nil, nil
	}

	if err := Validate(payload); err != nil {
		return nil, err
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("aoni: failed to marshal payload: %w", err)
	}

	if string(bodyBytes) == "null" {
		bodyBytes = nil
	}

	return bytes.NewReader(bodyBytes), nil
}

func handleResponse(resp *http.Response, target any, requester Requester) error {
	if resp == nil {
		return errors.New("aoni: response is nil")
	}

	if resp.Request != nil {
		if targetPtr, ok := resp.Request.Context().Value(capturerCtxKey{}).(**http.Response); ok {
			*targetPtr = resp
		} else {
			defer closeResponse(resp)
		}
	} else {
		defer closeResponse(resp)
	}

	if resp.Request != nil && resp.Request.Context().Value(debugCtxKey{}) != nil {
		reqDump, _ := httputil.DumpRequestOut(resp.Request, true)
		respDump, _ := httputil.DumpResponse(resp, true)

		reqDump = redactHeaders(reqDump)
		respDump = redactHeaders(respDump)

		if logger, ok := requester.(interface{ Logger() Logger }); ok {
			logger.Logger().Debug("Aoni HTTP Diagnostic", "request", string(reqDump), "response", string(respDump))
		} else {
			fmt.Fprintf(
				os.Stderr,
				"\n--- HTTP DEBUG ---\n%s\n%s\n------------------\n",
				string(reqDump),
				string(respDump),
			)
		}
	}

	peekableReader := bufio.NewReader(newBOMStrippingReader(resp.Body))

	resp.Body = &bomStrippingReadCloser{
		Reader: peekableReader,
		Closer: resp.Body,
	}

	decoder := JSONDecoder
	if resp.Request != nil {
		if d, ok := resp.Request.Context().Value(decoderCtxKey{}).(Decoder); ok {
			decoder = d
		}
	}

	_, isRaw := decoder.(rawDecoder)

	if !isRaw {
		if peekBytes, err := peekableReader.Peek(128); err == nil || (err == io.EOF && len(peekBytes) > 0) {
			firstNonSpace := byte(0)
			for _, b := range peekBytes {
				if b != ' ' && b != '\t' && b != '\r' && b != '\n' {
					firstNonSpace = b
					break
				}
			}

			if firstNonSpace == '<' {
				bodyStr := strings.ToLower(string(peekBytes))
				isHTML := strings.Contains(bodyStr, "<html") || strings.Contains(bodyStr, "<!doctype html")

				if isHTML {
					if strings.Contains(bodyStr, "cf-challenge") || strings.Contains(bodyStr, "ray id") ||
						strings.Contains(bodyStr, "cloudflare") {
						return ErrCloudflareChallenge
					}

					return fmt.Errorf("%w: expected structured data but got HTML", ErrUnexpectedContentType)
				}
			}
		}
	}

	if contentType := resp.Header.Get("Content-Type"); contentType != "" {
		if mediaType, _, err := mime.ParseMediaType(contentType); err == nil {
			if (mediaType == "text/html" || mediaType == "application/xhtml+xml") && !isRaw {
				bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 100*1024))
				_ = resp.Body.Close()

				bodyStr := string(bodyBytes)
				if strings.Contains(bodyStr, "cf-challenge") || strings.Contains(bodyStr, "ray id") ||
					strings.Contains(bodyStr, "cloudflare") {
					return ErrCloudflareChallenge
				}

				return fmt.Errorf("%w: expected structured data but got HTML", ErrUnexpectedContentType)
			}
		}
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))

		apiErr := &APIError{StatusCode: resp.StatusCode, Body: bodyBytes}
		if resp.Request != nil {
			if errModel := resp.Request.Context().Value(errorModelCtxKey{}); errModel != nil {
				if err := json.Unmarshal(bodyBytes, errModel); err == nil {
					apiErr.Model = errModel
				}
			}
		}

		return apiErr
	}

	if target == nil || resp.StatusCode == http.StatusNoContent {
		bufPtr := bytePool.Get().(*[]byte)
		_, _ = io.CopyBuffer(io.Discard, resp.Body, *bufPtr)
		bytePool.Put(bufPtr)

		return nil
	}

	if provider, ok := requester.(BaseResponseProvider); ok {
		if br := provider.BaseResponse(); br != nil {
			br.SetData(target)

			if err := decoder.Decode(resp.Body, br); err != nil {
				return err
			}

			if !br.IsSuccess() {
				return br.Error()
			}

			return nil
		}
	}

	err := decoder.Decode(resp.Body, target)
	if errors.Is(err, io.EOF) {
		return nil
	}

	return err
}

func requestTo[Resp any](
	ctx context.Context,
	c Requester,
	method, path string,
	mods ...RequestModifier,
) (*Resp, error) {
	resp, err := c.Request(ctx, method, path, mods...)
	if err != nil {
		return nil, err
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		closeResponse(resp)

		return nil, err
	}

	result := new(Resp)
	if err := handleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}
