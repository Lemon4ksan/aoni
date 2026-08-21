// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package values

import (
	"reflect"
	"strings"

	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	fvalues "github.com/lemon4ksan/foundation/types/values"
)

// Lenient types re-exported from [github.com/lemon4ksan/foundation/types/values].
type (
	// Uint64String parses uint64 values from numeric or quoted string JSON payloads.
	Uint64String = fvalues.Uint64String

	// Int64String parses int64 values from numeric or quoted string JSON payloads.
	Int64String = fvalues.Int64String

	// Float64String parses float64 values from numeric or quoted string JSON payloads.
	Float64String = fvalues.Float64String

	// BoolInt parses boolean flags represented as numbers or strings in JSON.
	BoolInt = fvalues.BoolInt

	// UnixTimestamp parses UNIX epoch timestamps from strings or numbers in JSON.
	UnixTimestamp = fvalues.UnixTimestamp

	// RFC3339Timestamp parses ISO-8601 / RFC-3339 formatted date-time strings in JSON.
	RFC3339Timestamp = fvalues.RFC3339Timestamp
)

// CommaSlice encodes a slice into a single comma-separated string parameter.
type CommaSlice[T any] []T

// MarshalText formats the slice items as a comma-joined string representation.
func (cs CommaSlice[T]) MarshalText() ([]byte, error) {
	if len(cs) == 0 {
		return nil, nil
	}

	var sb strings.Builder
	for i, item := range cs {
		if i > 0 {
			sb.WriteByte(',')
		}

		str, err := toString(reflect.ValueOf(item))
		if err != nil {
			return nil, &ValueError{Index: i, Err: err}
		}

		sb.WriteString(str)
	}

	return bytesconv.S2B(sb.String()), nil
}
