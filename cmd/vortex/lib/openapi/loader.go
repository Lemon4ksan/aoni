// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package openapi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/getkin/kin-openapi/openapi2"
	"github.com/getkin/kin-openapi/openapi2conv"
	"github.com/getkin/kin-openapi/openapi3"
	"gopkg.in/yaml.v3"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/cache"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ingest"
)

// LoadSpec loads an OpenAPI specification using default Union merge mode.
func LoadSpec(filename string, data []byte) (*openapi3.T, error) {
	return LoadSpecWithMode(filename, data, MergeModeUnion)
}

// LoadSpecWithMode loads and combines multiple specifications using the specified MergeMode (union, intersect, diff).
func LoadSpecWithMode(filename string, data []byte, mode MergeMode) (*openapi3.T, error) {
	if len(data) > 0 {
		return loadSingleSpec(filename, data)
	}

	if strings.Contains(filename, ",") {
		parts := strings.Split(filename, ",")

		var allSpecs []*openapi3.T

		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}

			if strings.ContainsAny(part, "*?[]") {
				matches, err := filepath.Glob(part)
				if err != nil {
					return nil, fmt.Errorf("invalid glob pattern %q: %w", part, err)
				}

				for _, match := range matches {
					doc, lErr := loadSingleSpec(match, nil)
					if lErr != nil {
						return nil, fmt.Errorf("failed reading spec file %s: %w", match, lErr)
					}

					allSpecs = append(allSpecs, doc)
				}
			} else {
				doc, lErr := loadSingleSpec(part, nil)
				if lErr != nil {
					return nil, fmt.Errorf("failed reading spec file %s: %w", part, lErr)
				}

				allSpecs = append(allSpecs, doc)
			}
		}

		if len(allSpecs) == 0 {
			return nil, fmt.Errorf("no valid specification files found in %q", filename)
		}

		return MergeOpenAPISpecsWithMode(mode, allSpecs...), nil
	}

	if strings.ContainsAny(filename, "*?[]") {
		matches, err := filepath.Glob(filename)
		if err == nil && len(matches) > 0 {
			var allSpecs []*openapi3.T
			for _, match := range matches {
				doc, lErr := loadSingleSpec(match, nil)
				if lErr != nil {
					return nil, fmt.Errorf("failed reading spec file %s: %w", match, lErr)
				}

				allSpecs = append(allSpecs, doc)
			}

			return MergeOpenAPISpecsWithMode(mode, allSpecs...), nil
		}
	}

	return loadSingleSpec(filename, nil)
}

func loadSingleSpec(filename string, data []byte) (*openapi3.T, error) {
	if len(data) == 0 {
		var err error

		if strings.HasPrefix(filename, "cache:") {
			cacheID := strings.TrimPrefix(filename, "cache:")

			data, _, err = cache.GetTraffic(".", cacheID)
			if err != nil {
				return nil, fmt.Errorf("loading cached traffic %q: %w", cacheID, err)
			}
		} else {
			data, err = os.ReadFile(filename)
			if err != nil {
				// Fallback: check traffic cache by ID or hash
				if cData, _, cErr := cache.GetTraffic(".", filename); cErr == nil && len(cData) > 0 {
					data = cData
				} else if cData, _, cErr := cache.GetTraffic(".", strings.TrimSuffix(filename, ".har")); cErr == nil && len(cData) > 0 {
					data = cData
				} else {
					return nil, fmt.Errorf("failed reading spec file %s: %w", filename, err)
				}
			}
		}
	}

	data = sanitizeSpecData(data)

	format, _ := ingest.DetectFormat(data)
	if format == ingest.FormatHAR {
		return ingest.HARToOpenAPI(data)
	}

	var versionDetector struct {
		Swagger string `json:"swagger" yaml:"swagger"`
		OpenAPI string `json:"openapi" yaml:"openapi"`
	}

	if err := yaml.Unmarshal(data, &versionDetector); err == nil {
		if strings.HasPrefix(versionDetector.Swagger, "2.") || versionDetector.Swagger == "2.0" {
			var doc2 openapi2.T
			if err := json.Unmarshal(data, &doc2); err != nil {
				if errYaml := yaml.Unmarshal(data, &doc2); errYaml != nil {
					return nil, fmt.Errorf("failed parsing Swagger 2.0 spec: %w", err)
				}
			}

			doc3, err := openapi2conv.ToV3(&doc2)
			if err != nil {
				return nil, fmt.Errorf("failed converting Swagger 2.0 to OpenAPI 3.0: %w", err)
			}

			return doc3, nil
		}
	}

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true

	doc3, err := loader.LoadFromData(data)
	if err != nil {
		return nil, fmt.Errorf("failed parsing OpenAPI 3.x spec: %w", err)
	}

	return doc3, nil
}

func sanitizeSpecData(data []byte) []byte {
	var rawNode any
	if err := yaml.Unmarshal(data, &rawNode); err != nil {
		return data
	}

	sanitizeMapNode(rawNode)

	cleaned, err := json.Marshal(rawNode)
	if err != nil {
		return data
	}

	return cleaned
}

func sanitizeMapNode(node any) {
	switch v := node.(type) {
	case map[string]any:
		for key, val := range v {
			if key == "type" {
				if arr, ok := val.([]any); ok && len(arr) > 0 {
					var nonNull []any
					for _, item := range arr {
						if s, ok := item.(string); ok && s != "null" {
							nonNull = append(nonNull, s)
						}
					}

					if len(nonNull) > 0 {
						v["type"] = nonNull[0]
					} else {
						v["type"] = "string"
					}
				}
			}

			if key == "$ref" {
				if strVal, ok := val.(string); ok {
					if strings.HasPrefix(strVal, "#") && !strings.HasPrefix(strVal, "#/") {
						v[key] = "#/" + strVal[1:]
					}
				}
			} else {
				switch key {
				case "nullable", "deprecated", "readOnly", "writeOnly", "exclusiveMinimum", "exclusiveMaximum":
					if strVal, ok := val.(string); ok {
						if strings.EqualFold(strVal, "true") {
							v[key] = true
						} else if strings.EqualFold(strVal, "false") {
							v[key] = false
						}
					}

				case "type":
					if strVal, ok := val.(string); ok {
						if strings.EqualFold(strVal, "string|number") || strings.EqualFold(strVal, "number|string") {
							v[key] = "string"
						}
					}
				}
			}

			sanitizeMapNode(val)
		}

	case map[any]any:
		for k, val := range v {
			keyStr := fmt.Sprintf("%v", k)
			if keyStr == "$ref" {
				if strVal, ok := val.(string); ok {
					if strings.HasPrefix(strVal, "#") && !strings.HasPrefix(strVal, "#/") {
						v[k] = "#/" + strVal[1:]
					}
				}
			} else {
				switch keyStr {
				case "nullable", "deprecated", "readOnly", "writeOnly", "exclusiveMinimum", "exclusiveMaximum":
					if strVal, ok := val.(string); ok {
						if strings.EqualFold(strVal, "true") {
							v[k] = true
						} else if strings.EqualFold(strVal, "false") {
							v[k] = false
						}
					}

				case "type":
					if strVal, ok := val.(string); ok {
						if strings.EqualFold(strVal, "string|number") || strings.EqualFold(strVal, "number|string") {
							v[k] = "string"
						}
					}
				}
			}

			sanitizeMapNode(val)
		}

	case []any:
		for _, item := range v {
			sanitizeMapNode(item)
		}
	}
}
