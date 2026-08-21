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

func writeSchemaModel(buf *bytes.Buffer, rawName string, s *Schema, cfg ImportConfig) {
	name := toPascalCase(rawName)

	if s.Description != "" {
		fmt.Fprintf(buf, "// %s — %s\n", name, strings.ReplaceAll(s.Description, "\n", " "))
	}

	if len(s.Enum) > 0 && (s.Type == nil || s.IsType("string")) {
		fmt.Fprintf(buf, "type %s string\n\n", name)
		fmt.Fprintf(buf, "const (\n")

		for _, enumVal := range s.Enum {
			valStr := fmt.Sprintf("%v", enumVal)
			constName := name + toPascalCase(valStr)
			fmt.Fprintf(buf, "\t%s %s = %q\n", constName, name, valStr)
		}

		fmt.Fprintf(buf, ")\n\n")

		return
	}

	if s.Type != nil && !s.IsType("object") && len(s.Properties) == 0 {
		goType := mapSchemaType(s, cfg)
		fmt.Fprintf(buf, "type %s %s\n\n", name, goType)
		return
	}

	// Struct DTO
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

		fieldName := toPascalCase(pk)
		if fieldName == "" {
			fieldName = "Field"
		}

		if fieldName == name {
			fieldName += "Val"
		}

		fieldType := mapSchemaType(propSchema, cfg)
		if propSchema.Ref != "" {
			refName := toPascalCase(path.Base(propSchema.Ref))
			if propSchema.Type != nil && propSchema.IsType("object") {
				fieldType = "*" + refName
			} else {
				fieldType = refName
			}
		}

		tag := fmt.Sprintf("`json:\"%s,omitempty\"`", pk)
		if requiredMap[pk] {
			tag = fmt.Sprintf("`json:\"%s\"`", pk)
		}

		if propSchema.Description != "" {
			fmt.Fprintf(buf, "\t// %s\n", strings.ReplaceAll(propSchema.Description, "\n", " "))
		}

		fmt.Fprintf(buf, "\t%s %s %s\n", fieldName, fieldType, tag)
	}

	fmt.Fprintf(buf, "}\n\n")
}

func shortTypeName(raw string) string {
	if idx := strings.LastIndex(raw, "/"); idx != -1 {
		dotIdx := strings.LastIndex(raw, ".")
		if dotIdx > idx {
			return path.Base(raw[:dotIdx]) + "." + raw[dotIdx+1:]
		}
	}

	return raw
}

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

	primaryType := s.Type.Primary()

	switch primaryType {
	case "string":
		switch s.Format {
		case "date-time":
			return "time.Time"
		case "date":
			return "string"
		case "binary", "byte":
			return "[]byte"
		default:
			return "string"
		}

	case "integer":
		switch s.Format {
		case "int64":
			return "int64"
		case "uint64":
			return "uint64"
		case "uint32", "uint":
			return "uint32"
		default:
			return "int"
		}

	case "number":
		if s.Format == "float" {
			return "float32"
		}
		return "float64"

	case "boolean":
		return "bool"

	case "array":
		if s.Items != nil {
			if s.Items.Ref != "" {
				return "[]" + toPascalCase(path.Base(s.Items.Ref))
			}

			return "[]" + mapSchemaType(s.Items, cfg)
		}

		return "[]any"

	case "object":
		if s.AdditionalProperties != nil {
			valType := "any"
			if apSchema, ok := s.AdditionalProperties.(*Schema); ok && apSchema != nil {
				if apSchema.Ref != "" {
					valType = toPascalCase(path.Base(apSchema.Ref))
				} else {
					valType = mapSchemaType(apSchema, cfg)
				}
			}
			return "map[string]" + valType
		}

		return "map[string]any"

	default:
		return "any"
	}
}
