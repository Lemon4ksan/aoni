// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	stdio "io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lemon4ksan/aoni/internal/bytesconv"
)

// SavePushedResponseToCache validates and stores an HTTP/2 server-pushed response into the cache store.
func (p *Pipeline) SavePushedResponseToCache(req *http.Request, resp *http.Response, cfg *CacheConfig) {
	p.saveToCache(req, resp, cfg)
}

// DefaultIgnoredTrackingParams lists standard marketing and tracking parameters ignored by No-Vary-Search.
var DefaultIgnoredTrackingParams = []string{
	"utm_source", "utm_medium", "utm_campaign", "utm_term",
	"utm_content", "fbclid", "gclid", "msclkid", "_ga", "ref",
}

// NoVarySearchConfig specifies query parameter normalization rules (W3C No-Vary-Search) for caching.
type NoVarySearchConfig struct {
	IgnoreParams    []string
	ExceptParams    []string
	IgnoreAllParams bool
}

// DefaultNoVarySearchConfig provides standard No-Vary-Search rules ignoring common tracking parameters.
var DefaultNoVarySearchConfig = NoVarySearchConfig{
	IgnoreParams: DefaultIgnoredTrackingParams,
}

// NormalizeCacheURL applies No-Vary-Search rules to strip tracking query parameters and order keys.
func NormalizeCacheURL(rawURL string, cfg *NoVarySearchConfig) string {
	if rawURL == "" {
		return ""
	}

	u, err := url.Parse(rawURL)
	if err != nil || u.RawQuery == "" {
		return rawURL
	}

	query := u.Query()
	if len(query) == 0 {
		return rawURL
	}

	activeCfg := cfg
	if activeCfg == nil {
		activeCfg = &DefaultNoVarySearchConfig
	}

	normalizedQuery := filterQueryParams(query, activeCfg)
	u.RawQuery = normalizedQuery.Encode()

	return u.String()
}

// ComputeCookieIndicesHash calculates a SHA-256 hash of specific requested cookie names.
func ComputeCookieIndicesHash(req *http.Request, cookieNames []string) string {
	if req == nil || len(cookieNames) == 0 {
		return ""
	}

	cookies := req.Cookies()
	if len(cookies) == 0 {
		return ""
	}

	var matched []string
	for _, name := range cookieNames {
		for _, c := range cookies {
			if strings.EqualFold(c.Name, name) {
				matched = append(matched, c.Name+"="+c.Value)
				break
			}
		}
	}

	if len(matched) == 0 {
		return ""
	}

	slices.Sort(matched)
	concat := strings.Join(matched, ";")
	hash := sha256.Sum256(bytesconv.S2B(concat))

	return hex.EncodeToString(hash[:6])
}

// ParseCookieIndicesHeader parses the 'Cookie-Indices' response header (e.g. 'Cookie-Indices: "theme", "lang"').
func ParseCookieIndicesHeader(header string) []string {
	if header == "" {
		return nil
	}

	var names []string
	for p := range strings.SplitSeq(header, ",") {
		cleaned := strings.Trim(strings.TrimSpace(p), `"'`)
		if cleaned != "" {
			names = append(names, cleaned)
		}
	}

	return names
}

func filterQueryParams(query url.Values, cfg *NoVarySearchConfig) url.Values {
	result := make(url.Values, len(query))

	for key, values := range query {
		if shouldIgnoreQueryParam(key, cfg) {
			continue
		}

		result[key] = values
	}

	return result
}

func shouldIgnoreQueryParam(key string, cfg *NoVarySearchConfig) bool {
	if cfg.IgnoreAllParams {
		for _, exc := range cfg.ExceptParams {
			if strings.EqualFold(key, exc) {
				return false
			}
		}

		return true
	}

	for _, ign := range cfg.IgnoreParams {
		if strings.EqualFold(key, ign) {
			return true
		}
	}

	return false
}

