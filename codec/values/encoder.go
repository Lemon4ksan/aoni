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

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni/internal/bytesconv"
	"github.com/lemon4ksan/aoni/internal/mapper"
	"github.com/lemon4ksan/aoni/internal/urlutil"
)

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

func writeQueryKeyValuePair(sb *strings.Builder, key, value string, first *bool) {
	if !*first {
		sb.WriteByte('&')
	}

	buf := make([]byte, 0, len(key)+len(value)+16)
	buf = urlutil.AppendQueryEscapeString(buf, key)
	buf = append(buf, '=')
	buf = urlutil.AppendQueryEscapeString(buf, value)

	sb.Write(buf)

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

func derefPointer(v reflect.Value) reflect.Value {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return reflect.Value{}
		}

		v = v.Elem()
	}

	return v
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
