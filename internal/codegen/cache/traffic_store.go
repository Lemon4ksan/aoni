// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// TrafficEntry represents a cached traffic session snapshot.
type TrafficEntry struct {
	ID              string    `json:"id"`
	OriginalFile    string    `json:"original_file"`
	Hash            string    `json:"hash"`
	Origins         []string  `json:"origins"`
	SizeBytes       int64     `json:"size_bytes"`
	CompressedBytes int64     `json:"compressed_bytes"`
	Sanitized       bool      `json:"sanitized"`
	EndpointCount   int       `json:"endpoint_count"`
	StoredAt        time.Time `json:"stored_at"`
}

// TrafficIndex stores the catalog of cached traffic captures in .vortex/cache/traffic/index.json.
type TrafficIndex struct {
	Entries map[string]TrafficEntry `json:"entries"`
}

// LoadTrafficIndex loads the traffic index from the root directory.
func LoadTrafficIndex(rootDir string) (*TrafficIndex, string, error) {
	indexPath := filepath.Join(rootDir, ".vortex", "cache", "traffic", "index.json")

	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &TrafficIndex{Entries: make(map[string]TrafficEntry)}, indexPath, nil
		}

		return nil, indexPath, err
	}

	var idx TrafficIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, indexPath, fmt.Errorf("parsing traffic index: %w", err)
	}

	if idx.Entries == nil {
		idx.Entries = make(map[string]TrafficEntry)
	}

	return &idx, indexPath, nil
}

// Save persists the traffic index to disk.
func (idx *TrafficIndex) Save(indexPath string) error {
	dir := filepath.Dir(indexPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}

	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(indexPath, data, 0o600)
}

// StoreTraffic compresses and archives a HAR payload into .vortex/cache/traffic/<hash>.har.gz.
func StoreTraffic(
	rootDir, srcPath string,
	data []byte,
	moveOriginal, sanitize bool,
	configs ...*SecretsConfig,
) (*TrafficEntry, map[string]SecretEntry, error) {
	idx, indexPath, err := LoadTrafficIndex(rootDir)
	if err != nil {
		return nil, nil, err
	}

	var sc *SecretsConfig
	if len(configs) > 0 && configs[0] != nil {
		sc = configs[0]
	} else {
		sc = &SecretsConfig{}
	}

	extractedSecrets := make(map[string]SecretEntry)
	processedData := data

	if sanitize {
		sanitized, sec, sanErr := SanitizeHAR(data, sc)
		if sanErr == nil && len(sanitized) > 0 {
			processedData = sanitized
			extractedSecrets = sec
		}
	}

	sum := sha256.Sum256(processedData)
	hashStr := hex.EncodeToString(sum[:])
	shortHash := hashStr[:12]

	origins, epCount := extractHARMetadata(processedData)
	baseName := filepath.Base(srcPath)

	entryID := strings.TrimSuffix(baseName, filepath.Ext(baseName))
	if entryID == "" || entryID == "." {
		entryID = shortHash
	}

	trafficDir := filepath.Join(rootDir, ".vortex", "cache", "traffic")
	if err := os.MkdirAll(trafficDir, 0o750); err != nil {
		return nil, nil, err
	}

	blobPath := filepath.Join(trafficDir, hashStr+".har.gz")

	var compressedBuf bytes.Buffer

	gw := gzip.NewWriter(&compressedBuf)
	if _, err := gw.Write(processedData); err != nil {
		return nil, nil, fmt.Errorf("compressing traffic blob: %w", err)
	}

	if err := gw.Close(); err != nil {
		return nil, nil, err
	}

	if err := os.WriteFile(blobPath, compressedBuf.Bytes(), 0o600); err != nil {
		return nil, nil, fmt.Errorf("writing compressed traffic blob: %w", err)
	}

	entry := TrafficEntry{
		ID:              entryID,
		OriginalFile:    baseName,
		Hash:            hashStr,
		Origins:         origins,
		SizeBytes:       int64(len(processedData)),
		CompressedBytes: int64(compressedBuf.Len()),
		Sanitized:       sanitize,
		EndpointCount:   epCount,
		StoredAt:        time.Now(),
	}

	idx.Entries[entryID] = entry
	idx.Entries[shortHash] = entry

	if err := idx.Save(indexPath); err != nil {
		return nil, nil, err
	}

	// If moveOriginal requested and srcPath exists on disk, remove it safely
	if moveOriginal && srcPath != "" && srcPath != "-" {
		if absSrc, err := filepath.Abs(srcPath); err == nil {
			if absBlob, err := filepath.Abs(blobPath); err == nil && absSrc != absBlob {
				_ = os.Remove(srcPath)
			}
		}
	}

	return &entry, extractedSecrets, nil
}

