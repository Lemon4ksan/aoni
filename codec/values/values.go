// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package values

import (
	"net/url"
	"reflect"
	"strings"

	"github.com/lemon4ksan/foundation/refkit"
	"google.golang.org/protobuf/proto"
)

// QueryEncoder is implemented by types that encode themselves directly into [url.Values].
type QueryEncoder interface {
	EncodeValues() url.Values
}

// Encode converts any Go structure, map, or [proto.Message] into [url.Values].
func Encode(v any) (url.Values, error) {
	if v == nil {
		return make(url.Values), nil
	}

	if qe, ok := v.(QueryEncoder); ok {
		return qe.EncodeValues(), nil
	}

	if pm, ok := v.(proto.Message); ok {
		return protoToValues(pm)
	}

	val := refkit.DerefValue(reflect.ValueOf(v))
	if !val.IsValid() {
		return make(url.Values), nil
	}

	var res url.Values

	switch val.Kind() {
	case reflect.Struct:
		s := getStructSchema(val.Type())
		res = make(url.Values, len(s.Fields))
	case reflect.Map:
		res = make(url.Values, val.Len())
	default:
		return nil, ErrUnsupportedType
	}

	if err := EncodeInto(res, v); err != nil {
		return nil, err
	}

	return res, nil
}

// EncodeInto encodes structure or map fields into an existing [url.Values] instance.
func EncodeInto(values url.Values, v any) error {
	if v == nil || values == nil {
		return nil
	}

	if qe, ok := v.(QueryEncoder); ok {
		for k, list := range qe.EncodeValues() {
			for _, item := range list {
				values.Add(k, item)
			}
		}

		return nil
	}

	val := refkit.DerefValue(reflect.ValueOf(v))
	if !val.IsValid() {
		return nil
	}

	if val.Kind() == reflect.Map {
		return encodeMap(values, val)
	}

	if val.Kind() != reflect.Struct {
		return ErrUnsupportedType
	}

	s := getStructSchema(val.Type())

	return encodeStruct(values, s, val)
}

// EncodeQueryString serializes structure or map fields into a URL query string without intermediate allocations (RFC 3986 §3.4).
func EncodeQueryString(v any, sb *strings.Builder) error {
	if v == nil || sb == nil {
		return nil
	}

	if qe, ok := v.(QueryEncoder); ok {
		first := sb.Len() == 0

		for k, list := range qe.EncodeValues() {
			for _, item := range list {
				writeQueryKeyValuePair(sb, k, item, &first)
			}
		}

		return nil
	}

	val := refkit.DerefValue(reflect.ValueOf(v))
	if !val.IsValid() {
		return nil
	}

	if val.Kind() != reflect.Struct {
		vals, err := Encode(v)
		if err != nil {
			return err
		}

		encoded := vals.Encode()
		if len(encoded) > 0 {
			if sb.Len() > 0 {
				sb.WriteByte('&')
			}

			sb.WriteString(encoded)
		}

		return nil
	}

	s := getStructSchema(val.Type())
	sink := &queryStringSink{sb: sb, first: sb.Len() == 0}

	return encodeStruct(sink, s, val)
}

// StructToQueryString serializes structure v into an RFC 3986 §3.4 URL query parameter string.
func StructToQueryString(v any) (string, error) {
	var sb strings.Builder
	if err := EncodeQueryString(v, &sb); err != nil {
		return "", err
	}

	return sb.String(), nil
}
