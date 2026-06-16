// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RequestModifier is a function that can modify an *http.Request before it is sent.
// This is used for adding one-off headers, authentication tokens, or logging.
type RequestModifier func(req *http.Request)

// WithVar replaces a placeholder in the path (e.g., "{id}") with a value.
// The value is automatically URL-escaped.
func WithVar(key string, value any) RequestModifier {
	return func(req *http.Request) {
		placeholder := "{" + key + "}"
		escapedValue := url.PathEscape(fmt.Sprint(value))

		req.URL.Path = strings.ReplaceAll(req.URL.Path, placeholder, escapedValue)
		if req.URL.RawPath != "" {
			req.URL.RawPath = strings.ReplaceAll(req.URL.RawPath, placeholder, escapedValue)
		}
	}
}

// WithVars replaces multiple placeholders in the path.
// It accepts pairs of key-value arguments.
// If the pairs slice has an odd length, the function returns early
// without modifying the request.
func WithVars(pairs ...any) RequestModifier {
	return func(req *http.Request) {
		if len(pairs)%2 != 0 {
			return
		}

		for i := 0; i < len(pairs); i += 2 {
			key := fmt.Sprint(pairs[i])
			value := fmt.Sprint(pairs[i+1])
			WithVar(key, value)(req)
		}
	}
}

// WithQuery adds a query string to the request URL.
func WithQuery(query any) RequestModifier {
	return func(req *http.Request) {
		if query == nil {
			return
		}

		if err := Validate(query); err != nil {
			ctx := context.WithValue(req.Context(), queryErrorCtxKey{}, err)
			*req = *req.WithContext(ctx)
			return
		}

		qValues, err := StructToValues(query)
		if err != nil {
			ctx := context.WithValue(req.Context(), queryErrorCtxKey{}, err)
			*req = *req.WithContext(ctx)
			return
		}

		if len(qValues) > 0 {
			req.URL.RawQuery = qValues.Encode()
		}
	}
}

// WithHeader sets the value of a header on the request.
func WithHeader(key, value string) RequestModifier {
	return func(req *http.Request) {
		req.Header.Set(key, value)
	}
}

// WithBearer adds a "Authorization: Bearer <token>" header.
func WithBearer(token string) RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

// WithBasicAuth adds a "Authorization: Basic <base64>" header.
func WithBasicAuth(username, password string) RequestModifier {
	return func(req *http.Request) {
		req.SetBasicAuth(username, password)
	}
}

// WithUserAgent sets the "User-Agent" header.
func WithUserAgent(ua string) RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("User-Agent", ua)
	}
}

// WithContentType sets the "Content-Type" header.
func WithContentType(ct string) RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("Content-Type", ct)
	}
}

// WithAccept sets the "Accept" header.
func WithAccept(accept string) RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("Accept", accept)
	}
}

// WithCookie adds a cookie to the request.
func WithCookie(c *http.Cookie) RequestModifier {
	return func(req *http.Request) {
		req.AddCookie(c)
	}
}

// WithCookies adds multiple cookies to the request from a map.
func WithCookies(kv map[string]string) RequestModifier {
	return func(req *http.Request) {
		for k, v := range kv {
			req.AddCookie(&http.Cookie{Name: k, Value: v})
		}
	}
}

// WithBody overrides the request body with the provided io.Reader.
func WithBody(r io.Reader) RequestModifier {
	return func(req *http.Request) {
		rc, ok := r.(io.ReadCloser)
		if !ok && r != nil {
			rc = io.NopCloser(r)
		}

		req.Body = rc

		// Attempt to set Content-Length if it's a known size type
		if r != nil {
			if b, ok := r.(interface{ Len() int }); ok {
				req.ContentLength = int64(b.Len())
			} else if s, ok := r.(interface{ Len() int64 }); ok {
				req.ContentLength = s.Len()
			}
		}
	}
}

// WithJSONBody sets the request body to the JSON representation of the provided payload.
func WithJSONBody(payload any) RequestModifier {
	return func(req *http.Request) {
		bodyBytes, err := json.Marshal(payload)
		if err != nil {
			ctx := context.WithValue(req.Context(), bodyErrorCtxKey{}, err)
			*req = *req.WithContext(ctx)
			return
		}

		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		req.ContentLength = int64(len(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
	}
}

// WithMultipart creates a RequestModifier that sets the request body to a multipart form
// containing the specified fields and files.
// It automatically sets the Content-Type header to multipart/form-data with the correct boundary.
func WithMultipart(fields map[string]string, files map[string]io.Reader) RequestModifier {
	return func(req *http.Request) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		// Write text fields
		for k, v := range fields {
			if err := writer.WriteField(k, v); err != nil {
				return
			}
		}

		// Write files
		for key, r := range files {
			part, err := writer.CreateFormFile(key, key)
			if err != nil {
				return
			}

			bufPtr := bytePool.Get().(*[]byte)
			_, err = io.CopyBuffer(part, r, *bufPtr)
			bytePool.Put(bufPtr)

			if err != nil {
				return
			}
		}

		if err := writer.Close(); err != nil {
			return
		}

		req.Body = io.NopCloser(body)
		req.ContentLength = int64(body.Len())
		req.Header.Set("Content-Type", writer.FormDataContentType())
	}
}

// WithOrigin sets the "Origin" header.
func WithOrigin(origin string) RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("Origin", origin)
	}
}

type debugCtxKey struct{}

// Debug returns a modifier that enables verbose logging for the request.
// It prints the request and response (including headers and body) to stderr.
func Debug() RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), debugCtxKey{}, true)
		*req = *req.WithContext(ctx)
	}
}

// WithDecoder sets the decoder to use for the response body.
func WithDecoder(d Decoder) RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), decoderCtxKey{}, d)
		*req = *req.WithContext(ctx)
	}
}

// WithErrorModel returns a RequestModifier that sets the target structure
// for decoding structured error responses when HTTP status is not 2xx.
func WithErrorModel(target any) RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), errorModelCtxKey{}, target)
		*req = *req.WithContext(ctx)
	}
}

// WithUploadProgress returns a RequestModifier that tracks upload progress.
func WithUploadProgress(onProgress ProgressFunc) RequestModifier {
	return func(req *http.Request) {
		if req.Body != nil && req.Body != http.NoBody {
			req.Body = &progressReader{
				reader:     req.Body,
				total:      req.ContentLength,
				onProgress: onProgress,
			}
		}
	}
}

// WithDownloadProgress returns a RequestModifier that tracks download progress.
func WithDownloadProgress(onProgress ProgressFunc) RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), downloadProgressCtxKey{}, onProgress)
		*req = *req.WithContext(ctx)
	}
}

// WithHedging returns a RequestModifier that sets the hedging delay for this request.
// Hedging is disabled if delay <= 0.
func WithHedging(delay time.Duration) RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), hedgingCtxKey{}, delay)
		*req = *req.WithContext(ctx)
	}
}

// CaptureResponse returns a modifier that captures the *http.Response
// of the request. This is useful for accessing headers or cookies
// when using high-level functions like GetJSON.
func CaptureResponse(target **http.Response) RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), capturerCtxKey{}, target)
		*req = *req.WithContext(ctx)
	}
}
