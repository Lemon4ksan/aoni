// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"

	"github.com/lemon4ksan/aoni/internal/io"
	"github.com/lemon4ksan/aoni/internal/pipeline"
)

var (
	// ErrNilRequest is returned when an operation is executed on a nil Request contract.
	ErrNilRequest = errors.New("aoni: request is nil")

	// ErrInvalidPath indicates that the provided URL path could not be parsed.
	ErrInvalidPath = errors.New("aoni: invalid path")

	// ErrMaxRedirectsExceeded is returned when the request execution halts because the maximum redirect threshold was reached.
	ErrMaxRedirectsExceeded = errors.New("aoni: maximum redirects limit exceeded")

	// ErrResponseTooLarge indicates that response payload length exceeded configured bounds.
	ErrResponseTooLarge = io.ErrResponseTooLarge

	// ErrBufferLimitExceeded indicates replayable buffer size exceeded memory threshold without disk backing.
	ErrBufferLimitExceeded = io.ErrBufferLimitExceeded

	// ErrRedirectDomainForbidden is returned when a redirect target hostname is excluded by policy.
	ErrRedirectDomainForbidden = errors.New("aoni: redirect domain not allowed")

	// ErrHedgingBodyNonRepeatable is returned when request hedging attempt cannot duplicate a non-replayable payload stream.
	ErrHedgingBodyNonRepeatable = errors.New("aoni: request body cannot be duplicated for hedging")

	// ErrConflictingContentLength is returned when a response carries multiple conflicting Content-Length headers (RFC 9112).
	ErrConflictingContentLength = pipeline.ErrConflictingContentLength

	// ErrConflictingLocationHeader is returned when a response carries multiple conflicting Location headers.
	ErrConflictingLocationHeader = pipeline.ErrConflictingLocationHeader

	// ErrHeaderInjectionDetected is returned when a response header contains CRLF control characters.
	ErrHeaderInjectionDetected = pipeline.ErrHeaderInjectionDetected

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

// Error describes a structured operational failure in the aoni package.
type Error struct {
	// Err holds the underlying cause error.
	Err error
	// Op identifies the high-level operation during which the failure occurred.
	Op string
	// Path specifies the request path or URI associated with the error.
	Path string
	// Target identifies the remote target host or address.
	Target string
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}

	var sb strings.Builder
	sb.Grow(len(e.Op) + len(e.Target) + len(e.Path) + 32)
	sb.WriteString("aoni: ")

	if e.Op != "" {
		sb.WriteString(e.Op)
		sb.WriteString(": ")
	}

	if e.Target != "" {
		sb.WriteString(e.Target)
		sb.WriteString(": ")
	}

	if e.Path != "" {
		sb.WriteString(e.Path)
		sb.WriteString(": ")
	}

	if e.Err != nil {
		sb.WriteString(e.Err.Error())
	}

	return sb.String()
}

func (e *Error) Unwrap() error { return e.Err }

// LogValue implements [slog.LogValuer] for structured, zero-allocation logging.
func (e *Error) LogValue() slog.Value {
	if e == nil {
		return slog.GroupValue()
	}

	attrs := make([]slog.Attr, 0, 4)
	if e.Op != "" {
		attrs = append(attrs, slog.String("op", e.Op))
	}

	if e.Target != "" {
		attrs = append(attrs, slog.String("target", e.Target))
	}

	if e.Path != "" {
		attrs = append(attrs, slog.String("path", e.Path))
	}

	if e.Err != nil {
		attrs = append(attrs, slog.String("cause", e.Err.Error()))
	}

	return slog.GroupValue(attrs...)
}

// APIError describes an HTTP response status failure (>= 400).
type APIError struct {
	// Model holds an unmarshaled error envelope structure if parsed by a decoder.
	Model any
	// Body contains a snippet or full payload of the HTTP response error body.
	Body []byte
	// StatusCode records the non-2xx HTTP response status code (e.g., 400, 404, 500).
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

	cleanBody := bytes.Map(func(r rune) rune {
		if r == '\n' || r == '\r' {
			return ' '
		}

		return r
	}, bodySlice)

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

// HTTPStatusCategory classifies HTTP status codes into their RFC 9110 standard families.
type HTTPStatusCategory uint8

const (
	// CategoryUnknown represents an unclassified or invalid HTTP status code.
	CategoryUnknown HTTPStatusCategory = iota
	// CategoryInformational represents 1xx Informational response status codes.
	CategoryInformational
	// CategorySuccess represents 2xx Successful response status codes.
	CategorySuccess
	// CategoryRedirection represents 3xx Redirection response status codes.
	CategoryRedirection
	// CategoryClientError represents 4xx Client Error response status codes.
	CategoryClientError
	// CategoryServerError represents 5xx Server Error response status codes.
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

// AsTypedResult bridges a value-error tuple into a strongly typed [generic.TypedResult]
// wrapping [*APIError], conforming to Swift-like Typed Throws error models.
func AsTypedResult[T any](val T, err error) generic.TypedResult[T, *APIError] {
	if err == nil {
		return generic.SuccessTyped[T, *APIError](val)
	}

	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return generic.FailureTyped[T, *APIError](apiErr)
	}

	return generic.FailureTyped[T, *APIError](&APIError{
		StatusCode: http.StatusInternalServerError,
		Body:       bytesconv.S2B(err.Error()),
	})
}

// IsNotFound reports whether err represents an HTTP 404 Not Found response.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.IsNotFound()
}

// IsUnauthorized reports whether err represents an HTTP 401 Unauthorized response.
func IsUnauthorized(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.IsUnauthorized()
}

// IsForbidden reports whether err represents an HTTP 403 Forbidden response.
func IsForbidden(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.IsForbidden()
}

// IsRateLimited reports whether err represents an HTTP 429 Too Many Requests response.
func IsRateLimited(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.IsRateLimited()
}

// IsTooManyRequests is an alias for [IsRateLimited].
func IsTooManyRequests(err error) bool {
	return IsRateLimited(err)
}

// IsConflict reports whether err represents an HTTP 409 Conflict response.
func IsConflict(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.IsConflict()
}

// IsBadRequest reports whether err represents an HTTP 400 Bad Request response.
func IsBadRequest(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.IsBadRequest()
}

// IsTimeout reports whether err represents an HTTP 408/504 Timeout or context deadline exceeded.
func IsTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var apiErr *APIError

	return errors.As(err, &apiErr) && apiErr.IsTimeout()
}

// IsServerError reports whether err represents an HTTP 5xx server-side response.
func IsServerError(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.IsServerError()
}

// IsClientError reports whether err represents an HTTP 4xx client-side response.
func IsClientError(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.IsClientError()
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

// BridgeError describes an execution failure during stdlib [http.Client] bridging.
type BridgeError struct {
	// Err holds the underlying transport or pipeline error.
	Err error
	// Metadata contains additional context metadata (e.g. host, scheme).
	Metadata map[string]any
	// Op specifies the HTTP request method during which the error occurred.
	Op string
	// URL specifies the target request URL string.
	URL string
}

func (e *BridgeError) Error() string {
	if e == nil {
		return "<nil>"
	}

	var sb strings.Builder
	sb.Grow(len(e.Op) + len(e.URL) + 32)
	sb.WriteString("aoni/bridge: ")
	sb.WriteString(e.Op)
	sb.WriteByte(' ')
	sb.WriteString(e.URL)

	if e.Err != nil {
		sb.WriteString(": ")
		sb.WriteString(e.Err.Error())
	}

	return sb.String()
}

func (e *BridgeError) Unwrap() error {
	return e.Err
}
