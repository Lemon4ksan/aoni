// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package openapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lemon4ksan/foundation/net/http/header"

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

		if strings.HasPrefix(clean, "http://") || strings.HasPrefix(clean, "https://") {
			result = append(result, clean)
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

	if strings.HasPrefix(filename, "http://") || strings.HasPrefix(filename, "https://") {
		return fetchRemoteSpecWithCache(filename)
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

func fetchRemoteSpecWithCache(rawURL string) ([]byte, error) {
	sum := sha256.Sum256([]byte(rawURL))
	cacheKey := hex.EncodeToString(sum[:12])
	cachePath := filepath.Join(".vortex", "cache", "specs", cacheKey+".spec")

	client := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, rawURL, nil)
	if err == nil {
		req.Header.Set(header.UserAgent, "Vortex-API-Guardian/1.0 (Zero-Alloc Go Client)")
		req.Header.Set(header.Accept, "application/json, application/yaml, text/yaml, text/plain, */*")

		resp, getErr := client.Do(req)
		if getErr == nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			defer resp.Body.Close()

			bodyBytes, readErr := io.ReadAll(resp.Body)
			if readErr == nil && len(bodyBytes) > 0 {
				_ = os.MkdirAll(filepath.Dir(cachePath), 0o750)
				_ = os.WriteFile(cachePath, bodyBytes, 0o600)

				return bodyBytes, nil
			}
		}
	}

	// Offline-First fallback: check local cached snapshot
	if cachedData, cErr := os.ReadFile(cachePath); cErr == nil && len(cachedData) > 0 {
		return cachedData, nil
	}

	return nil, fmt.Errorf(
		"vortex/openapi: failed fetching remote spec from %s and no offline cache snapshot found",
		rawURL,
	)
}