// GetTraffic retrieves and decompresses a cached traffic session by ID or hash prefix.
func GetTraffic(rootDir, idOrHash string) ([]byte, *TrafficEntry, error) {
	idx, _, err := LoadTrafficIndex(rootDir)
	if err != nil {
		return nil, nil, err
	}

	var matchedEntry *TrafficEntry
	for k, e := range idx.Entries {
		if k == idOrHash || strings.HasPrefix(e.Hash, idOrHash) ||
			strings.EqualFold(e.ID, idOrHash) ||
			strings.Contains(strings.ToLower(e.ID), strings.ToLower(idOrHash)) {
			matchedEntry = &e
			break
		}
	}

	if matchedEntry == nil {
		return nil, nil, fmt.Errorf("vortex: traffic session %q not found in cache", idOrHash)
	}

	blobPath := filepath.Join(rootDir, ".vortex", "cache", "traffic", matchedEntry.Hash+".har.gz")

	f, err := os.Open(blobPath)
	if err != nil {
		return nil, nil, fmt.Errorf("reading cached traffic blob: %w", err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return nil, nil, fmt.Errorf("decompressing traffic blob: %w", err)
	}
	defer gr.Close()

	decompressed, err := io.ReadAll(gr)
	if err != nil {
		return nil, nil, fmt.Errorf("reading decompressed traffic payload: %w", err)
	}

	return decompressed, matchedEntry, nil
}

// ListTraffic returns all cached traffic sessions sorted by storage date descending.
func ListTraffic(rootDir string) ([]TrafficEntry, error) {
	idx, _, err := LoadTrafficIndex(rootDir)
	if err != nil {
		return nil, err
	}

	seenHash := make(map[string]bool)

	var list []TrafficEntry

	for _, e := range idx.Entries {
		if !seenHash[e.Hash] {
			seenHash[e.Hash] = true
			list = append(list, e)
		}
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].StoredAt.After(list[j].StoredAt)
	})

	return list, nil
}

// PruneTraffic removes old or unreferenced traffic captures.
func PruneTraffic(rootDir string, olderThan time.Duration, all bool) (int, error) {
	idx, indexPath, err := LoadTrafficIndex(rootDir)
	if err != nil {
		return 0, err
	}

	cutoff := time.Now().Add(-olderThan)
	removedCount := 0
	seenHash := make(map[string]bool)

	var toDelete []string

	for k, e := range idx.Entries {
		if all || (olderThan > 0 && e.StoredAt.Before(cutoff)) {
			toDelete = append(toDelete, k)

			if !seenHash[e.Hash] {
				seenHash[e.Hash] = true
				blobPath := filepath.Join(rootDir, ".vortex", "cache", "traffic", e.Hash+".har.gz")
				_ = os.Remove(blobPath)
				removedCount++
			}
		}
	}

	for _, k := range toDelete {
		delete(idx.Entries, k)
	}

	_ = idx.Save(indexPath)

	return removedCount, nil
}

