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

		writeSchemaModel(buf, k, s, schemas, cfg)
	}
}

// writeSchemaModel dispatches schema generation to enum, union, primitive alias, or struct generator.
//
// # References
//   - OpenAPI 3.1.0 §4.8.24 Schema Object: https://spec.openapis.org/oas/v3.1.0#schema-object
//   - OpenAPI 3.1.0 §4.8.25 Discriminator Object: https://spec.openapis.org/oas/v3.1.0#discriminator-object
//   - JSON Schema draft 2020-12 §Validation: https://json-schema.org/draft/2020-12/json-schema-validation.html
func writeSchemaModel(buf *bytes.Buffer, rawName string, s *Schema, allSchemas map[string]*Schema, cfg ImportConfig) {
	name := toPascalCase(rawName)

	if s.Description != "" {
		fmt.Fprintf(buf, "// %s — %s\n", name, strings.ReplaceAll(s.Description, "\n", " "))
	}

	if isStringEnum(s) {
		writeEnumModel(buf, name, s)
		return
	}

	if isUnionModel(s) {
		writeUnionModel(buf, name, s, cfg)
		return
	}

	if isPrimitiveAlias(s) {
		goType := mapSchemaType(s, cfg)
		fmt.Fprintf(buf, "type %s %s\n\n", name, goType)
		return
	}

	writeStructModel(buf, name, s, allSchemas, cfg)
}

func isStringEnum(s *Schema) bool {
	return len(s.Enum) > 0 && (len(s.Type) == 0 || s.IsType("string"))
}

// isUnionModel reports whether the schema is a polymorphic union (oneOf / anyOf) without direct properties.
func isUnionModel(s *Schema) bool {
	return (len(s.OneOf) > 0 || len(s.AnyOf) > 0) && len(s.Properties) == 0 && len(s.AllOf) == 0
}

func isPrimitiveAlias(s *Schema) bool {
	return len(s.Type) > 0 && !s.IsType("object") && len(s.Properties) == 0 && len(s.AllOf) == 0
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

// writeUnionModel generates a tagged union struct for polymorphic oneOf / anyOf schemas.
//
// # References
//   - OpenAPI 3.1.0 §4.8.25 Discriminator Object: https://spec.openapis.org/oas/v3.1.0#discriminator-object
func writeUnionModel(buf *bytes.Buffer, name string, s *Schema, cfg ImportConfig) {
	fmt.Fprintf(buf, "// @aoni:union")

	if s.Discriminator != nil && s.Discriminator.PropertyName != "" {
		fmt.Fprintf(buf, " discriminator=%s", s.Discriminator.PropertyName)
	}

	fmt.Fprintf(buf, "\n")
	fmt.Fprintf(buf, "type %s struct {\n", name)

	if s.Discriminator != nil && s.Discriminator.PropertyName != "" {
		discField := toPascalCase(s.Discriminator.PropertyName)
		fmt.Fprintf(buf, "\t%s string `json:\"%s\"`\n", discField, s.Discriminator.PropertyName)
	}

	variants := s.OneOf
	if len(variants) == 0 {
		variants = s.AnyOf
	}

	for _, v := range variants {
		if v == nil {
			continue
		}

		if v.Ref != "" {
			vName := toPascalCase(path.Base(v.Ref))
			fmt.Fprintf(buf, "\t%s *%s `json:\"%s,omitempty\"`\n", vName, vName, toSnakeCase(vName))
		}
	}

	fmt.Fprintf(buf, "}\n\n")
}

// writeStructModel generates a Go struct definition from schema properties and allOf inheritance.
//
// # References
//   - OpenAPI 3.1.0 §4.8.24 Schema Object (allOf composition): https://spec.openapis.org/oas/v3.1.0#schema-object
func writeStructModel(buf *bytes.Buffer, name string, s *Schema, allSchemas map[string]*Schema, cfg ImportConfig) {
	fmt.Fprintf(buf, "// @aoni:dto casing=snake_case omitempty=true\n")
	fmt.Fprintf(buf, "type %s struct {\n", name)

	allProps, allRequired := collectAllProperties(s, allSchemas)
	propKeys := generic.Keys(allProps)
	slices.Sort(propKeys)

	requiredMap := make(map[string]bool, len(allRequired))
	for _, req := range allRequired {
		requiredMap[req] = true
	}

	for _, pk := range propKeys {
		propSchema := allProps[pk]
		if propSchema == nil {
			continue
		}

		writeStructField(buf, name, pk, propSchema, requiredMap[pk], cfg)
	}

	fmt.Fprintf(buf, "}\n\n")
}

func collectAllProperties(s *Schema, allSchemas map[string]*Schema) (map[string]*Schema, []string) {
	props := make(map[string]*Schema)
	required := slices.Clone(s.Required)

	for k, v := range s.Properties {
		props[k] = v
	}

	for _, sub := range s.AllOf {
		if sub == nil {
			continue
		}

		target := sub
		if sub.Ref != "" && allSchemas != nil {
			refName := path.Base(sub.Ref)
			if resolved, ok := allSchemas[refName]; ok && resolved != nil {
				target = resolved
			}
		}

		subProps, subReq := collectAllProperties(target, allSchemas)
		for k, v := range subProps {
			if _, exists := props[k]; !exists {
				props[k] = v
			}
		}

		required = append(required, subReq...)
	}

	return props, required
}

func writeStructField(
	buf *bytes.Buffer,
	structName, propKey string,
	propSchema *Schema,
	isRequired bool,
	cfg ImportConfig,
) {
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

// deriveFieldType resolves the Go type for a schema property.
//
// Quirk (Circular References / $ref recursion): We emit pointers (`*RefType`) for referenced models.
// This prevents Go compiler "invalid recursive type" sizing errors on self-referential or mutually
// recursive schemas (such as TreeNode, Parent/Child, or Comment trees).
func deriveFieldType(propSchema *Schema, cfg ImportConfig) string {
	if propSchema.Ref == "" {
		return mapSchemaType(propSchema, cfg)
	}

	refName := toPascalCase(path.Base(propSchema.Ref))

	return "*" + refName
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
		return "[]*" + toPascalCase(path.Base(items.Ref))
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
		return "map[string]*" + toPascalCase(path.Base(apSchema.Ref))
	}

	return "map[string]" + mapSchemaType(apSchema, cfg)
}
