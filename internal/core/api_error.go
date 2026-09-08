// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package core

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/lemon4ksan/foundation/silicon/bytesconv"
)

var (
	// ErrNotFound matches any HTTP 404 Not Found response when checked via [errors.Is].
	ErrNotFound = errors.New("aoni: HTTP 404 Not Found")

	// ErrUnauthorized matches any HTTP 401 Unauthorized response when checked via [errors.Is].
	ErrUnauthorized = errors.New("aoni: HTTP 401 Unauthorized")

	// ErrForbidden matches any HTTP 403 Forbidden response when checked via [errors.Is].
	ErrForbidden = errors.New("aoni: HTTP 403 Forbidden")

	// ErrRateLimited matches any HTTP 429 Too Many Requests response when checked via [errors.Is].
	ErrRateLimited = errors.New("aoni: HTTP 429 Too Many Requests")

	// ErrConflict matches any HTTP 409 Conflict response when checked via [errors.Is].
	ErrConflict = errors.New("aoni: HTTP 409 Conflict")

	// ErrBadRequest matches any HTTP 400 Bad Request response when checked via [errors.Is].
	ErrBadRequest = errors.New("aoni: HTTP 400 Bad Request")

	// ErrTimeout matches any HTTP 408 / 504 Timeout response when checked via [errors.Is].
	ErrTimeout = errors.New("aoni: HTTP Timeout")

	// ErrServerError matches any HTTP 5xx Server Error response when checked via [errors.Is].
	ErrServerError = errors.New("aoni: HTTP 5xx Server Error")

	// ErrClientError matches any HTTP 4xx Client Error response when checked via [errors.Is].
	ErrClientError = errors.New("aoni: HTTP 4xx Client Error")
)

// APIError represents an HTTP protocol failure returned by the remote server (status code >= 400).
type APIError struct {
	// Model holds the typed error envelope structure if unmarshaled by a registered decoder or middleware.
	Model any

	// Body contains the raw byte slice of the HTTP error response body.
	Body []byte

	// StatusCode records the non-2xx HTTP status code (e.g. 400, 401, 403, 404, 429, 500, 503).
	StatusCode int
}

func (e *APIError) Error() string {
	if e == nil {
		return "<nil>"
	}

	var numBuf [10]byte

	statusBytes := strconv.AppendInt(numBuf[:0], int64(e.StatusCode), 10)
	statusText := http.StatusText(e.StatusCode)

	if len(e.Body) == 0 {
		if statusText != "" {
			return "aoni: HTTP " + bytesconv.B2S(statusBytes) + " " + statusText
		}

		return "aoni: HTTP " + bytesconv.B2S(statusBytes)
	}

	limit := min(len(e.Body), 128)
	bodySlice := e.Body[:limit]

	var cleanBuf [128]byte
	for i := range limit {
		b := bodySlice[i]
		if b == '\n' || b == '\r' {
			cleanBuf[i] = ' '
		} else {
			cleanBuf[i] = b
		}
	}

	cleanBody := cleanBuf[:limit]

	var sb strings.Builder
	sb.Grow(48 + len(cleanBody))
	sb.WriteString("aoni: HTTP ")
	sb.Write(statusBytes)

	if statusText != "" {
		sb.WriteByte(' ')
		sb.WriteString(statusText)
	}

	sb.WriteString(" (body: ")
	sb.Write(cleanBody)
	sb.WriteByte(')')

	return sb.String()
}

// Is reports whether this APIError matches target error for [errors.Is] compatibility.
func (e *APIError) Is(target error) bool {
	if e == nil {
		return false
	}

	switch target {
	case ErrNotFound:
		return e.IsNotFound()
	case ErrUnauthorized:
		return e.IsUnauthorized()
	case ErrForbidden:
		return e.IsForbidden()
	case ErrRateLimited:
		return e.IsRateLimited()
	case ErrConflict:
		return e.IsConflict()
	case ErrBadRequest:
		return e.IsBadRequest()
	case ErrTimeout:
		return e.IsTimeout()
	case ErrServerError:
		return e.IsServerError()
	case ErrClientError:
		return e.IsClientError()
	default:
		return false
	}
}

// IsNotFound reports whether the error represents an HTTP 404 Not Found response.
func (e *APIError) IsNotFound() bool {
	return e != nil && e.StatusCode == http.StatusNotFound
}

