// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package values

import (
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"github.com/lemon4ksan/foundation/codec/json"
	"github.com/lemon4ksan/foundation/net/urlkit"
	"github.com/lemon4ksan/foundation/refkit"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni/internal/mapper"
)

// valueSink models any destination that receives encoded URL query parameters or form fields.
type valueSink interface {
	Set(key, value string)
	Add(key, value string)
}

// queryStringSink buffers query parameter key-value pairs directly into [strings.Builder]
// with zero heap allocations.
type queryStringSink struct {
	sb    *strings.Builder
	first bool
}

func (s *queryStringSink) Set(key, value string) {
	writeQueryKeyValuePair(s.sb, key, value, &s.first)
}

func (s *queryStringSink) Add(key, value string) {
	writeQueryKeyValuePair(s.sb, key, value, &s.first)
}

// getStructSchema resolves cached struct field metadata schema for type t.
func getStructSchema(t reflect.Type) *mapper.StructSchema {
	return mapper.DefaultSchemaCache.GetSchema(t)
}

// encodeStruct populates sink with query key-value pairs derived from struct value v according to schema s.
func encodeStruct[S valueSink](sink S, s *mapper.StructSchema, v reflect.Value) error {
	for i := range s.Fields {
		if err := encodeField(sink, &s.Fields[i], v.Field(s.Fields[i].Index)); err != nil {
			return err
		}
	}

	return nil
}

// encodeField serializes an individual struct field into sink based on tag rules and default values.
func encodeField[S valueSink](sink S, f *mapper.FieldSchema, fieldValue reflect.Value) error {
	if refkit.IsNil(fieldValue) {
		if f.DefaultVal != "" && f.Key != "" && f.Key != "-" {
			sink.Set(f.Key, f.DefaultVal)
		}

		return nil
	}

	fieldValue = refkit.DerefValue(fieldValue)
	if !fieldValue.IsValid() {
		return nil
	}

	if (f.IsAnonymous || f.IsInline) && fieldValue.Kind() == reflect.Struct {
		if f.SubSchema != nil {
			return encodeStruct(sink, f.SubSchema, fieldValue)
		}

		return encodeStruct(sink, getStructSchema(fieldValue.Type()), fieldValue)
	}

	if f.IsIgnored || f.Key == "" || f.Key == "-" {
		return nil
	}

	if shouldSkipZeroValue(f, fieldValue, sink) {
		return nil
	}

	return serializeValue(sink, f, fieldValue)
}

// shouldSkipZeroValue checks whether a zero-value field should be omitted or assigned its default value.
func shouldSkipZeroValue[S valueSink](f *mapper.FieldSchema, fieldValue reflect.Value, sink S) bool {
	if !refkit.IsZero(fieldValue) {
		return false
	}

	if f.DefaultVal != "" {
		sink.Set(f.Key, f.DefaultVal)
		return true
	}

	return f.OmitEmpty
}

