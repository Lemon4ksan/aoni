// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package values

import (
	"encoding"
	"encoding/json"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni/internal/bytesconv"
	"github.com/lemon4ksan/aoni/internal/mapper"
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
		return []byte("1"), nil
	}

	return []byte("0"), nil
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
		return []byte("0"), nil
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
		return []byte("null"), nil
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

// QueryEncoder is implemented by types that encode themselves directly into [url.Values].
type QueryEncoder interface {
	EncodeValues() url.Values
}

func getStructSchema(t reflect.Type) *mapper.StructSchema {
	return mapper.DefaultSchemaCache.GetSchema(t)
}

func fillValues(s *mapper.StructSchema, v reflect.Value, values url.Values) error {
	for i := range s.Fields {
		if err := fillField(&s.Fields[i], v.Field(s.Fields[i].Index), values); err != nil {
			return err
		}
	}

	return nil
}

func fillField(f *mapper.FieldSchema, fieldValue reflect.Value, values url.Values) error {
	if fieldValue.Kind() == reflect.Pointer {
		if fieldValue.IsNil() {
			if f.DefaultVal != "" && f.Key != "" && f.Key != "-" {
				values.Set(f.Key, f.DefaultVal)
			}

			return nil
		}

		fieldValue = fieldValue.Elem()
	}

	if (f.IsAnonymous || f.IsInline) && fieldValue.Kind() == reflect.Struct {
		if f.SubSchema != nil {
			return fillValues(f.SubSchema, fieldValue, values)
		}

		return fillValues(getStructSchema(fieldValue.Type()), fieldValue, values)
	}

	if f.IsIgnored || f.Key == "" || f.Key == "-" {
		return nil
	}

	if shouldSkipZeroValue(f, fieldValue, values) {
		return nil
	}

	return serializeValue(f, fieldValue, values)
}

func shouldSkipZeroValue(f *mapper.FieldSchema, fieldValue reflect.Value, values url.Values) bool {
	if !fieldValue.IsZero() {
		return false
	}

	if f.DefaultVal != "" {
		values.Set(f.Key, f.DefaultVal)
		return true
	}

	return f.OmitEmpty
}

func serializeValue(f *mapper.FieldSchema, fieldValue reflect.Value, values url.Values) error {
	if !fieldValue.CanInterface() {
		return nil
	}

	val := fieldValue.Interface()

	if pm, ok := val.(proto.Message); ok {
		opts := protojson.MarshalOptions{UseProtoNames: true}

		b, err := opts.Marshal(pm)
		if err != nil {
			return &ValueError{Field: f.Name, Err: err}
		}

		values.Set(f.Key, bytesconv.B2S(b))

		return nil
	}

	if hasTextOrStringerRepresentation(fieldValue) {
		str, err := toString(fieldValue)
		if err != nil {
			return &ValueError{Field: f.Name, Err: err}
		}

		values.Set(f.Key, str)

		return nil
	}

	if fieldValue.Kind() == reflect.Struct || fieldValue.Kind() == reflect.Map {
		b, err := json.Marshal(val)
		if err != nil {
			return &ValueError{Field: f.Name, Err: err}
		}

		values.Set(f.Key, bytesconv.B2S(b))

		return nil
	}

	if fieldValue.Kind() == reflect.Slice || fieldValue.Kind() == reflect.Array {
		return serializeSlice(f, fieldValue, values)
	}

	str, err := toString(fieldValue)
	if err != nil {
		return &ValueError{Field: f.Name, Err: err}
	}

	values.Set(f.Key, str)

	return nil
}

func serializeSlice(f *mapper.FieldSchema, fieldValue reflect.Value, values url.Values) error {
	if f.HasComma || f.HasSpace || f.HasPipe {
		return serializeDelimitedSlice(f, fieldValue, values)
	}

	for j := range fieldValue.Len() {
		val := derefPointer(fieldValue.Index(j))
		if !val.IsValid() {
			continue
		}

		strValue, err := toString(val)
		if err != nil {
			return &ValueError{Field: f.Name, Index: j, Err: err}
		}

		values.Add(f.Key, strValue)
	}

	return nil
}

func serializeDelimitedSlice(f *mapper.FieldSchema, fieldValue reflect.Value, values url.Values) error {
	sep := ","
	if f.HasSpace {
		sep = " "
	} else if f.HasPipe {
		sep = "|"
	}

	var sb strings.Builder
	for j := range fieldValue.Len() {
		val := derefPointer(fieldValue.Index(j))
		if !val.IsValid() {
			continue
		}

		str, err := toString(val)
		if err != nil {
			return &ValueError{Field: f.Name, Index: j, Err: err}
		}

		if j > 0 {
			sb.WriteString(sep)
		}

		sb.WriteString(str)
	}

	values.Set(f.Key, sb.String())

	return nil
}

func derefPointer(val reflect.Value) reflect.Value {
	if val.Kind() == reflect.Pointer {
		if val.IsNil() {
			return reflect.Value{}
		}

		return val.Elem()
	}

	return val
}