// IsUnauthorized reports whether the error represents an HTTP 401 Unauthorized response.
func (e *APIError) IsUnauthorized() bool {
	return e != nil && e.StatusCode == http.StatusUnauthorized
}

// IsForbidden reports whether the error represents an HTTP 403 Forbidden response.
func (e *APIError) IsForbidden() bool {
	return e != nil && e.StatusCode == http.StatusForbidden
}

// IsTooManyRequests reports whether the error represents an HTTP 429 Too Many Requests response.
func (e *APIError) IsTooManyRequests() bool {
	return e != nil && e.StatusCode == http.StatusTooManyRequests
}

// IsRateLimited is a convenience alias for [IsTooManyRequests].
func (e *APIError) IsRateLimited() bool {
	return e.IsTooManyRequests()
}

// IsConflict reports whether the error represents an HTTP 409 Conflict response.
func (e *APIError) IsConflict() bool {
	return e != nil && e.StatusCode == http.StatusConflict
}

// IsBadRequest reports whether the error represents an HTTP 400 Bad Request response.
func (e *APIError) IsBadRequest() bool {
	return e != nil && e.StatusCode == http.StatusBadRequest
}

// IsTimeout reports whether the error represents an HTTP 408 Request Timeout or 504 Gateway Timeout response.
func (e *APIError) IsTimeout() bool {
	return e != nil && (e.StatusCode == http.StatusRequestTimeout || e.StatusCode == http.StatusGatewayTimeout)
}

// IsServerError reports whether the error represents an HTTP 5xx server-side response.
func (e *APIError) IsServerError() bool {
	return e != nil && e.StatusCode >= http.StatusInternalServerError && e.StatusCode <= 599
}

// IsClientError reports whether the error represents an HTTP 4xx client-side response.
func (e *APIError) IsClientError() bool {
	return e != nil && e.StatusCode >= http.StatusBadRequest && e.StatusCode <= 499
}

// HTTPStatusCategory classifies HTTP status codes into their RFC 9110 standard families (§15).
type HTTPStatusCategory uint8

const (
	// CategoryUnknown represents an unclassified or invalid HTTP status code.
	CategoryUnknown HTTPStatusCategory = iota
	// CategoryInformational represents 1xx Informational response status codes (RFC 9110 §15.2).
	CategoryInformational
	// CategorySuccess represents 2xx Successful response status codes (RFC 9110 §15.3).
	CategorySuccess
	// CategoryRedirection represents 3xx Redirection response status codes (RFC 9110 §15.4).
	CategoryRedirection
	// CategoryClientError represents 4xx Client Error response status codes (RFC 9110 §15.5).
	CategoryClientError
	// CategoryServerError represents 5xx Server Error response status codes (RFC 9110 §15.6).
	CategoryServerError
)

// String returns the human-readable description of the HTTP status category.
func (c HTTPStatusCategory) String() string {
	switch c {
	case CategoryInformational:
		return "1xx Informational"
	case CategorySuccess:
		return "2xx Success"
	case CategoryRedirection:
		return "3xx Redirection"
	case CategoryClientError:
		return "4xx Client Error"
	case CategoryServerError:
		return "5xx Server Error"
	default:
		return "Unknown"
	}
}

// Category returns the RFC 9110 status code category family for this APIError.
func (e *APIError) Category() HTTPStatusCategory {
	if e == nil || e.StatusCode < 100 || e.StatusCode > 599 {
		return CategoryUnknown
	}

	return HTTPStatusCategory(e.StatusCode / 100)
}

// BodyString returns the error response payload as a string.
func (e *APIError) BodyString() string {
	if e == nil || len(e.Body) == 0 {
		return ""
	}

	return bytesconv.B2S(e.Body)
}

// LogValue implements [slog.LogValuer] for structured, zero-allocation logging.
func (e *APIError) LogValue() slog.Value {
	if e == nil {
		return slog.GroupValue()
	}

	attrs := make([]slog.Attr, 0, 3)
	attrs = append(attrs, slog.Int("status", e.StatusCode))

	if text := http.StatusText(e.StatusCode); text != "" {
		attrs = append(attrs, slog.String("error", text))
	}

	if len(e.Body) > 0 {
		limit := min(len(e.Body), 128)
		attrs = append(attrs, slog.String("body", bytesconv.B2S(e.Body[:limit])))
	}

	return slog.GroupValue(attrs...)
}
