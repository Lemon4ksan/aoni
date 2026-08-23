// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ingest

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

// SpecFormat identifies standard API specification and capture formats.
type SpecFormat string

const (
	// FormatOpenAPI3 represents OpenAPI 3.0.x or 3.1.x JSON/YAML.
	FormatOpenAPI3 SpecFormat = "openapi_3"

	// FormatSwagger2 represents Swagger / OpenAPI 2.0 JSON/YAML.
	FormatSwagger2 SpecFormat = "swagger_2"

	// FormatPostman represents Postman Collection v2 / v2.1 JSON files.
	FormatPostman SpecFormat = "postman"

	// FormatHAR represents W3C HTTP Archive (HAR) 1.2 network traffic recordings.
	FormatHAR SpecFormat = "har"

	// FormatUnknown represents unrecognized payload formats.
	FormatUnknown SpecFormat = "unknown"
)

// DetectFormat inspects raw specification bytes and determines its schema format.
func DetectFormat(data []byte) (SpecFormat, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return FormatUnknown, errors.New("empty specification payload")
	}

	// 1. JSON-based heuristics
	if trimmed[0] == '{' {
		var rawMap map[string]any
		if err := json.Unmarshal(trimmed, &rawMap); err == nil {
			if openapiVal, ok := rawMap["openapi"].(string); ok && strings.HasPrefix(openapiVal, "3.") {
				return FormatOpenAPI3, nil
			}

			if swaggerVal, ok := rawMap["swagger"].(string); ok && strings.HasPrefix(swaggerVal, "2.") {
				return FormatSwagger2, nil
			}

			if info, ok := rawMap["info"].(map[string]any); ok {
				if schema, ok := info["schema"].(string); ok && strings.Contains(schema, "postman.com") {
					return FormatPostman, nil
				}
			}

			if log, ok := rawMap["log"].(map[string]any); ok {
				if _, ok := log["entries"]; ok {
					return FormatHAR, nil
				}
			}
		}
	}

	// 2. YAML-based text search heuristics
	str := string(trimmed)
	if strings.Contains(str, "openapi: 3.") || strings.Contains(str, "openapi: \"3.") ||
		strings.Contains(str, "openapi: '3.") {
		return FormatOpenAPI3, nil
	}

	if strings.Contains(str, "swagger: \"2.") || strings.Contains(str, "swagger: '2.") ||
		strings.Contains(str, "swagger: 2.") {
		return FormatSwagger2, nil
	}

	return FormatUnknown, nil
}
