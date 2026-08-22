// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package values

import (
	"encoding"
	"encoding/json"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	furl "github.com/lemon4ksan/foundation/net/url"
	"github.com/lemon4ksan/foundation/refkit"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni/internal/mapper"
)

// getStructSchema resolves cached struct field metadata schema for type t.
func getStructSchema(t reflect.Type) *mapper.StructSchema {
	return mapper.DefaultSchemaCache.GetSchema(t)
}

// fillValues populates target url.Values with query key-value pairs derived from struct value v.
func fillValues(s *mapper.StructSchema, v reflect.Value, values url.Values) error {
	for i := range s.Fields {
		if err := fillField(&s.Fields[i], v.Field(s.Fields[i].Index), values); err != nil {
			return err
		}
	}

	return nil
}

// fillField serializes an individual struct field into url.Values based on tag rules and default values.
func fillField(f *mapper.FieldSchema, fieldValue reflect.Value, values url.Values) error {
	if refkit.IsNil(fieldValue) {
		if f.DefaultVal != "" && f.Key != "" && f.Key != "-" {
			values.Set(f.Key, f.DefaultVal)
		}

		return nil
	}

	fieldValue = refkit.DerefValue(fieldValue)
	if !fieldValue.IsValid() {
		return nil
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

// shouldSkipZeroValue checks whether a zero-value field should be omitted or assigned its default value.
func shouldSkipZeroValue(f *mapper.FieldSchema, fieldValue reflect.Value, values url.Values) bool {
	if !refkit.IsZero(fieldValue) {
		return false
	}

	if f.DefaultVal != "" {
		values.Set(f.Key, f.DefaultVal)
		return true
	}

	return f.OmitEmpty
}

// serializeValue converts a concrete field value into its string representation and sets it in values.
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

// serializeSlice serializes a slice or array field into multiple query parameter entries.
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

// serializeDelimitedSlice joins slice element values with a configured delimiter (comma, space, or pipe).
func serializeDelimitedSlice(f *mapper.FieldSchema, fieldValue reflect.Value, values url.Values) error {
	sep := ","
	switch {
	case f.HasSpace:
		sep = " "
	case f.HasPipe:
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

// writeQueryKeyValuePair appends a percent-encoded key=value pair to sb using a stack-allocated buffer (RFC 3986 §2.1 & §3.4).
// It encodes reserved delimiters while preserving unreserved characters (RFC 3986 §2.2 & §2.3) with zero heap allocations.
func writeQueryKeyValuePair(sb *strings.Builder, key, value string, first *bool) {
	if !*first {
		sb.WriteByte('&')
	}

	var tmpBuf [64]byte

	buf := furl.AppendQueryEscapeString(tmpBuf[:0], key)
	buf = append(buf, '=')
	buf = furl.AppendQueryEscapeString(buf, value)

	sb.Write(buf)

	*first = false
}

// protoToValues encodes a Protocol Buffer message into url.Values via protojson.
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

// derefPointer unwraps nested pointer values until reaching a concrete value or nil pointer.
func derefPointer(v reflect.Value) reflect.Value {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return reflect.Value{}
		}

		v = v.Elem()
	}

	return v
}

// hasTextOrStringerRepresentation reports whether value v implements encoding.TextMarshaler or fmt.Stringer.
func hasTextOrStringerRepresentation(v reflect.Value) bool {
	if !v.CanInterface() {
		return false
	}

	val := v.Interface()
	_, hasText := val.(encoding.TextMarshaler)
	_, hasStringer := val.(interface{ String() string })

	return hasText || hasStringer
}

// toString formats primitive values, TextMarshalers, and Stringers into a string.
func toString(v reflect.Value) (string, error) {
	for v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return "", nil
		}

		v = v.Elem()
	}

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
