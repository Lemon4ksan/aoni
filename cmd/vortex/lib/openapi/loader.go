// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package openapi

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/cache"
)

// LoadSpec loads an OpenAPI specification using default Union merge mode.
//
// # References
//   - OpenAPI 3.1.0 Specification: https://spec.openapis.org/oas/v3.1.0
//   - Swagger 2.0 Specification: https://swagger.io/specification/v2/
func LoadSpec(filename string, data []byte) (*Document, error) {
	return LoadSpecWithMode(filename, data, MergeModeUnion)
}

// LoadSpecWithMode loads and combines multiple specifications using the specified MergeMode (union, intersect, diff).
//
// # References
//   - OpenAPI 3.1.0 §4.8.1 OpenAPI Object: https://spec.openapis.org/oas/v3.1.0#openapi-object
func LoadSpecWithMode(filename string, data []byte, mode MergeMode) (*Document, error) {
	if len(data) > 0 {
		return loadSingleSpec(filename, data)
	}

	files, err := resolveSpecFiles(filename)
	if err != nil {
		return nil, err
	}

	if len(files) == 1 {
		return loadSingleSpec(files[0], nil)
	}

	var allSpecs []*Document
	for _, f := range files {
		doc, lErr := loadSingleSpec(f, nil)
		if lErr != nil {
			return nil, fmt.Errorf("vortex/openapi: read spec file %s: %w", f, lErr)
		}

		allSpecs = append(allSpecs, doc)
	}

	return MergeOpenAPISpecsWithMode(mode, allSpecs...), nil
}

func resolveSpecFiles(target string) ([]string, error) {
	parts := strings.Split(target, ",")

	var result []string

	for _, part := range parts {
		clean := strings.TrimSpace(part)
		if clean == "" {
			continue
		}

		if strings.ContainsAny(clean, "*?[]") {
			matches, err := filepath.Glob(clean)
			if err != nil {
				return nil, fmt.Errorf("vortex/openapi: invalid glob pattern %q: %w", clean, err)
			}

			result = append(result, matches...)

			continue
		}

		result = append(result, clean)
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("vortex/openapi: no valid specification files found in %q", target)
	}

	return result, nil
}

func loadSingleSpec(filename string, data []byte) (*Document, error) {
	if len(data) > 0 {
		return ParseSpec(data)
	}

	raw, err := readSpecBytes(filename)
	if err != nil {
		return nil, err
	}

	return ParseSpec(raw)
}

func readSpecBytes(filename string) ([]byte, error) {
	if strings.HasPrefix(filename, "cache:") {
		cacheID := strings.TrimPrefix(filename, "cache:")

		data, _, err := cache.GetTraffic(".", cacheID)
		if err != nil {
			return nil, fmt.Errorf("vortex/openapi: load cached traffic %q: %w", cacheID, err)
		}

		return data, nil
	}

	data, err := os.ReadFile(filename)
	if err == nil {
		return data, nil
	}

	// Fallback: check traffic cache by ID or filename without .har
	if cData, _, cErr := cache.GetTraffic(".", filename); cErr == nil && len(cData) > 0 {
		return cData, nil
	}

	if cData, _, cErr := cache.GetTraffic(".", strings.TrimSuffix(filename, ".har")); cErr == nil && len(cData) > 0 {
		return cData, nil
	}

	return nil, fmt.Errorf("vortex/openapi: read spec file %s: %w", filename, err)
}
