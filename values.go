// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
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
		// Attempt to parse any other number; non-zero is true
		val, err := strconv.Atoi(s)
		if err == nil {
			*bi = val != 0
			return nil
		}

		*bi = false
	}

	return nil
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

// Time returns the [time.Time] value.
func (t *UnixTimestamp) Time() time.Time {
	return time.Time(*t)
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

// Validate checks struct fields for the "validate:required" tag.
// It returns a [ValidationError] for the first missing required field.
func Validate(s any) error {
	if s == nil {
		return nil
	}

	v := reflect.ValueOf(s)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return nil
	}

	return validateValue(v, "")
}

func validateValue(v reflect.Value, parent string) error {
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		fieldValue := v.Field(i)

		name := field.Name
		if parent != "" {
			name = parent + "." + name
		}

		validateTag := field.Tag.Get("validate")
		if validateTag == "required" && fieldValue.IsZero() {
			return &ValidationError{Field: name}
		}

		if fieldValue.Kind() == reflect.Struct {
			if err := validateValue(fieldValue, name); err != nil {
				return err
			}
		}

		if fieldValue.Kind() == reflect.Pointer && !fieldValue.IsNil() {
			elem := fieldValue.Elem()
			if elem.Kind() == reflect.Struct {
				if err := validateValue(elem, name); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func fillValues(v reflect.Value, values url.Values) error {
	t := v.Type()
	for i := range v.NumField() {
		field := t.Field(i)
		fieldValue := v.Field(i)

		defaultVal := field.Tag.Get("default")

		if fieldValue.Kind() == reflect.Pointer {
			if fieldValue.IsNil() {
				if defaultVal != "" {
					tag := field.Tag.Get("url")
					if tag == "" {
						tag = field.Tag.Get("json")
					}

					key := strings.Split(tag, ",")[0]
					if key != "" && key != "-" {
						values.Set(key, defaultVal)
					}
				}

				continue
			}

			fieldValue = fieldValue.Elem()
		}

		tag := field.Tag.Get("url")
		if tag == "" {
			tag = field.Tag.Get("json")
		}

		parts := strings.Split(tag, ",")
		key := parts[0]

		isInline := slices.Contains(parts[1:], "inline")
		if (field.Anonymous || isInline) && fieldValue.Kind() == reflect.Struct {
			if err := fillValues(fieldValue, values); err != nil {
				return err
			}

			continue
		}

		if key == "" || key == "-" {
			continue
		}

		omitempty := len(parts) > 1 && (parts[1] == "omitempty" || slices.Contains(parts[1:], "omitempty"))
		if omitempty && fieldValue.IsZero() {
			continue
		}

		if fieldValue.IsZero() {
			if defaultVal != "" {
				values.Set(key, defaultVal)
				continue
			}

			if omitempty {
				continue
			}
		}

		if fieldValue.Kind() == reflect.Slice || fieldValue.Kind() == reflect.Array {
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
					return fmt.Errorf("field %s[%d]: %w", field.Name, j, err)
				}

				values.Add(key, strValue)
			}

			continue
		}

		strValue, err := toString(fieldValue)
		if err != nil {
			return fmt.Errorf("field %s: %w", field.Name, err)
		}

		values.Set(key, strValue)
	}

	return nil
}

func toString(v reflect.Value) (string, error) {
	if v.CanInterface() {
		if s, ok := v.Interface().(interface{ String() string }); ok {
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