func (p *Pipeline) tryGetFromCache(req *http.Request, cfg *CacheConfig) *http.Response {
	if req.Method != http.MethodGet || cfg == nil || cfg.Store == nil {
		return nil
	}

	cc := req.Header.Get("Cache-Control")
	if strings.Contains(cc, "no-cache") || strings.Contains(cc, "no-store") {
		return nil
	}

	normURL := NormalizeCacheURL(req.URL.String(), cfg.NoVarySearch)
	cookieHash := ComputeCookieIndicesHash(req, cfg.CookieIndices)

	cachedData, err := cfg.Store.Get(req.Context(), CacheKey{
		Method:     req.Method,
		URL:        normURL,
		CookieHash: cookieHash,
	})
	if err != nil {
		return nil
	}

	var cached CachedResponse
	if decodeErr := json.Unmarshal(cachedData, &cached); decodeErr != nil {
		return nil
	}

	if !matchVaryHeaders(req, cached.VaryHeaders) {
		return nil
	}

	bodyBytes, _ := base64.StdEncoding.DecodeString(cached.BodyBase64)

	respHeaders := http.Header(cached.Header).Clone()
	if !cached.CachedAt.IsZero() {
		ageSeconds := int64(time.Since(cached.CachedAt).Seconds())
		respHeaders.Set("Age", strconv.FormatInt(max(ageSeconds, 0), 10))
	}

	return &http.Response{
		StatusCode:    cached.StatusCode,
		Header:        respHeaders,
		Body:          stdio.NopCloser(bytes.NewReader(bodyBytes)),
		ContentLength: int64(len(bodyBytes)),
		Request:       req,
	}
}

func matchVaryHeaders(req *http.Request, varyHeaders map[string]string) bool {
	if len(varyHeaders) == 0 {
		return true
	}

	for k, expectedVal := range varyHeaders {
		if req.Header.Get(k) != expectedVal {
			return false
		}
	}

	return true
}

func parseFreshnessLifetime(resp *http.Response) (time.Duration, bool) {
	cc := resp.Header.Get("Cache-Control")
	for p := range strings.SplitSeq(cc, ",") {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "s-maxage=") {
			if secs, err := strconv.ParseInt(p[9:], 10, 64); err == nil && secs >= 0 {
				return time.Duration(secs) * time.Second, true
			}
		}

		if strings.HasPrefix(p, "max-age=") {
			if secs, err := strconv.ParseInt(p[8:], 10, 64); err == nil && secs >= 0 {
				return time.Duration(secs) * time.Second, true
			}
		}
	}

	if exp := resp.Header.Get("Expires"); exp != "" {
		if t, err := http.ParseTime(exp); err == nil {
			return max(time.Until(t), 0), true
		}
	}

	return 0, false
}

func (p *Pipeline) saveToCache(req *http.Request, resp *http.Response, cfg *CacheConfig) {
	if req.Method != http.MethodGet || resp == nil || resp.StatusCode != http.StatusOK || cfg == nil ||
		cfg.Store == nil {
		return
	}

	respCC := resp.Header.Get("Cache-Control")
	if strings.Contains(respCC, "no-store") || strings.Contains(respCC, "private") {
		return
	}

	varyHeader := resp.Header.Get("Vary")
	if varyHeader == "*" {
		return
	}

	var bodyBuf bytes.Buffer

	tee := stdio.TeeReader(resp.Body, &bodyBuf)

	bodyBytes, readErr := stdio.ReadAll(tee)
	if readErr != nil {
		return
	}

	_ = resp.Body.Close()
	resp.Body = stdio.NopCloser(bytes.NewReader(bodyBytes))

	cached := CachedResponse{
		StatusCode:  resp.StatusCode,
		Header:      resp.Header,
		VaryHeaders: extractVaryHeaders(req, varyHeader),
		BodyBase64:  base64.StdEncoding.EncodeToString(bodyBytes),
		CachedAt:    time.Now().UTC(),
	}

	cachedData, marshalErr := json.Marshal(cached)
	if marshalErr != nil {
		return
	}

	ttl := cfg.DefaultTTL
	if reqCfg := GetRequestConfig(req.Context()); reqCfg != nil && reqCfg.CacheTTL > 0 {
		ttl = reqCfg.CacheTTL
	} else if parsedTTL, ok := parseFreshnessLifetime(resp); ok {
		ttl = parsedTTL
	}

	effectiveConfig := resolveEffectiveNoVarySearch(resp, cfg)
	normURL := NormalizeCacheURL(req.URL.String(), effectiveConfig)

	effectiveCookieNames := resolveEffectiveCookieIndices(resp, cfg)
	cookieHash := ComputeCookieIndicesHash(req, effectiveCookieNames)

	_ = cfg.Store.Set(req.Context(), CacheKey{
		Method:     req.Method,
		URL:        normURL,
		CookieHash: cookieHash,
	}, cachedData, ttl)
}