// serializeValue converts a concrete field value into its string representation and registers it in sink.
func serializeValue[S valueSink](sink S, f *mapper.FieldSchema, fieldValue reflect.Value) error {
	switch fieldValue.Kind() {
	case reflect.String:
		if fieldValue.Type().PkgPath() == "" {
			sink.Set(f.Key, fieldValue.String())
			return nil
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if fieldValue.Type().PkgPath() == "" {
			sink.Set(f.Key, strconv.FormatInt(fieldValue.Int(), 10))
			return nil
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if fieldValue.Type().PkgPath() == "" {
			sink.Set(f.Key, strconv.FormatUint(fieldValue.Uint(), 10))
			return nil
		}
	case reflect.Bool:
		if fieldValue.Type().PkgPath() == "" {
			sink.Set(f.Key, strconv.FormatBool(fieldValue.Bool()))
			return nil
		}
	case reflect.Float32, reflect.Float64:
		if fieldValue.Type().PkgPath() == "" {
			sink.Set(f.Key, strconv.FormatFloat(fieldValue.Float(), 'f', -1, 64))
			return nil
		}
	}

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

		sink.Set(f.Key, bytesconv.B2S(b))

		return nil
	}

	if refkit.ValueHasTextRepresentation(fieldValue) {
		str, err := refkit.ValueToString(fieldValue)
		if err != nil {
			return &ValueError{Field: f.Name, Err: err}
		}

		sink.Set(f.Key, str)

		return nil
	}

	if fieldValue.Kind() == reflect.Struct || fieldValue.Kind() == reflect.Map {
		b, err := json.Marshal(val)
		if err != nil {
			return &ValueError{Field: f.Name, Err: err}
		}

		sink.Set(f.Key, bytesconv.B2S(b))

		return nil
	}

	if fieldValue.Kind() == reflect.Slice || fieldValue.Kind() == reflect.Array {
		return encodeSlice(sink, f, fieldValue)
	}

	str, err := refkit.ValueToString(fieldValue)
	if err != nil {
		return &ValueError{Field: f.Name, Err: ErrUnsupportedType}
	}

	sink.Set(f.Key, str)

	return nil
}

// encodeSlice serializes a slice or array field into sink.
func encodeSlice[S valueSink](sink S, f *mapper.FieldSchema, fieldValue reflect.Value) error {
	if f.HasComma || f.HasSpace || f.HasPipe {
		return encodeDelimitedSlice(sink, f, fieldValue)
	}

	for j := range fieldValue.Len() {
		val := refkit.DerefPointer(fieldValue.Index(j))
		if !val.IsValid() {
			continue
		}

		strValue, err := refkit.ValueToString(val)
		if err != nil {
			return &ValueError{Field: f.Name, Index: j, Err: ErrUnsupportedType}
		}

		sink.Add(f.Key, strValue)
	}

	return nil
}

// encodeDelimitedSlice joins slice elements with configured delimiter (comma, space, or pipe) and records in sink.
func encodeDelimitedSlice[S valueSink](sink S, f *mapper.FieldSchema, fieldValue reflect.Value) error {
	sep := ","
	switch {
	case f.HasSpace:
		sep = " "
	case f.HasPipe:
		sep = "|"
	}

	var sb strings.Builder
	for j := range fieldValue.Len() {
		val := refkit.DerefPointer(fieldValue.Index(j))
		if !val.IsValid() {
			continue
		}

		str, err := refkit.ValueToString(val)
		if err != nil {
			return &ValueError{Field: f.Name, Index: j, Err: ErrUnsupportedType}
		}

		if j > 0 {
			sb.WriteString(sep)
		}

		sb.WriteString(str)
	}

	sink.Set(f.Key, sb.String())

	return nil
}

// encodeMap iterates over map key-value pairs and writes them to sink.
func encodeMap[S valueSink](sink S, val reflect.Value) error {
	iter := val.MapRange()
	for iter.Next() {
		keyStr, err := refkit.ValueToString(iter.Key())
		if err != nil {
			return &ValueError{Type: "map", Err: ErrUnsupportedType}
		}

		elemVal := refkit.DerefValue(iter.Value())
		if !elemVal.IsValid() {
			continue
		}

		if elemVal.Kind() == reflect.Slice || elemVal.Kind() == reflect.Array {
			for j := range elemVal.Len() {
				valStr, err := refkit.ValueToString(elemVal.Index(j))
				if err != nil {
					return &ValueError{Field: keyStr, Index: j, Err: ErrUnsupportedType}
				}

				sink.Add(keyStr, valStr)
			}
		} else {
			valStr, err := refkit.ValueToString(elemVal)
			if err != nil {
				return &ValueError{Field: keyStr, Err: ErrUnsupportedType}
			}

			sink.Add(keyStr, valStr)
		}
	}

	return nil
}

// writeQueryKeyValuePair appends a percent-encoded key=value pair to sb using a stack-allocated buffer (RFC 3986 §2.1 & §3.4).
// It encodes reserved delimiters while preserving unreserved characters (RFC 3986 §2.2 & §2.3) with zero heap allocations.
func writeQueryKeyValuePair(sb *strings.Builder, key, value string, first *bool) {
	if !*first {
		sb.WriteByte('&')
	}

	var tmpBuf [64]byte

	buf := urlkit.AppendQueryEscapeString(tmpBuf[:0], key)
	buf = append(buf, '=')
	buf = urlkit.AppendQueryEscapeString(buf, value)

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
