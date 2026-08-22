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
	"slices"
	"strings"

	"github.com/lemon4ksan/foundation/generic"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/cache"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ingest"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
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
		if m == nil || (m.MockFixture != nil && m.MockFixture.Body != "") {
			continue // Preserve explicit @mock:fixture directive
		}

		httpVerb, cleanPath := resolveMethodRouteKey(m)
		if f := findMatchingFixture(fixtures, httpVerb, cleanPath); f != nil {
			m.MockFixture = f
		}
	}

	return nil
}

func resolveMethodRouteKey(m *ir.MethodIR) (httpVerb, cleanPath string) {
	httpVerb = generic.Coalesce(strings.ToUpper(m.HTTPMethod), "GET")

	rawPath := ""
	if m.Path != nil {
		rawPath = m.Path.RawTemplate
	}

	cleanPath = strings.Trim(rawPath, "/")
	if cleanPath == "" {
		cleanPath = strings.ToLower(m.Name)
	}

	return httpVerb, cleanPath
}

func findMatchingFixture(fixtures map[string]*ir.MockFixtureIR, httpVerb, cleanPath string) *ir.MockFixtureIR {
	// 1. Exact match (METHOD + path)
	if f, ok := fixtures[httpVerb+" "+cleanPath]; ok {
		return f
	}

	// 2. Normalized template match (METHOD + normalized/path)
	if f, ok := fixtures[httpVerb+" "+normalizeFixturePath(cleanPath)]; ok {
		return f
	}

	// 3. Route template matching for dynamic {param} segments
	prefix := httpVerb + " "
	for fKey, f := range fixtures {
		if strings.HasPrefix(fKey, prefix) {
			fPath := strings.TrimPrefix(fKey, prefix)
			if matchRoute(cleanPath, fPath) || matchRoute(fPath, cleanPath) {
				return f
			}
		}
	}

	return nil
}

func matchRoute(pattern, actual string) bool {
	pParts := strings.Split(strings.Trim(pattern, "/"), "/")
	aParts := strings.Split(strings.Trim(actual, "/"), "/")

	if len(pParts) != len(aParts) {
		return false
	}

	return slices.EqualFunc(pParts, aParts, func(p, a string) bool {
		if isRouteVariable(p) || isRouteVariable(a) {
			return true
		}

		return strings.EqualFold(p, a)
	})
}

func isRouteVariable(s string) bool {
	return strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}")
}

// LoadFixturesFromSource parses one or more source descriptors (comma-separated) and returns
// a map of route keys ("GET api/features") to recorded MockFixtureIR.
func LoadFixturesFromSource(rootDir, sourceSpec string) (map[string]*ir.MockFixtureIR, error) {
	result := make(map[string]*ir.MockFixtureIR)
	sources := strings.Split(sourceSpec, ",")

	for _, src := range sources {
		cleanSrc := strings.TrimSpace(src)
		if cleanSrc == "" {
			continue
		}

		data, err := loadSourceBytes(rootDir, cleanSrc)
		if err != nil || len(data) == 0 {
			continue
		}

		extractFixturesFromHAR(data, result)
	}

	return result, nil
}

func loadSourceBytes(rootDir, src string) ([]byte, error) {
	if strings.HasPrefix(src, "cache:") {
		cacheID := strings.TrimPrefix(src, "cache:")

		data, _, err := cache.GetTraffic(rootDir, cacheID)
		if err != nil {
			data, _, err = cache.GetTraffic(".", cacheID)
		}

		return data, err
	}

	filePath := src
	if !filepath.IsAbs(filePath) && rootDir != "" {
		filePath = filepath.Join(rootDir, filePath)
	}

	return os.ReadFile(filePath)
}

func extractFixturesFromHAR(data []byte, result map[string]*ir.MockFixtureIR) {
	var har ingest.HARLog
	if err := json.Unmarshal(data, &har); err != nil {
		return
	}

	for _, entry := range har.Log.Entries {
		method, cleanPath, fixture, ok := parseHAREntryFixture(entry)
		if !ok {
			continue
		}

		result[method+" "+cleanPath] = fixture
		result[method+" "+normalizeFixturePath(cleanPath)] = fixture
	}
}

func parseHAREntryFixture(entry ingest.HAREntry) (method, cleanPath string, fixture *ir.MockFixtureIR, ok bool) {
	if entry.Response.Status == 0 && (entry.Response.Content == nil || entry.Response.Content.Text == "") {
		return "", "", nil, false
	}

	u, err := url.Parse(entry.Request.URL)
	if err != nil {
		return "", "", nil, false
	}

	cleanPath = strings.Trim(u.Path, "/")
	method = generic.Coalesce(strings.ToUpper(entry.Request.Method), "GET")
	statusCode := generic.Coalesce(entry.Response.Status, 200)

	contentType := "application/json"
	headers := make(map[string]string)

	for _, h := range entry.Response.Headers {
		if strings.EqualFold(h.Name, "content-type") {
			contentType = h.Value
		}

		if strings.HasPrefix(strings.ToLower(h.Name), "grpc-") || strings.HasPrefix(strings.ToLower(h.Name), "x-") {
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

	return method, cleanPath, &ir.MockFixtureIR{
		StatusCode:  statusCode,
		ContentType: contentType,
		Headers:     headers,
		Body:        bodyText,
		RequestBody: reqBodyText,
	}, true
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
	parts := strings.Split(strings.Trim(p, "/"), "/")
	for i, part := range parts {
		if isRouteVariable(part) {
			parts[i] = "{var}"
		}
	}

	return strings.Join(parts, "/")
}
