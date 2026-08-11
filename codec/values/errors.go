// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package values

import (
	"errors"
	"strconv"
	"strings"
)

var (
	// ErrUnsupportedType is returned when a type cannot be encoded into URL query or form parameters.
	ErrUnsupportedType = errors.New("aoni/values: unsupported type for encoding")

	// ErrInvalidFormat is returned when a raw string representation fails parsing into a structured type.
	ErrInvalidFormat = errors.New("aoni/values: invalid value format")
)

// ValueError describes an error encountered during structure reflection or value marshaling.
type ValueError struct {
	Err   error
	Type  string
	Field string
	Index int
}

func (e *ValueError) Error() string {
	if e == nil {
		return "<nil>"
	}

	var sb strings.Builder
	sb.Grow(64)
	sb.WriteString("aoni/values: ")

	if e.Field != "" {
		sb.WriteString("field ")
		sb.WriteString(e.Field)

		if e.Index >= 0 {
			var numBuf [12]byte

			sb.WriteByte('[')
			sb.Write(strconv.AppendInt(numBuf[:0], int64(e.Index), 10))
			sb.WriteByte(']')
		}

		sb.WriteString(": ")
		sb.WriteString(e.Err.Error())

		return sb.String()
	}

	if e.Type != "" {
		sb.WriteString(e.Type)
		sb.WriteString(": ")
		sb.WriteString(e.Err.Error())

		return sb.String()
	}

	sb.WriteString(e.Err.Error())

	return sb.String()
}

func (e *ValueError) Unwrap() error { return e.Err }
