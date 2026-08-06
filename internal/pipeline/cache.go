// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	stdio "io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (p *Pipeline) tryGetFromCache(req *http.Request, cfg *CacheConfig) *http.Response {
	if req.Method != http.MethodGet || cfg == nil || cfg.Store == nil {
		return nil
	}

	cc := req.Header.Get("Cache-Control")
	if strings.Contains(cc, "no-cache") || strings.Contains(cc, "no-store") {
		return nil
	}

	cachedData, err := cfg.Store.Get(req.Context(), CacheKey{Method: req.Method, URL: req.URL.String()})
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

	_ = cfg.Store.Set(req.Context(), CacheKey{Method: req.Method, URL: req.URL.String()}, cachedData, ttl)
}

func (p *Pipeline) invalidateCache(req *http.Request, resp *http.Response, cfg *CacheConfig) {
	if cfg == nil || cfg.Store == nil || resp == nil || resp.StatusCode >= 400 {
		return
	}

	if req.Method == http.MethodPost || req.Method == http.MethodPut ||
		req.Method == http.MethodDelete || req.Method == http.MethodPatch {
		key := CacheKey{Method: http.MethodGet, URL: req.URL.String()}
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