func resolveEffectiveCookieIndices(resp *http.Response, cfg *CacheConfig) []string {
	ciHeader := resp.Header.Get("Cookie-Indices")
	if ciHeader == "" {
		if cfg != nil {
			return cfg.CookieIndices
		}

		return nil
	}

	parsed := ParseCookieIndicesHeader(ciHeader)
	if cfg != nil && len(cfg.CookieIndices) > 0 {
		parsed = append(parsed, cfg.CookieIndices...)
	}

	return parsed
}

func resolveEffectiveNoVarySearch(resp *http.Response, cfg *CacheConfig) *NoVarySearchConfig {
	nvsHeader := resp.Header.Get("No-Vary-Search")
	if nvsHeader == "" {
		if cfg != nil {
			return cfg.NoVarySearch
		}

		return &DefaultNoVarySearchConfig
	}

	parsed := ParseNoVarySearchHeader(nvsHeader)
	if cfg != nil && cfg.NoVarySearch != nil {
		parsed.IgnoreParams = append(parsed.IgnoreParams, cfg.NoVarySearch.IgnoreParams...)
	}

	return parsed
}

// ParseNoVarySearchHeader parses the W3C 'No-Vary-Search' response header into a NoVarySearchConfig.
func ParseNoVarySearchHeader(header string) *NoVarySearchConfig {
	cfg := &NoVarySearchConfig{}
	if header == "" {
		return cfg
	}

	if strings.Contains(header, "params") && !strings.Contains(header, "params=(") {
		cfg.IgnoreAllParams = true
		return cfg
	}

	if start := strings.Index(header, "params=("); start != -1 {
		end := strings.IndexByte(header[start:], ')')
		if end != -1 {
			paramsStr := header[start+8 : start+end]
			cfg.IgnoreParams = parseHeaderParamsList(paramsStr)
		}
	}

	if start := strings.Index(header, "except=("); start != -1 {
		end := strings.IndexByte(header[start:], ')')
		if end != -1 {
			paramsStr := header[start+8 : start+end]
			cfg.ExceptParams = parseHeaderParamsList(paramsStr)
		}
	}

	return cfg
}

func parseHeaderParamsList(paramsStr string) []string {
	var params []string
	for p := range strings.FieldsSeq(paramsStr) {
		cleaned := strings.Trim(p, `"'`)
		if cleaned != "" {
			params = append(params, cleaned)
		}
	}

	return params
}

func (p *Pipeline) invalidateCache(req *http.Request, resp *http.Response, cfg *CacheConfig) {
	if cfg == nil || cfg.Store == nil || resp == nil || resp.StatusCode >= 400 {
		return
	}

	if req.Method == http.MethodPost || req.Method == http.MethodPut ||
		req.Method == http.MethodDelete || req.Method == http.MethodPatch {
		normURL := NormalizeCacheURL(req.URL.String(), cfg.NoVarySearch)
		key := CacheKey{Method: http.MethodGet, URL: normURL}
		_ = cfg.Store.Set(req.Context(), key, nil, 0)
	}
}

func extractVaryHeaders(req *http.Request, varyHeader string) map[string]string {
	if varyHeader == "" {
		return nil
	}

	varyMap := make(map[string]string)
	for p := range strings.SplitSeq(varyHeader, ",") {
		hName := strings.TrimSpace(p)
		if hName != "" && hName != "*" {
			varyMap[hName] = req.Header.Get(hName)
		}
	}

	return varyMap
}
