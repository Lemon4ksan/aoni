// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/iokit"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"

	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/pipeline"
)

var (
	// ErrNilRequest is returned when an operation is executed on a nil Request contract.
	ErrNilRequest = errors.New("aoni: request is nil")

	// ErrNilURL is returned when attempting to route an outbound HTTP request
	// that does not specify a destination URL through [Transport].
	ErrNilURL = errors.New("aoni: *http.Request URL is nil")

	// ErrInvalidPath indicates that the provided URL path could not be parsed.
	ErrInvalidPath = errors.New("aoni: invalid path")

	// ErrMaxRedirectsExceeded is returned when the request execution halts because the maximum redirect threshold was reached.
	ErrMaxRedirectsExceeded = errors.New("aoni: maximum redirects limit exceeded")

	// ErrResponseTooLarge indicates that response payload length exceeded configured bounds.
	ErrResponseTooLarge = iokit.ErrResponseTooLarge

	// ErrBufferLimitExceeded indicates replayable buffer size exceeded memory threshold without disk backing.
	ErrBufferLimitExceeded = iokit.ErrBufferLimitExceeded

	// ErrRedirectDomainForbidden is returned when a redirect target hostname is excluded by policy.
	ErrRedirectDomainForbidden = errors.New("aoni: redirect domain not allowed")

	// ErrRedirectBlocked is returned when a redirect is blocked by path matching policy.
	ErrRedirectBlocked = errors.New("aoni: redirect blocked by path policy")

	// ErrHedgingBodyNonRepeatable is returned when request hedging attempt cannot duplicate a non-replayable payload stream.
	ErrHedgingBodyNonRepeatable = errors.New("aoni: request body cannot be duplicated for hedging")

	// ErrConflictingContentLength is returned when a response carries multiple conflicting Content-Length headers (RFC 9112 §6.3).
	ErrConflictingContentLength = pipeline.ErrConflictingContentLength

	// ErrConflictingLocationHeader is returned when a response carries multiple conflicting Location headers.
	ErrConflictingLocationHeader = pipeline.ErrConflictingLocationHeader

	// ErrHeaderInjectionDetected is returned when a response header contains illegal CRLF/control characters (RFC 9112 §2.2 & §11.1).
	ErrHeaderInjectionDetected = pipeline.ErrHeaderInjectionDetected

	// ErrNotFound matches any HTTP 404 Not Found response when checked via [errors.Is].
	ErrNotFound = core.ErrNotFound

	// ErrUnauthorized matches any HTTP 401 Unauthorized response when checked via [errors.Is].
	ErrUnauthorized = core.ErrUnauthorized

	// ErrForbidden matches any HTTP 403 Forbidden response when checked via [errors.Is].
	ErrForbidden = core.ErrForbidden

	// ErrRateLimited matches any HTTP 429 Too Many Requests response when checked via [errors.Is].
	ErrRateLimited = core.ErrRateLimited

	// ErrConflict matches any HTTP 409 Conflict response when checked via [errors.Is].
	ErrConflict = core.ErrConflict

	// ErrBadRequest matches any HTTP 400 Bad Request response when checked via [errors.Is].
	ErrBadRequest = core.ErrBadRequest

	// ErrTimeout matches any HTTP 408 / 504 Timeout response when checked via [errors.Is].
	ErrTimeout = core.ErrTimeout

	// ErrServerError matches any HTTP 5xx Server Error response when checked via [errors.Is].
	ErrServerError = core.ErrServerError

	// ErrClientError matches any HTTP 4xx Client Error response when checked via [errors.Is].
	ErrClientError = core.ErrClientError
)

// APIError represents an HTTP protocol failure returned by the remote server (status code >= 400).
//
// # Architectural Rationale: Observability & Log-Injection Safety
//
// When APIs return errors, servers often respond with massive HTML error pages (e.g. Cloudflare 502s,
// Nginx 404s, or AWS XML errors). Printing raw error bodies into structured logs can cause log flooding,
// high ingestion costs, or CRLF log injection vulnerabilities.
//
// To prevent this:
//   - [APIError.Error] truncates body output to a bounded 128-byte preview snippet.
//   - Control characters ('\r', '\n') are sanitized into spaces to neutralize log injection.
//   - The full, unmodified payload remains accessible via [APIError.Body] and [APIError.BodyString].
//
// # Structured Error Extraction
//
// If a custom response decoder or middleware parses a structured error DTO (e.g. `{"code": "token_expired"}`),
// the unmarshaled struct is preserved in the [APIError.Model] field.
//
// # Standard Errors Interoperability
//
// APIError implements the standard Go [errors.Is] protocol ([APIError.Is]), allowing callers to check
// status categories idiomatically:
//
//	if errors.Is(err, aoni.ErrNotFound) { ... }
//	if errors.Is(err, aoni.ErrRateLimited) { ... }
//	// Or via single-line package predicates:
//	if aoni.IsRateLimited(err) { ... }
type (
	// APIError represents an HTTP protocol failure returned by the remote server (status code >= 400).
	APIError = core.APIError

	// HTTPStatusCategory classifies HTTP status codes into their RFC 9110 standard families (§15).
	HTTPStatusCategory = core.HTTPStatusCategory
)

