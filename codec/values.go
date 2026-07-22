// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license can be found in the LICENSE file.

package codec

import (
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Uint64String parses uint64 values from string representations in JSON.
// It safely handles raw integers, JSON null, or empty strings.
type Uint64String uint64

// UnmarshalJSON parses JSON byte data into the [Uint64String] target.
func (u *Uint64String) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*u = 0
		return nil
	}

	val, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return fmt.Errorf("Uint64String: %w", err)
	}

	*u = Uint64String(val)

	return nil
}

// MarshalJSON serializes the [Uint64String] back as a JSON string representation.
func (u Uint64String) MarshalJSON() ([]byte, error) {
	return json.Marshal(strconv.FormatUint(uint64(u), 10))
}

// Int64String parses int64 values from string representations in JSON.
// It safely handles raw integers, JSON null, or empty strings.
type Int64String int64

// UnmarshalJSON parses JSON byte data into the [Int64String] target.
func (i *Int64String) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*i = 0
		return nil
	}

	val, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("Int64String: %w", err)
	}

	*i = Int64String(val)

	return nil
}

// MarshalJSON serializes the [Int64String] back as a JSON string representation.
func (i Int64String) MarshalJSON() ([]byte, error) {
	return json.Marshal(strconv.FormatInt(int64(i), 10))
}

// Float64String parses float64 values from string representations in JSON.
type Float64String float64

// UnmarshalJSON parses JSON byte data into the [Float64String] target.
func (f *Float64String) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}

	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("Float64String: %w", err)
	}

	*f = Float64String(val)

	return nil
}

// MarshalJSON serializes the [Float64String] back as a JSON string representation.
func (f Float64String) MarshalJSON() ([]byte, error) {
	return json.Marshal(strconv.FormatFloat(float64(f), 'f', -1, 64))
}

// BoolInt parses booleans from integers or strings in JSON.
// It maps 1, "1", "true" to true and 0, "0", "false", "null" to false.
type BoolInt bool

// UnmarshalJSON implements json.Unmarshaler.
func (bi *BoolInt) UnmarshalJSON(b []byte) error {
	s := strings.ToLower(strings.Trim(string(b), `"`))
	switch s {
	case "1", "true":
		*bi = true
	case "0", "false", "", "null":
		*bi = false
	default:
		val, err := strconv.Atoi(s)
		if err == nil {
			*bi = val != 0
			return nil
		}

		*bi = false
	}

	return nil
}

// MarshalJSON serializes [BoolInt] back as numeric "1" or "0" JSON representations.
func (bi BoolInt) MarshalJSON() ([]byte, error) {
	if bi {
		return []byte("1"), nil
	}

	return []byte("0"), nil
}

// UnixTimestamp parses Unix timestamps from strings or numbers in JSON.
type UnixTimestamp time.Time

// UnmarshalJSON implements json.Unmarshaler.
func (t *UnixTimestamp) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" || s == "0" {
		*t = UnixTimestamp(time.Time{})
		return nil
	}

	unix, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("UnixTimestamp: %w", err)
	}

	*t = UnixTimestamp(time.Unix(unix, 0).UTC())

	return nil
}

// MarshalJSON serializes the [UnixTimestamp] back as a numeric Unix epoch timestamp.
func (t UnixTimestamp) MarshalJSON() ([]byte, error) {
	if time.Time(t).IsZero() {
		return []byte("0"), nil
	}

	return []byte(strconv.FormatInt(time.Time(t).Unix(), 10)), nil
}

// Time returns the [time.Time] value.
func (t UnixTimestamp) Time() time.Time {
	return time.Time(t)
}

// RFC3339Timestamp parses ISO-8601 / RFC-3339 formatted date-time strings in JSON.
// It safely handles JSON null, empty strings, and outputs RFC-3339 strings on marshal.
type RFC3339Timestamp time.Time

// UnmarshalJSON implements json.Unmarshaler.
func (t *RFC3339Timestamp) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*t = RFC3339Timestamp(time.Time{})
		return nil
	}

	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return fmt.Errorf("RFC3339Timestamp: %w", err)
	}

	*t = RFC3339Timestamp(parsed.UTC())

	return nil
}

// MarshalJSON implements json.Marshaler.
func (t RFC3339Timestamp) MarshalJSON() ([]byte, error) {
	if time.Time(t).IsZero() {
		return []byte("null"), nil
	}

	return json.Marshal(time.Time(t).Format(time.RFC3339))
}

// Time returns the underlying standard time.Time representation.
func (t RFC3339Timestamp) Time() time.Time {
	return time.Time(t)
}

// String returns the RFC-3339 formatted representation.
func (t RFC3339Timestamp) String() string {
	if time.Time(t).IsZero() {
		return ""
	}

	return time.Time(t).Format(time.RFC3339)
}

// StructToValues encodes a struct into [url.Values] using "url" or "json" tags.
// It expands inline structs recursively and supports slices, arrays, and primitive types.
// Returns an error if the input is not a struct or pointer to a struct.
func StructToValues(s any) (url.Values, error) {
	if s == nil {
		return nil, nil
	}

	if vals, ok := s.(url.Values); ok {
		return vals, nil
	}

	v := reflect.ValueOf(s)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return nil, errors.New("unsupported type: input must be a struct or a pointer to a struct")
	}

	values := make(url.Values)
	if err := fillValues(v, values); err != nil {
		return nil, err
	}

	return values, nil
}

