// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package mapper provides high-performance zero-allocation reflection schema caching and struct field mapping.
package mapper

import (
	"reflect"
	"slices"
	"strings"

	"github.com/lemon4ksan/foundation/generic"
)

// FieldSchema describes a pre-computed struct field index, tag options, and nested sub-schemas.
type FieldSchema struct {
	SubSchema   *StructSchema // Parsed sub-schema for embedded/anonymous or inlined struct fields
	Name        string        // Go struct field identifier
	Key         string        // Serialized query/json key extracted from struct tags
	DefaultVal  string        // Default fallback value from the `default` tag
	Index       int           // Zero-based index within the struct definition
	IsInline    bool          // True if tagged with `inline` for flattened sub-fields
	IsAnonymous bool          // True if the field is an embedded anonymous struct
	OmitEmpty   bool          // True if tagged with `omitempty`
	HasComma    bool          // True if slice values should be formatted as comma-separated values
	HasSpace    bool          // True if slice values should be formatted as space-separated values
	HasPipe     bool          // True if slice values should be formatted as pipe-separated values
	IsIgnored   bool          // True if tagged with `json:"-"` / `url:"-"` or unexported
}

// StructSchema holds pre-computed field metadata for a target struct type.
type StructSchema struct {
	Fields []FieldSchema // Ordered slice of pre-computed field reflection descriptors
}

// SchemaCache caches reflection struct schemas by [reflect.Type] using [generic.ConcurrentMap] to eliminate runtime reflection overhead.
type SchemaCache struct {
	cache generic.ConcurrentMap[reflect.Type, *StructSchema]
}

// DefaultSchemaCache is the global default schema cache instance.
var DefaultSchemaCache = &SchemaCache{}

// GetSchema returns or computes the cached [StructSchema] for type t.
func (s *SchemaCache) GetSchema(t reflect.Type) *StructSchema {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return nil
	}

	if cached, ok := s.cache.Load(t); ok {
		return cached
	}

	schema := BuildStructSchema(t)
	cached, _ := s.cache.LoadOrStore(t, schema)

	return cached
}

// BuildStructSchema parses [reflect.Type] t and constructs a pre-computed [StructSchema].
func BuildStructSchema(t reflect.Type) *StructSchema {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return nil
	}

	numField := t.NumField()
	fields := make([]FieldSchema, 0, numField)

	for i := range numField {
		field := t.Field(i)
		defaultVal := field.Tag.Get("default")

		tag := field.Tag.Get("url")
		if tag == "" {
			tag = field.Tag.Get("json")
		}

		parts := strings.Split(tag, ",")
		key := parts[0]

		fSchema := FieldSchema{
			Index:       i,
			Name:        field.Name,
			Key:         key,
			DefaultVal:  defaultVal,
			IsInline:    slices.Contains(parts[1:], "inline"),
			IsAnonymous: field.Anonymous,
			OmitEmpty:   slices.Contains(parts[1:], "omitempty"),
			HasComma:    slices.Contains(parts[1:], "comma"),
			HasSpace:    slices.Contains(parts[1:], "space"),
			HasPipe:     slices.Contains(parts[1:], "pipe"),
			IsIgnored:   key == "-",
		}

		fieldType := field.Type
		if fieldType.Kind() == reflect.Pointer {
			fieldType = fieldType.Elem()
		}

		if (field.Anonymous || fSchema.IsInline) && fieldType.Kind() == reflect.Struct {
			fSchema.SubSchema = BuildStructSchema(fieldType)
		} else if key == "" && !field.Anonymous && !fSchema.IsInline {
			fSchema.IsIgnored = true
		}

		fields = append(fields, fSchema)
	}

	return &StructSchema{Fields: fields}
}