// StructToValues encodes a struct or Protobuf message into [url.Values].
func StructToValues(s any) (url.Values, error) {
	if s == nil {
		return nil, nil
	}

	switch v := s.(type) {
	case url.Values:
		return v, nil
	case QueryEncoder:
		return v.EncodeValues(), nil
	case interface{ EncodeValues() (url.Values, error) }:
		return v.EncodeValues()
	case proto.Message:
		return protoToValues(v)
	case map[string]string:
		res := make(url.Values, len(v))
		for k, val := range v {
			res.Set(k, val)
		}

		return res, nil

	case map[string][]string:
		res := make(url.Values, len(v))
		for k, val := range v {
			res[k] = slices.Clone(val)
		}

		return res, nil
	}

	v := reflect.ValueOf(s)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil, nil
		}

		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return nil, ErrUnsupportedType
	}

	schema := getStructSchema(v.Type())
	values := make(url.Values)

	if err := fillValues(schema, v, values); err != nil {
		return nil, err
	}

	return values, nil
}

// FastQueryEncoder is implemented by types capable of encoding URL query strings directly.
type FastQueryEncoder interface {
	EncodeQueryString() (string, error)
}

// StructToQueryString encodes a struct or map directly into a raw URL query string (e.g. "key=val&a=b").
func StructToQueryString(s any) (string, error) {
	if s == nil {
		return "", nil
	}

	if fqe, ok := s.(FastQueryEncoder); ok {
		return fqe.EncodeQueryString()
	}

	if v, ok := s.(url.Values); ok {
		return v.Encode(), nil
	}

	if m, ok := s.(map[string]string); ok {
		return encodeMapDirect(m), nil
	}

	v := reflect.ValueOf(s)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return "", nil
		}

		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return "", ErrUnsupportedType
	}

	schema := getStructSchema(v.Type())

	var sb strings.Builder
	sb.Grow(len(schema.Fields) * 20)

	if err := writeQueryString(schema, v, &sb); err != nil {
		return "", err
	}

	return sb.String(), nil
}

func encodeMapDirect(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.Grow(len(m) * 20)

	first := true
	for k, val := range m {
		if !first {
			sb.WriteByte('&')
		}

		sb.WriteString(url.QueryEscape(k))
		sb.WriteByte('=')
		sb.WriteString(url.QueryEscape(val))

		first = false
	}

	return sb.String()
}

func writeQueryString(s *mapper.StructSchema, v reflect.Value, sb *strings.Builder) error {
	first := true

	for i := range s.Fields {
		f := &s.Fields[i]
		val := v.Field(f.Index)

		if val.Kind() == reflect.Pointer {
			if val.IsNil() {
				if f.DefaultVal != "" && f.Key != "" && f.Key != "-" {
					writeQueryKeyValuePair(sb, f.Key, f.DefaultVal, &first)
				}

				continue
			}

			val = val.Elem()
		}

		if f.IsIgnored || f.Key == "" || f.Key == "-" {
			continue
		}

		if val.IsZero() {
			if f.DefaultVal != "" {
				writeQueryKeyValuePair(sb, f.Key, f.DefaultVal, &first)
				continue
			}

			if f.OmitEmpty {
				continue
			}
		}

		strVal, err := toString(val)
		if err != nil {
			return &ValueError{Field: f.Name, Err: err}
		}

		writeQueryKeyValuePair(sb, f.Key, strVal, &first)
	}

	return nil
}

func writeQueryKeyValuePair(sb *strings.Builder, key, value string, first *bool) {
	if !*first {
		sb.WriteByte('&')
	}

	sb.WriteString(url.QueryEscape(key))
	sb.WriteByte('=')
	sb.WriteString(url.QueryEscape(value))

	*first = false
}

func protoToValues(pm proto.Message) (url.Values, error) {
	opts := protojson.MarshalOptions{UseProtoNames: true}

	b, err := opts.Marshal(pm)
	if err != nil {
		return nil, &ValueError{Type: "proto.Message", Err: err}
	}

	var rawMap map[string]any
	if err := json.Unmarshal(b, &rawMap); err != nil {
		return nil, &ValueError{Type: "proto.Message", Err: err}
	}

	res := make(url.Values, len(rawMap))
	for k, val := range rawMap {
		switch v := val.(type) {
		case string:
			res.Set(k, v)
		default:
			subJSON, _ := json.Marshal(v)
			res.Set(k, bytesconv.B2S(subJSON))
		}
	}

	return res, nil
}

func hasTextOrStringerRepresentation(v reflect.Value) bool {
	if !v.CanInterface() {
		return false
	}

	val := v.Interface()
	_, hasText := val.(encoding.TextMarshaler)
	_, hasStringer := val.(interface{ String() string })

	return hasText || hasStringer
}

func toString(v reflect.Value) (string, error) {
	if v.CanInterface() {
		val := v.Interface()

		if tm, ok := val.(encoding.TextMarshaler); ok {
			b, err := tm.MarshalText()
			if err != nil {
				return "", err
			}

			return bytesconv.B2S(b), nil
		}

		if s, ok := val.(interface{ String() string }); ok {
			return s.String(), nil
		}
	}

	switch v.Kind() {
	case reflect.String:
		return v.String(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10), nil
	case reflect.Bool:
		return strconv.FormatBool(v.Bool()), nil
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'f', -1, 64), nil
	default:
		return "", ErrUnsupportedType
	}
}
