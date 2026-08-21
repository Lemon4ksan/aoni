// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package openapi

import (
	"bytes"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/lemon4ksan/foundation/generic"
)

// writeSchemas translates OpenAPI Component Schemas into typed Go DTO structs, enums, or type aliases.
//
// References:
//   - OpenAPI 3.1.0 §4.8.7 Components Object: https://spec.openapis.org/oas/v3.1.0#components-object
//   - OpenAPI 3.1.0 §4.8.24 Schema Object: https://spec.openapis.org/oas/v3.1.0#schema-object
//   - Swagger 2.0 §5.17 Schema Object: https://swagger.io/specification/v2/#schema-object
func writeSchemas(buf *bytes.Buffer, schemas map[string]*Schema, cfg ImportConfig) {
	keys := generic.Keys(schemas)
	slices.Sort(keys)

	for _, k := range keys {
		s := schemas[k]
		if s == nil {
			continue
		}
		writeSchemaModel(buf, k, s, cfg)
	}
}

// writeSchemaModel dispatches schema generation to enum, primitive alias, or struct generator.
//
// References:
//   - OpenAPI 3.1.0 §4.8.24 Schema Object: https://spec.openapis.org/oas/v3.1.0#schema-object
//   - JSON Schema draft 2020-12 §Validation: https://json-schema.org/draft/2020-12/json-schema-validation.html
func writeSchemaModel(buf *bytes.Buffer, rawName string, s *Schema, cfg ImportConfig) {
	name := toPascalCase(rawName)

	if s.Description != "" {
		fmt.Fprintf(buf, "// %s — %s\n", name, strings.ReplaceAll(s.Description, "\n", " "))
	}

	if isStringEnum(s) {
		writeEnumModel(buf, name, s)
		return
	}

	if isPrimitiveAlias(s) {
		goType := mapSchemaType(s, cfg)
		fmt.Fprintf(buf, "type %s %s\n\n", name, goType)
		return
	}

	writeStructModel(buf, name, s, cfg)
}

func isStringEnum(s *Schema) bool {
	return len(s.Enum) > 0 && (len(s.Type) == 0 || s.IsType("string"))
}

func isPrimitiveAlias(s *Schema) bool {
	return len(s.Type) > 0 && !s.IsType("object") && len(s.Properties) == 0
}

func writeEnumModel(buf *bytes.Buffer, name string, s *Schema) {
	fmt.Fprintf(buf, "type %s string\n\n", name)
	fmt.Fprintf(buf, "const (\n")

	for _, enumVal := range s.Enum {
		valStr := fmt.Sprintf("%v", enumVal)
		constName := name + toPascalCase(valStr)
		fmt.Fprintf(buf, "\t%s %s = %q\n", constName, name, valStr)
	}

	fmt.Fprintf(buf, ")\n\n")
}

func writeStructModel(buf *bytes.Buffer, name string, s *Schema, cfg ImportConfig) {
	fmt.Fprintf(buf, "// @aoni:dto casing=snake_case omitempty=true\n")
	fmt.Fprintf(buf, "type %s struct {\n", name)

	propKeys := generic.Keys(s.Properties)
	slices.Sort(propKeys)

	requiredMap := make(map[string]bool, len(s.Required))
	for _, req := range s.Required {
		requiredMap[req] = true
	}

	for _, pk := range propKeys {
		propSchema := s.Properties[pk]
		if propSchema == nil {
			continue
		}
		writeStructField(buf, name, pk, propSchema, requiredMap[pk], cfg)
	}

	fmt.Fprintf(buf, "}\n\n")
}

func writeStructField(buf *bytes.Buffer, structName, propKey string, propSchema *Schema, isRequired bool, cfg ImportConfig) {
	fieldName := deriveFieldName(structName, propKey)
	fieldType := deriveFieldType(propSchema, cfg)
	tag := deriveFieldJSONTag(propKey, isRequired)

	if propSchema.Description != "" {
		fmt.Fprintf(buf, "\t// %s\n", strings.ReplaceAll(propSchema.Description, "\n", " "))
	}

	fmt.Fprintf(buf, "\t%s %s %s\n", fieldName, fieldType, tag)
}

