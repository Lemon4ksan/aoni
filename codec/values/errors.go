// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package values

import (
	"errors"
	"strconv"
)

var (
	// ErrUnsupportedType is returned when a value or field type cannot be encoded into URL parameters.
	ErrUnsupportedType = errors.New("aoni values: unsupported type for encoding")

	// ErrInvalidFormat is returned when a raw string representation fails parsing into a structured type.
	ErrInvalidFormat = errors.New("aoni values: invalid value format")
)

// ValueError describes an error encountered during structure reflection or value marshaling.
type ValueError struct {
	Type  string // Type name for scalar unmarshaling errors (e.g. "Uint64String")
	Field string // Field name in struct schema
	Index int    // Slice element index (-1 if scalar)
	Err   error  // Underlying cause
}

func (e *ValueError) Error() string {
	if e == nil {
		return "<nil>"
	}

	if e.Field != "" {
		if e.Index >= 0 {
			return "aoni values: field " + e.Field + "[" + strconv.Itoa(e.Index) + "]: " + e.Err.Error()
		}

		return "aoni values: field " + e.Field + ": " + e.Err.Error()
	}

	if e.Type != "" {
		return "aoni values: " + e.Type + ": " + e.Err.Error()
	}

	return "aoni values: " + e.Err.Error()
}

func (e *ValueError) Unwrap() error { return e.Err }
