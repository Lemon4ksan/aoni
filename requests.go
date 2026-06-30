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
type NoResponse struct{}

// Get performs a global GET request using [DefaultClient] and decodes the JSON response body.
func Get[Resp any](ctx context.Context, path string, mods ...RequestModifier) (*Resp, error) {
	return GetJSON[Resp](ctx, DefaultClient, path, mods...)
}

// Post performs a global POST request using [DefaultClient] and decodes the JSON response body.
func Post[Resp any](
	ctx context.Context,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, error) {
	return PostJSON[Resp](ctx, DefaultClient, path, body, mods...)
}

// Put performs a global PUT request using [DefaultClient] and decodes the JSON response body.
func Put[Resp any](
	ctx context.Context,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, error) {
	return PutJSON[Resp](ctx, DefaultClient, path, body, mods...)
}

// Patch performs a global PATCH request using [DefaultClient] and decodes the JSON response body.
func Patch[Resp any](
	ctx context.Context,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, error) {
	return PatchJSON[Resp](ctx, DefaultClient, path, body, mods...)
}

// Delete performs a global DELETE request using [DefaultClient] and decodes the JSON response body.
func Delete[Resp any](
	ctx context.Context,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, error) {
	return DeleteJSON[Resp](ctx, DefaultClient, path, body, mods...)
}

// GetJSON performs a GET request and decodes the JSON response body into a new instance of Resp.
// It returns an [APIError] if the server responds with a non-2xx status code.
func GetJSON[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	mods ...RequestModifier,
) (*Resp, error) {
	resp, err := c.Request(ctx, http.MethodGet, path, mods...)
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

// GetJSONEx is like [GetJSON] but returns both the parsed response payload and the raw *http.Response.
func GetJSONEx[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	mods ...RequestModifier,
) (*Resp, *http.Response, error) {
	var raw *http.Response

	mods = append(mods, CaptureResponse(&raw))

	result, err := GetJSON[Resp](ctx, c, path, mods...)
	if err != nil {
		return nil, raw, err
	}

	return result, raw, nil
}

// PostFormJSON marshals the body, performs a POST request with URL-encoded parameters,
// and decodes the resulting JSON response body into Resp.
//
// If the body implements [io.Reader], it is used directly as the request body.
// Otherwise, the body is marshaled to URL-encoded form values and wrapped in a [strings.Reader].
//
// It validates the body structure beforehand using [Validate].
// Returns a [ValidationError] if validation fails.
func PostFormJSON[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, error) {
	var bodyReader io.Reader

	if r, ok := body.(io.Reader); ok {
		bodyReader = r
	} else if body != nil {
		if err := Validate(body); err != nil {
			return nil, err
		}

		formValues, err := StructToValues(body)
		if err != nil {
			return nil, err
		}

		bodyReader = strings.NewReader(formValues.Encode())
	}

	mods = append([]RequestModifier{
		WithContentType("application/x-www-form-urlencoded"),
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

// PostFormJSONEx is like [PostFormJSON] but returns both the parsed response payload and the raw *http.Response.
func PostFormJSONEx[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, *http.Response, error) {
	var raw *http.Response

	mods = append(mods, CaptureResponse(&raw))

	result, err := PostFormJSON[Resp](ctx, c, path, body, mods...)
	if err != nil {
		return nil, raw, err
	}

	return result, raw, nil
}

// PostJSON marshals the body to JSON, executes a POST request, and decodes the response body.
// It automatically configures the request headers with Content-Type and Accept set to "application/json".
//
// It validates the body structure beforehand using [Validate].
// Returns a [ValidationError] if validation fails.
func PostJSON[Resp any](
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

// PostJSONEx is like [PostJSON] but returns both the parsed response payload and the raw *http.Response.
func PostJSONEx[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, *http.Response, error) {
	var raw *http.Response

	mods = append(mods, CaptureResponse(&raw))

	result, err := PostJSON[Resp](ctx, c, path, body, mods...)
	if err != nil {
		return nil, raw, err
	}

	return result, raw, nil
}

// PutJSON marshals the body to JSON, executes a PUT request, and decodes the response body.
// It automatically configures the request headers with Content-Type and Accept set to "application/json".
//
// It validates the body structure beforehand using [Validate].
// Returns a [ValidationError] if validation fails.
func PutJSON[Resp any](
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

// PutJSONEx is like [PutJSON] but returns both the parsed response payload and the raw *http.Response.
func PutJSONEx[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, *http.Response, error) {
	var raw *http.Response

	mods = append(mods, CaptureResponse(&raw))

	result, err := PutJSON[Resp](ctx, c, path, body, mods...)
	if err != nil {
		return nil, raw, err
	}

	return result, raw, nil
}

// PatchJSON marshals the body to JSON, executes a PATCH request, and decodes the response body.
// It automatically configures the request headers with Content-Type and Accept set to "application/json".
//
// It validates the body structure beforehand using [Validate].
// Returns a [ValidationError] if validation fails.
func PatchJSON[Resp any](
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

// PatchJSONEx is like [PatchJSON] but returns both the parsed response payload and the raw *http.Response.
func PatchJSONEx[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, *http.Response, error) {
	var raw *http.Response

	mods = append(mods, CaptureResponse(&raw))

	result, err := PatchJSON[Resp](ctx, c, path, body, mods...)
	if err != nil {
		return nil, raw, err
	}

	return result, raw, nil
}

// DeleteJSON marshals the body to JSON, executes a DELETE request, and decodes the response body.
// It automatically configures the request headers with Content-Type and Accept set to "application/json".
//
// It validates the body structure beforehand using [Validate].
// Returns a [ValidationError] if validation fails.
func DeleteJSON[Resp any](
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

// DeleteJSONEx is like [DeleteJSON] but returns both the parsed response payload and the raw *http.Response.
func DeleteJSONEx[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, *http.Response, error) {
	var raw *http.Response

	mods = append(mods, CaptureResponse(&raw))

	result, err := DeleteJSON[Resp](ctx, c, path, body, mods...)
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