// SanitizeHAR masks sensitive credentials and scrubs static assets from HAR JSON bytes.
func SanitizeHAR(data []byte, configs ...*SecretsConfig) ([]byte, map[string]SecretEntry, error) {
	var sc *SecretsConfig
	if len(configs) > 0 && configs[0] != nil {
		sc = configs[0]
	} else {
		sc = &SecretsConfig{}
	}

	var harDoc map[string]any
	if err := json.Unmarshal(data, &harDoc); err != nil {
		return nil, nil, err
	}

	logObj, ok := harDoc["log"].(map[string]any)
	if !ok {
		return data, nil, nil
	}

	entries, ok := logObj["entries"].([]any)
	if !ok {
		return data, nil, nil
	}

	extractedSecrets := make(map[string]SecretEntry)

	var safeEntries []any

	for _, item := range entries {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}

		req, ok := entry["request"].(map[string]any)
		if !ok {
			continue
		}

		rawURL, _ := req["url"].(string)
		if isIgnoredEndpointURL(rawURL) {
			continue
		}

		// 1. Sanitize and extract request headers
		if headers, ok := req["headers"].([]any); ok {
			var safeHeaders []any
			for _, h := range headers {
				hMap, ok := h.(map[string]any)
				if !ok {
					continue
				}

				name, _ := hMap["name"].(string)
				val, _ := hMap["value"].(string)

				if strings.HasPrefix(name, ":") {
					continue
				}

				lower := strings.ToLower(name)
				if lower == "cookie" {
					continue
				}

				if sc != nil {
					if envVar, ok := sc.GetHeaderEnv(name); ok && envVar != "" && val != "" {
						if strings.EqualFold(name, "authorization") &&
							(strings.HasPrefix(val, "Bearer ") || strings.HasPrefix(val, "bearer ")) {
							tokenVal := strings.TrimSpace(val[7:])
							extractedSecrets[envVar] = SecretEntry{
								Key:    envVar,
								Value:  tokenVal,
								Header: name,
							}
							hMap["value"] = "Bearer ${" + envVar + "}"
						} else {
							extractedSecrets[envVar] = SecretEntry{
								Key:    envVar,
								Value:  val,
								Header: name,
							}
							hMap["value"] = "${" + envVar + "}"
						}
					}
				}

				safeHeaders = append(safeHeaders, hMap)
			}

			req["headers"] = safeHeaders
		}

		// 2. Sanitize and extract query parameters
		if qParams, ok := req["queryString"].([]any); ok {
			var safeQuery []any
			for _, q := range qParams {
				qMap, ok := q.(map[string]any)
				if !ok {
					continue
				}

				qName, _ := qMap["name"].(string)
				qVal, _ := qMap["value"].(string)

				if sc != nil {
					if envVar, ok := sc.GetQueryEnv(qName); ok && envVar != "" && qVal != "" {
						extractedSecrets[envVar] = SecretEntry{
							Key:   envVar,
							Value: qVal,
							Query: qName,
						}
						qMap["value"] = "${" + envVar + "}"
					}
				}

				safeQuery = append(safeQuery, qMap)
			}

			req["queryString"] = safeQuery
		}

		// Scrub cookies
		req["cookies"] = []any{}

		if resp, ok := entry["response"].(map[string]any); ok {
			resp["cookies"] = []any{}

			// Auto-decompress gzipped response content
			decompressed := false
			if content, ok := resp["content"].(map[string]any); ok {
				if text, ok := content["text"].(string); ok && text != "" {
					enc, _ := content["encoding"].(string)
					decomp := tryDecompressHARText(text, enc)
					if decomp != "" && decomp != text {
						content["text"] = decomp
						content["size"] = len(decomp)
						delete(content, "encoding")
						decompressed = true
					}
				}
			}

			if respHeaders, ok := resp["headers"].([]any); ok {
				var safeRespHeaders []any
				for _, h := range respHeaders {
					hMap, ok := h.(map[string]any)
					if !ok {
						continue
					}

					name, _ := hMap["name"].(string)
					if strings.EqualFold(name, "set-cookie") {
						continue
					}
					if decompressed && strings.EqualFold(name, "content-encoding") {
						continue
					}

					safeRespHeaders = append(safeRespHeaders, hMap)
				}

				resp["headers"] = safeRespHeaders
			}
		}

		safeEntries = append(safeEntries, entry)
	}

	logObj["entries"] = safeEntries

	cleaned, err := json.Marshal(harDoc)
	if err != nil {
		return nil, nil, err
	}

	return cleaned, extractedSecrets, nil
}

func extractHARMetadata(data []byte) ([]string, int) {
	var harDoc map[string]any
	if err := json.Unmarshal(data, &harDoc); err != nil {
		return nil, 0
	}

	logObj, ok := harDoc["log"].(map[string]any)
	if !ok {
		return nil, 0
	}

	entries, ok := logObj["entries"].([]any)
	if !ok {
		return nil, 0
	}

	originMap := make(map[string]bool)
	count := 0

	for _, item := range entries {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}

		req, ok := entry["request"].(map[string]any)
		if !ok {
			continue
		}

		rawURL, _ := req["url"].(string)
		if u, err := url.Parse(rawURL); err == nil && u.Host != "" {
			originMap[u.Host] = true
			count++
		}
	}

	var origins []string
	for o := range originMap {
		origins = append(origins, o)
	}

	sort.Strings(origins)

	return origins, count
}

func isIgnoredEndpointURL(rawURL string) bool {
	lowerURL := strings.ToLower(rawURL)

	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	lowerPath := strings.ToLower(u.Path)

	staticExts := []string{
		".js", ".mjs", ".css", ".png", ".jpg", ".jpeg", ".gif", ".svg",
		".ico", ".woff", ".woff2", ".ttf", ".eot", ".webp", ".map",
	}
	for _, ext := range staticExts {
		if strings.HasSuffix(lowerPath, ext) {
			return true
		}
	}

	noisePatterns := []string{
		"/csi", "/log", "/gen_204", "/survey/", "/collect", "/analytics",
		"google-analytics.com", "googletagmanager.com", "fonts.googleapis.com",
		"fonts.gstatic.com", "cookienotificationbar", "/a/acg8",
	}
	for _, p := range noisePatterns {
		if strings.Contains(lowerPath, p) || strings.Contains(lowerURL, p) {
			return true
		}
	}

	return false
}

func tryDecompressHARText(bodyText, encoding string) string {
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
	if len(runes) >= 2 && runes[0] == 0x1f && (runes[1] == 0x8b || runes[1] == '\b' || runes[1] == 0xef) {
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
