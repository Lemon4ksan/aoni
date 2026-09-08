// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package core

import "errors"

// NoResponse is a sentinel type indicating a request that produces no unmarshaled body structure.
type NoResponse struct{}

var (
	// ErrUnexpectedContentType indicates that the response Content-Type header violates expected structured MIME formats.
	ErrUnexpectedContentType = errors.New("aoni: unexpected content-type (possible captive portal or intercept)")

	// ErrModifierAsBody is returned when a [RequestModifier] is accidentally passed as a request body payload argument.
	ErrModifierAsBody = errors.New("aoni: passed a RequestModifier as the request body payload")

	// ErrNilResponse is returned when attempting to process a nil [*http.Response].
	ErrNilResponse = errors.New("aoni: response is nil")
)