const (
	// CategoryUnknown represents an unclassified or invalid HTTP status code.
	CategoryUnknown = core.CategoryUnknown
	// CategoryInformational represents 1xx Informational response status codes (RFC 9110 §15.2).
	CategoryInformational = core.CategoryInformational
	// CategorySuccess represents 2xx Successful response status codes (RFC 9110 §15.3).
	CategorySuccess = core.CategorySuccess
	// CategoryRedirection represents 3xx Redirection response status codes (RFC 9110 §15.4).
	CategoryRedirection = core.CategoryRedirection
	// CategoryClientError represents 4xx Client Error response status codes (RFC 9110 §15.5).
	CategoryClientError = core.CategoryClientError
	// CategoryServerError represents 5xx Server Error response status codes (RFC 9110 §15.6).
	CategoryServerError = core.CategoryServerError
)

// AsTypedResult converts a standard `(T, error)` tuple into a strongly typed `generic.TypedResult[T, *APIError]`.
//
// If err is non-nil and not already an [*APIError], it is wrapped into a 500 Internal Server Error [APIError].
//
// # Example
//
//	result := aoni.AsTypedResult(client.GetTo[User](ctx, "/users/1"))
//	if result.IsFailure() {
//	    log.Printf("Failed: %v", result.Error())
//	}
func AsTypedResult[T any](val T, err error) generic.TypedResult[T, *APIError] {
	if err == nil {
		return generic.SuccessTyped[T, *APIError](val)
	}

	if apiErr, ok := errors.AsType[*APIError](err); ok {
		return generic.FailureTyped[T](apiErr)
	}

	return generic.FailureTyped[T](&APIError{
		StatusCode: http.StatusInternalServerError,
		Body:       bytesconv.S2B(err.Error()),
	})
}

// IsNotFound reports whether err represents an HTTP 404 Not Found response.
//
// # Example
//
//	user, err := client.GetTo[User](ctx, "/users/unknown")
//	if aoni.IsNotFound(err) {
//	    // Handle missing user resource
//	}
func IsNotFound(err error) bool {
	apiErr, ok := errors.AsType[*APIError](err)
	return ok && apiErr.IsNotFound()
}

// IsUnauthorized reports whether err represents an HTTP 401 Unauthorized response (e.g. invalid or expired credentials).
func IsUnauthorized(err error) bool {
	apiErr, ok := errors.AsType[*APIError](err)
	return ok && apiErr.IsUnauthorized()
}

// IsForbidden reports whether err represents an HTTP 403 Forbidden response (e.g. insufficient permissions or WAF block).
func IsForbidden(err error) bool {
	apiErr, ok := errors.AsType[*APIError](err)
	return ok && apiErr.IsForbidden()
}

// IsRateLimited reports whether err represents an HTTP 429 Too Many Requests response.
//
// Inspect the response headers via `err.(*APIError).Headers` (e.g. `Retry-After`) to calculate backoff.
//
// # Example
//
//	if aoni.IsRateLimited(err) {
//	    time.Sleep(5 * time.Second)
//	}
func IsRateLimited(err error) bool {
	apiErr, ok := errors.AsType[*APIError](err)
	return ok && apiErr.IsRateLimited()
}

// IsTooManyRequests is an alias for [IsRateLimited].
func IsTooManyRequests(err error) bool {
	return IsRateLimited(err)
}

// IsConflict reports whether err represents an HTTP 409 Conflict response (e.g. optimistic locking or resource collision).
func IsConflict(err error) bool {
	apiErr, ok := errors.AsType[*APIError](err)
	return ok && apiErr.IsConflict()
}

// IsBadRequest reports whether err represents an HTTP 400 Bad Request response.
func IsBadRequest(err error) bool {
	apiErr, ok := errors.AsType[*APIError](err)
	return ok && apiErr.IsBadRequest()
}

// IsTimeout reports whether err represents an HTTP 408 Request Timeout, HTTP 504 Gateway Timeout,
// a context deadline cancellation ([context.DeadlineExceeded]), or an underlying network timeout ([net.Error]).
func IsTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	apiErr, ok := errors.AsType[*APIError](err)

	return ok && apiErr.IsTimeout()
}

// IsServerError reports whether err represents an HTTP 5xx Server Error status code (500-599).
func IsServerError(err error) bool {
	apiErr, ok := errors.AsType[*APIError](err)
	return ok && apiErr.IsServerError()
}

// IsClientError reports whether err represents an HTTP 4xx Client Error status code (400-499).
func IsClientError(err error) bool {
	apiErr, ok := errors.AsType[*APIError](err)
	return ok && apiErr.IsClientError()
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
