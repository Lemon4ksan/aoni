// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
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
	"strings"
)

// DefaultClient is the shared default client instance used by global helper functions.
var DefaultClient = NewClient(nil)

// Get performs a global GET request using [DefaultClient] and decodes the JSON response body.
func Get[Resp any](ctx context.Context, path string, mods ...RequestModifier) (*Resp, error) {
	return GetJSON[Resp](ctx, DefaultClient, path, mods...)
}

// Post performs a global POST request using [DefaultClient] and decodes the JSON response body.
func Post[Req, Resp any](
	ctx context.Context,
	path string,
	payload Req,
	mods ...RequestModifier,
) (*Resp, error) {
	return PostJSON[Req, Resp](ctx, DefaultClient, path, payload, mods...)
}

// Put performs a global PUT request using [DefaultClient] and decodes the JSON response body.
func Put[Req, Resp any](
	ctx context.Context,
	path string,
	payload Req,
	mods ...RequestModifier,
) (*Resp, error) {
	return PutJSON[Req, Resp](ctx, DefaultClient, path, payload, mods...)
}

// Patch performs a global PATCH request using [DefaultClient] and decodes the JSON response body.
func Patch[Req, Resp any](
	ctx context.Context,
	path string,
	payload Req,
	mods ...RequestModifier,
) (*Resp, error) {
	return PatchJSON[Req, Resp](ctx, DefaultClient, path, payload, mods...)
}

// Delete performs a global DELETE request using [DefaultClient] and decodes the JSON response body.
func Delete[Req, Resp any](
	ctx context.Context,
	path string,
	payload Req,
	mods ...RequestModifier,
) (*Resp, error) {
	return DeleteJSON[Req, Resp](ctx, DefaultClient, path, payload, mods...)
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

	result := new(Resp)
	if err := handleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// PostForm marshals the payload, performs a POST request with URL-encoded parameters,
// and decodes the resulting JSON response body into Resp.
//
// It validates the payload structure beforehand using [Validate].
// Returns a [ValidationError] if validation fails.
func PostForm[Req, Resp any](
	ctx context.Context,
	c Requester,
	path string,
	payload Req,
	mods ...RequestModifier,
) (*Resp, error) {
	if err := Validate(payload); err != nil {
		return nil, err
	}

	formValues, err := StructToValues(payload)
	if err != nil {
		return nil, err
	}

	mods = append([]RequestModifier{
		WithContentType("application/x-www-form-urlencoded"),
		WithBody(strings.NewReader(formValues.Encode())),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodPost, path, mods...)
	if err != nil {
		return nil, err
	}

	result := new(Resp)
	if err := handleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// PostJSON marshals the payload to JSON, executes a POST request, and decodes the response body.
// It automatically configures the request headers with Content-Type and Accept set to "application/json".
//
// It validates the payload structure beforehand using [Validate].
// Returns a [ValidationError] if validation fails.
func PostJSON[Req, Resp any](
	ctx context.Context,
	c Requester,
	path string,
	payload Req,
	mods ...RequestModifier,
) (*Resp, error) {
	if err := Validate(payload); err != nil {
		return nil, err
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("aoni: failed to marshal payload: %w", err)
	}

	mods = append([]RequestModifier{
		WithContentType("application/json"),
		WithAccept("application/json"),
		WithBody(bytes.NewReader(bodyBytes)),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodPost, path, mods...)
	if err != nil {
		return nil, err
	}

	result := new(Resp)
	if err := handleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// PutJSON marshals the payload to JSON, executes a PUT request, and decodes the response body.
// It automatically configures the request headers with Content-Type and Accept set to "application/json".
//
// It validates the payload structure beforehand using [Validate].
// Returns a [ValidationError] if validation fails.
func PutJSON[Req, Resp any](
	ctx context.Context,
	c Requester,
	path string,
	payload Req,
	mods ...RequestModifier,
) (*Resp, error) {
	if err := Validate(payload); err != nil {
		return nil, err
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("aoni: failed to marshal payload: %w", err)
	}

	mods = append([]RequestModifier{
		WithContentType("application/json"),
		WithAccept("application/json"),
		WithBody(bytes.NewReader(bodyBytes)),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodPut, path, mods...)
	if err != nil {
		return nil, err
	}

	result := new(Resp)
	if err := handleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// PatchJSON marshals the payload to JSON, executes a PATCH request, and decodes the response body.
// It automatically configures the request headers with Content-Type and Accept set to "application/json".
//
// It validates the payload structure beforehand using [Validate].
// Returns a [ValidationError] if validation fails.
func PatchJSON[Req, Resp any](
	ctx context.Context,
	c Requester,
	path string,
	payload Req,
	mods ...RequestModifier,
) (*Resp, error) {
	if err := Validate(payload); err != nil {
		return nil, err
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("aoni: failed to marshal payload: %w", err)
	}

	mods = append([]RequestModifier{
		WithContentType("application/json"),
		WithAccept("application/json"),
		WithBody(bytes.NewReader(bodyBytes)),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodPatch, path, mods...)
	if err != nil {
		return nil, err
	}

	result := new(Resp)
	if err := handleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// DeleteJSON marshals the payload to JSON, executes a DELETE request, and decodes the response body.
// It automatically configures the request headers with Content-Type and Accept set to "application/json".
//
// It validates the payload structure beforehand using [Validate].
// Returns a [ValidationError] if validation fails.
func DeleteJSON[Req, Resp any](
	ctx context.Context,
	c Requester,
	path string,
	payload Req,
	mods ...RequestModifier,
) (*Resp, error) {
	if err := Validate(payload); err != nil {
		return nil, err
	}

	var (
		bodyBytes []byte
		err       error
	)

	bodyBytes, err = json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("aoni: failed to marshal payload: %w", err)
	}

	if string(bodyBytes) == "null" {
		bodyBytes = nil
	}

	mods = append([]RequestModifier{
		WithContentType("application/json"),
		WithAccept("application/json"),
		WithBody(bytes.NewReader(bodyBytes)),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodDelete, path, mods...)
	if err != nil {
		return nil, err
	}

	result := new(Resp)
	if err := handleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

func handleResponse(resp *http.Response, target any, requester Requester) error {
	if resp == nil {
		return errors.New("aoni: response is nil")
	}

	if resp.Request != nil && resp.Request.Context().Value(debugCtxKey{}) != nil {
		reqDump, _ := httputil.DumpRequestOut(resp.Request, true)
		respDump, _ := httputil.DumpResponse(resp, true)

		fmt.Fprintf(os.Stderr, "\n--- HTTP DEBUG ---\n%s\n%s\n------------------\n", string(reqDump), string(respDump))
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

	resp.Body = &bomStrippingReadCloser{
		Reader: newBOMStrippingReader(resp.Body),
		Closer: resp.Body,
	}

	decoder := JSONDecoder
	if resp.Request != nil {
		if d, ok := resp.Request.Context().Value(decoderCtxKey{}).(Decoder); ok {
			decoder = d
		}
	}

	if contentType := resp.Header.Get("Content-Type"); contentType != "" {
		if mediaType, _, err := mime.ParseMediaType(contentType); err == nil {
			_, isRaw := decoder.(rawDecoder)
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
