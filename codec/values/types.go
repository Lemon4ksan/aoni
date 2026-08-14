// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package values

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/lemon4ksan/aoni/internal/bytesconv"
)

var (
	bOne  = []byte("1")
	bZero = []byte("0")
	bNull = []byte("null")
)

// Uint64String parses uint64 values from numeric or quoted string JSON payloads.
type Uint64String uint64

// UnmarshalJSON parses JSON byte data into [Uint64String].
func (u *Uint64String) UnmarshalJSON(b []byte) error {
	s := bytesconv.B2S(bytesconv.TrimQuotes(b))
	if len(s) == 0 || s == "null" {
		*u = 0
		return nil
	}

	val, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return &ValueError{Type: "Uint64String", Err: err}
	}

	*u = Uint64String(val)

	return nil
}

// MarshalJSON serializes [Uint64String] as a quoted JSON string.
func (u Uint64String) MarshalJSON() ([]byte, error) {
	return json.Marshal(strconv.FormatUint(uint64(u), 10))
}

// Int64String parses int64 values from numeric or quoted string JSON payloads.
type Int64String int64

// UnmarshalJSON parses JSON byte data into [Int64String].
func (i *Int64String) UnmarshalJSON(b []byte) error {
	s := bytesconv.B2S(bytesconv.TrimQuotes(b))
	if len(s) == 0 || s == "null" {
		*i = 0
		return nil
	}

	val, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return &ValueError{Type: "Int64String", Err: err}
	}

	*i = Int64String(val)

	return nil
}

// MarshalJSON serializes [Int64String] as a quoted JSON string.
func (i Int64String) MarshalJSON() ([]byte, error) {
	return json.Marshal(strconv.FormatInt(int64(i), 10))
}

// Float64String parses float64 values from numeric or quoted string JSON payloads.
type Float64String float64

// UnmarshalJSON parses JSON byte data into [Float64String].
func (f *Float64String) UnmarshalJSON(b []byte) error {
	s := bytesconv.B2S(bytesconv.TrimQuotes(b))
	if len(s) == 0 || s == "null" {
		*f = 0
		return nil
	}

	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return &ValueError{Type: "Float64String", Err: err}
	}

	*f = Float64String(val)

	return nil
}

// MarshalJSON serializes [Float64String] as a JSON string.
func (f Float64String) MarshalJSON() ([]byte, error) {
	return json.Marshal(strconv.FormatFloat(float64(f), 'f', -1, 64))
}

// BoolInt parses boolean flags represented as numbers or strings in JSON.
type BoolInt bool

// UnmarshalJSON implements [json.Unmarshaler].
func (bi *BoolInt) UnmarshalJSON(b []byte) error {
	s := bytesconv.B2S(bytesconv.TrimQuotes(b))

	switch {
	case s == "1" || bytesconv.EqualFoldASCII(s, "true"):
		*bi = true
	case s == "0" || bytesconv.EqualFoldASCII(s, "false") || len(s) == 0 || s == "null":
		*bi = false
	default:
		val, err := strconv.Atoi(s)
		*bi = (err == nil && val != 0)
	}

	return nil
}

// MarshalJSON serializes [BoolInt] back as numeric "1" or "0" JSON values.
func (bi BoolInt) MarshalJSON() ([]byte, error) {
	if bi {
		return bOne, nil
	}

	return bZero, nil
}

// UnixTimestamp parses UNIX epoch timestamps from strings or numbers in JSON.
type UnixTimestamp time.Time

// UnmarshalJSON implements [json.Unmarshaler].
func (t *UnixTimestamp) UnmarshalJSON(b []byte) error {
	s := bytesconv.B2S(bytesconv.TrimQuotes(b))
	if len(s) == 0 || s == "null" || s == "0" {
		*t = UnixTimestamp(time.Time{})
		return nil
	}

	unix, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return &ValueError{Type: "UnixTimestamp", Err: err}
	}

	*t = UnixTimestamp(time.Unix(unix, 0).UTC())

	return nil
}

// MarshalJSON serializes [UnixTimestamp] as a numeric Unix epoch timestamp.
func (t UnixTimestamp) MarshalJSON() ([]byte, error) {
	if time.Time(t).IsZero() {
		return bZero, nil
	}

	return []byte(strconv.FormatInt(time.Time(t).Unix(), 10)), nil
}

// Time returns the underlying [time.Time].
func (t UnixTimestamp) Time() time.Time { return time.Time(t) }

// RFC3339Timestamp parses ISO-8601 / RFC-3339 formatted date-time strings in JSON.
type RFC3339Timestamp time.Time

// UnmarshalJSON implements [json.Unmarshaler].
func (t *RFC3339Timestamp) UnmarshalJSON(b []byte) error {
	s := bytesconv.B2S(bytesconv.TrimQuotes(b))
	if len(s) == 0 || s == "null" {
		*t = RFC3339Timestamp(time.Time{})
		return nil
	}

	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return &ValueError{Type: "RFC3339Timestamp", Err: err}
	}

	*t = RFC3339Timestamp(parsed.UTC())

	return nil
}

// MarshalJSON implements [json.Marshaler].
func (t RFC3339Timestamp) MarshalJSON() ([]byte, error) {
	if time.Time(t).IsZero() {
		return bNull, nil
	}

	return json.Marshal(time.Time(t).Format(time.RFC3339))
}

// Time returns the underlying [time.Time] value.
func (t RFC3339Timestamp) Time() time.Time { return time.Time(t) }

// String returns the RFC-3339 formatted date-time string.
func (t RFC3339Timestamp) String() string {
	if time.Time(t).IsZero() {
		return ""
	}

	return time.Time(t).Format(time.RFC3339)
}

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
