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
func LoadSpec(filename string, data []byte) (*Document, error) {
	return LoadSpecWithMode(filename, data, MergeModeUnion)
}

// LoadSpecWithMode loads and combines multiple specifications using the specified MergeMode (union, intersect, diff).
func LoadSpecWithMode(filename string, data []byte, mode MergeMode) (*Document, error) {
	if len(data) > 0 {
		return loadSingleSpec(filename, data)
	}

	if strings.Contains(filename, ",") {
		parts := strings.Split(filename, ",")

		var allSpecs []*Document

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
			var allSpecs []*Document
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

func loadSingleSpec(filename string, data []byte) (*Document, error) {
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

	return ParseSpec(data)
}