func deriveFieldName(structName, propKey string) string {
	fieldName := toPascalCase(propKey)
	if fieldName == "" {
		fieldName = "Field"
	}
	if fieldName == structName {
		fieldName += "Val"
	}
	return fieldName
}

func deriveFieldType(propSchema *Schema, cfg ImportConfig) string {
	if propSchema.Ref == "" {
		return mapSchemaType(propSchema, cfg)
	}

	refName := toPascalCase(path.Base(propSchema.Ref))
	if propSchema.IsType("object") {
		return "*" + refName
	}
	return refName
}

func deriveFieldJSONTag(propKey string, isRequired bool) string {
	if isRequired {
		return fmt.Sprintf("`json:\"%s\"`", propKey)
	}
	return fmt.Sprintf("`json:\"%s,omitempty\"`", propKey)
}

func shortTypeName(raw string) string {
	idx := strings.LastIndex(raw, "/")
	if idx == -1 {
		return raw
	}

	dotIdx := strings.LastIndex(raw, ".")
	if dotIdx > idx {
		return path.Base(raw[:dotIdx]) + "." + raw[dotIdx+1:]
	}

	return raw
}

// mapSchemaType maps an OpenAPI Schema data type and format into an idiomatic Go type.
//
// References:
//   - OpenAPI 3.1.0 §4.8.24.1 Data Types: https://spec.openapis.org/oas/v3.1.0#data-types
//   - Swagger 2.0 §5.18 Data Types: https://swagger.io/specification/v2/#data-types
//   - RFC 3339 §Date and Time on the Internet: https://datatracker.ietf.org/doc/html/rfc3339
func mapSchemaType(s *Schema, cfg ImportConfig) string {
	if s == nil {
		return "any"
	}

	if cfg.TypeMap != nil && s.Title != "" {
		if mapped, ok := cfg.TypeMap[s.Title]; ok {
			return shortTypeName(mapped)
		}
	}

	if len(s.Type) == 0 {
		if len(s.Properties) > 0 {
			return "map[string]any"
		}
		return "any"
	}

	switch s.Type.Primary() {
	case "string":
		return mapStringType(s.Format)
	case "integer":
		return mapIntegerType(s.Format)
	case "number":
		return mapNumberType(s.Format)
	case "boolean":
		return "bool"
	case "array":
		return mapArrayType(s.Items, cfg)
	case "object":
		return mapObjectType(s.AdditionalProperties, cfg)
	default:
		return "any"
	}
}

func mapStringType(format string) string {
	switch format {
	case "date-time":
		return "time.Time"
	case "binary", "byte":
		return "[]byte"
	default:
		return "string"
	}
}

func mapIntegerType(format string) string {
	switch format {
	case "int64":
		return "int64"
	case "uint64":
		return "uint64"
	case "uint32", "uint":
		return "uint32"
	default:
		return "int"
	}
}

func mapNumberType(format string) string {
	if format == "float" {
		return "float32"
	}
	return "float64"
}

func mapArrayType(items *Schema, cfg ImportConfig) string {
	if items == nil {
		return "[]any"
	}
	if items.Ref != "" {
		return "[]" + toPascalCase(path.Base(items.Ref))
	}
	return "[]" + mapSchemaType(items, cfg)
}

func mapObjectType(additionalProps any, cfg ImportConfig) string {
	if additionalProps == nil {
		return "map[string]any"
	}

	apSchema, ok := additionalProps.(*Schema)
	if !ok || apSchema == nil {
		return "map[string]any"
	}

	if apSchema.Ref != "" {
		return "map[string]" + toPascalCase(path.Base(apSchema.Ref))
	}

	return "map[string]" + mapSchemaType(apSchema, cfg)
}
