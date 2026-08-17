// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package builder

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/cache"
	"github.com/lemon4ksan/aoni/internal/codegen/ingest"
	"github.com/lemon4ksan/aoni/internal/codegen/ir"
)

// PopulateMockFixtures extracts recorded HTTP responses from @source cache entries or HAR files
// and populates MockFixture on each MethodIR.
func PopulateMockFixtures(rootDir string, svc *ir.ServiceIR) error {
	if svc == nil || svc.Source == "" {
		return nil
	}

	fixtures, err := LoadFixturesFromSource(rootDir, svc.Source)
	if err != nil {
		return err
	}

	if len(fixtures) == 0 {
		return nil
	}

	for _, m := range svc.Methods {
		if m.MockFixture != nil && m.MockFixture.Body != "" {
			continue // Preserve explicit @mock:fixture directive
		}

		httpVerb := strings.ToUpper(m.HTTPMethod)
		if httpVerb == "" {
			httpVerb = "GET"
		}

		rawPath := ""
		if m.Path != nil {
			rawPath = m.Path.RawTemplate
		}

		cleanPath := strings.Trim(rawPath, "/")
		if cleanPath == "" {
			cleanPath = strings.ToLower(m.Name)
		}

		// Try exact match (METHOD + path)
		exactKey := httpVerb + " " + cleanPath
		if f, ok := fixtures[exactKey]; ok {
			m.MockFixture = f
			continue
		}

		// Try normalized template match (METHOD + normalized/path)
		normKey := httpVerb + " " + normalizeFixturePath(cleanPath)
		if f, ok := fixtures[normKey]; ok {
			m.MockFixture = f
			continue
		}

		// Try route template match (handling {param} segments)
		for fKey, f := range fixtures {
			if strings.HasPrefix(fKey, httpVerb+" ") {
				fPath := strings.TrimPrefix(fKey, httpVerb+" ")
				if matchRoute(cleanPath, fPath) || matchRoute(fPath, cleanPath) {
					m.MockFixture = f
					break
				}
			}
		}
	}

	return nil
}

func matchRoute(pattern, actual string) bool {
	pattern = strings.Trim(pattern, "/")

	actual = strings.Trim(actual, "/")
	if pattern == actual {
		return true
	}

	pParts := strings.Split(pattern, "/")

	aParts := strings.Split(actual, "/")
	if len(pParts) != len(aParts) {
		return false
	}

	for i := range pParts {
		p := pParts[i]
		a := aParts[i]

		if (strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}")) ||
			(strings.HasPrefix(a, "{") && strings.HasSuffix(a, "}")) {
			continue
		}

		if !strings.EqualFold(p, a) {
			return false
		}
	}

	return true
}

// LoadFixturesFromSource parses one or more source descriptors (comma-separated) and returns
// a map of route keys ("GET api/features") to recorded MockFixtureIR.
func LoadFixturesFromSource(rootDir, sourceSpec string) (map[string]*ir.MockFixtureIR, error) {
	result := make(map[string]*ir.MockFixtureIR)
	sources := strings.Split(sourceSpec, ",")

	for _, src := range sources {
		src = strings.TrimSpace(src)
		if src == "" {
			continue
		}

		var (
			data []byte
			err  error
		)

		if strings.HasPrefix(src, "cache:") {
			cacheID := strings.TrimPrefix(src, "cache:")

			data, _, err = cache.GetTraffic(rootDir, cacheID)
			if err != nil {
				// Non-fatal: try without rootDir or relative
				data, _, err = cache.GetTraffic(".", cacheID)
			}
		} else {
			filePath := src
			if !filepath.IsAbs(filePath) && rootDir != "" {
				filePath = filepath.Join(rootDir, filePath)
			}

			data, err = os.ReadFile(filePath)
		}

		if err != nil || len(data) == 0 {
			continue
		}

		// Parse HAR
		var har ingest.HARLog
		if err := json.Unmarshal(data, &har); err != nil {
			continue
		}

		for _, entry := range har.Log.Entries {
			if entry.Response.Status == 0 && (entry.Response.Content == nil || entry.Response.Content.Text == "") {
				continue
			}

			u, err := url.Parse(entry.Request.URL)
			if err != nil {
				continue
			}

			cleanPath := strings.Trim(u.Path, "/")

			method := strings.ToUpper(entry.Request.Method)
			if method == "" {
				method = "GET"
			}

			statusCode := entry.Response.Status
			if statusCode == 0 {
				statusCode = 200
			}

			contentType := "application/json"
			headers := make(map[string]string)

			for _, h := range entry.Response.Headers {
				if strings.EqualFold(h.Name, "content-type") {
					contentType = h.Value
				}

				if strings.HasPrefix(strings.ToLower(h.Name), "grpc-") ||
					strings.HasPrefix(strings.ToLower(h.Name), "x-") {
					headers[h.Name] = h.Value
				}
			}

			if entry.Response.Content != nil && entry.Response.Content.MimeType != "" {
				contentType = entry.Response.Content.MimeType
			}

			bodyText := ""
			if entry.Response.Content != nil {
				bodyText = tryDecompressPayload(entry.Response.Content.Text, entry.Response.Content.Encoding)
			}

			reqBodyText := ""
			if entry.Request.PostData != nil && entry.Request.PostData.Text != "" {
				reqBodyText = tryDecompressPayload(entry.Request.PostData.Text, entry.Request.PostData.Encoding)
			}

			fixture := &ir.MockFixtureIR{
				StatusCode:  statusCode,
				ContentType: contentType,
				Headers:     headers,
				Body:        bodyText,
				RequestBody: reqBodyText,
			}

			// Store both exact and normalized paths
			result[method+" "+cleanPath] = fixture
			result[method+" "+normalizeFixturePath(cleanPath)] = fixture
		}
	}

	return result, nil
}

func tryDecompressPayload(bodyText, encoding string) string {
	if bodyText == "" {
		return ""
	}

	// 1. Try base64
	if strings.EqualFold(encoding, "base64") {
		if dec, err := base64.StdEncoding.DecodeString(bodyText); err == nil {
			if decomp := tryGunzip(dec); decomp != "" {
				return decomp
			}

			return string(dec)
		}
	}

	// 2. Try raw UTF-8 bytes gunzip
	if decomp := tryGunzip([]byte(bodyText)); decomp != "" {
		return decomp
	}

	// 3. Try latin-1 / binary string rune-to-byte gunzip
	runes := []rune(bodyText)
	if len(runes) >= 2 && runes[0] == 0x1f && runes[1] == 0x8b {
		bin := make([]byte, len(runes))
		for i, r := range runes {
			bin[i] = byte(r)
		}

		if decomp := tryGunzip(bin); decomp != "" {
			return decomp
		}
	}

	return bodyText
}

func tryGunzip(data []byte) string {
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		gzReader, err := gzip.NewReader(bytes.NewReader(data))
		if err == nil {
			decompressed, err := io.ReadAll(gzReader)
			_ = gzReader.Close()

			if err == nil {
				return string(decompressed)
			}
		}
	}

	return ""
}

func normalizeFixturePath(p string) string {
	clean := strings.Trim(p, "/")
	parts := strings.Split(clean, "/")

	for i, part := range parts {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			parts[i] = "{var}"
		}
	}

	return strings.Join(parts, "/")
}