func fillValues(v reflect.Value, values url.Values) error {
	t := v.Type()
	for i := range v.NumField() {
		if err := fillField(v.Field(i), t.Field(i), values); err != nil {
			return err
		}
	}

	return nil
}

func fillField(fieldValue reflect.Value, field reflect.StructField, values url.Values) error {
	defaultVal := field.Tag.Get("default")

	if fieldValue.Kind() == reflect.Pointer {
		if fieldValue.IsNil() {
			applyDefaultValue(field, defaultVal, values)
			return nil
		}

		fieldValue = fieldValue.Elem()
	}

	key, parts, isInline := parseFieldTags(field)

	if (field.Anonymous || isInline) && fieldValue.Kind() == reflect.Struct {
		return fillValues(fieldValue, values)
	}

	if key == "" || key == "-" {
		return nil
	}

	if shouldSkipZeroValue(fieldValue, parts, defaultVal, key, values) {
		return nil
	}

	return serializeValue(fieldValue, field.Name, key, parts, values)
}

func parseFieldTags(field reflect.StructField) (string, []string, bool) {
	tag := field.Tag.Get("url")
	if tag == "" {
		tag = field.Tag.Get("json")
	}

	parts := strings.Split(tag, ",")
	key := parts[0]
	isInline := slices.Contains(parts[1:], "inline")

	return key, parts, isInline
}

func applyDefaultValue(field reflect.StructField, defaultVal string, values url.Values) {
	if defaultVal == "" {
		return
	}

	tag := field.Tag.Get("url")
	if tag == "" {
		tag = field.Tag.Get("json")
	}

	key := strings.Split(tag, ",")[0]
	if key != "" && key != "-" {
		values.Set(key, defaultVal)
	}
}

func shouldSkipZeroValue(fieldValue reflect.Value, parts []string, defaultVal, key string, values url.Values) bool {
	omitempty := slices.Contains(parts[1:], "omitempty")
	if omitempty && fieldValue.IsZero() {
		return true
	}

	if fieldValue.IsZero() {
		if defaultVal != "" {
			values.Set(key, defaultVal)
			return true
		}

		if omitempty {
			return true
		}
	}

	return false
}

func serializeValue(fieldValue reflect.Value, fieldName, key string, parts []string, values url.Values) error {
	if hasTextOrStringerRepresentation(fieldValue) {
		str, err := toString(fieldValue)
		if err != nil {
			return fmt.Errorf("field %s: %w", fieldName, err)
		}

		values.Set(key, str)

		return nil
	}

	isStruct := fieldValue.Kind() == reflect.Struct
	isMap := fieldValue.Kind() == reflect.Map

	if isStruct || isMap {
		b, err := json.Marshal(fieldValue.Interface())
		if err != nil {
			return fmt.Errorf("field %s: failed to marshal nested structure to JSON: %w", fieldName, err)
		}

		values.Set(key, string(b))

		return nil
	}

	if fieldValue.Kind() == reflect.Slice || fieldValue.Kind() == reflect.Array {
		return serializeSlice(fieldValue, fieldName, key, parts, values)
	}

	str, err := toString(fieldValue)
	if err != nil {
		return fmt.Errorf("field %s: %w", fieldName, err)
	}

	values.Set(key, str)

	return nil
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

func serializeSlice(fieldValue reflect.Value, fieldName, key string, parts []string, values url.Values) error {
	hasComma := slices.Contains(parts[1:], "comma")
	hasSpace := slices.Contains(parts[1:], "space")
	hasPipe := slices.Contains(parts[1:], "pipe")

	if hasComma || hasSpace || hasPipe {
		var strVals []string
		for j := range fieldValue.Len() {
			val := fieldValue.Index(j)
			if val.Kind() == reflect.Pointer {
				if val.IsNil() {
					continue
				}

				val = val.Elem()
			}

			str, err := toString(val)
			if err != nil {
				return fmt.Errorf("field %s[%d]: %w", fieldName, j, err)
			}

			strVals = append(strVals, str)
		}

		sep := ","
		if hasSpace {
			sep = " "
		} else if hasPipe {
			sep = "|"
		}

		values.Set(key, strings.Join(strVals, sep))

		return nil
	}

	for j := range fieldValue.Len() {
		val := fieldValue.Index(j)
		if val.Kind() == reflect.Pointer {
			if val.IsNil() {
				continue
			}

			val = val.Elem()
		}

		strValue, err := toString(val)
		if err != nil {
			return fmt.Errorf("field %s[%d]: %w", fieldName, j, err)
		}

		values.Add(key, strValue)
	}

	return nil
}

func toString(v reflect.Value) (string, error) {
	if v.CanInterface() {
		val := v.Interface()

		if tm, ok := val.(encoding.TextMarshaler); ok {
			b, err := tm.MarshalText()
			if err != nil {
				return "", err
			}

			return string(b), nil
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
		return "", fmt.Errorf("unsupported type: %s", v.Kind())
	}
}
