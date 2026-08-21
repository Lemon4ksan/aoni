// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ingest

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lemon4ksan/foundation/generic"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/openapi"
)

// HARLog models the top-level container of a W3C HAR 1.2 archive.
type HARLog struct {
	Log struct {
		Entries []HAREntry `json:"entries"`
	} `json:"log"`
}

// HAREntry describes an individual HTTP request-response transaction in a HAR log.
type HAREntry struct {
	Request struct {
		Method      string       `json:"method"`
		URL         string       `json:"url"`
		Headers     []HARNV      `json:"headers"`
		QueryString []HARNV      `json:"queryString"`
		PostData    *HARPostData `json:"postData,omitempty"`
	} `json:"request"`
	Response struct {
		Status  int         `json:"status"`
		Headers []HARNV     `json:"headers"`
		Content *HARContent `json:"content,omitempty"`
	} `json:"response"`
}

// HARNV models a name-value pair for headers or query parameters.
type HARNV struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// HARPostData models the submitted request payload.
type HARPostData struct {
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
	Encoding string `json:"encoding,omitempty"`
}

// HARContent models the received response payload.
type HARContent struct {
	Size     int64  `json:"size"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
	Encoding string `json:"encoding,omitempty"`
}

// HARToOpenAPI transforms one or more recorded W3C HAR 1.2 logs into an OpenAPI 3.0 specification,
// automatically unifying query parameters, status codes, and schema object properties.
func HARToOpenAPI(data []byte) (*openapi.Document, error) {
	var har HARLog
	if err := json.Unmarshal(data, &har); err != nil {
		return nil, fmt.Errorf("parsing HAR JSON: %w", err)
	}

	doc := &openapi.Document{
		OpenAPI: "3.0.3",
		Info: &openapi.Info{
			Title:       "API Specification (Captured from Traffic)",
			Version:     "1.0.0",
			Description: "Synthesized automatically by Vortex HAR Traffic Ingestion Engine",
		},
		Paths: make(map[string]*openapi.PathItem),
		Components: &openapi.Components{
			Schemas: make(map[string]*openapi.Schema),
		},
	}

	for _, entry := range har.Log.Entries {
		rawURL := entry.Request.URL

		u, err := url.Parse(rawURL)
		if err != nil {
			continue
		}

		cleanPath := u.Path
		if cleanPath == "" {
			cleanPath = "/"
		}

		method := strings.ToUpper(entry.Request.Method)
		if method == "OPTIONS" || method == "HEAD" || isIgnoredHAREndpoint(cleanPath, rawURL) {
			continue
		}

		// Extract or create PathItem
		pathItem := doc.Paths[cleanPath]
		if pathItem == nil {
			pathItem = &openapi.PathItem{}
			doc.Paths[cleanPath] = pathItem
		}

		var op *openapi.Operation
		switch method {
		case "GET":
			op = pathItem.Get
		case "POST":
			op = pathItem.Post
		case "PUT":
			op = pathItem.Put
		case "DELETE":
			op = pathItem.Delete
		case "PATCH":
			op = pathItem.Patch
		}

		if op == nil {
			op = &openapi.Operation{
				OperationID: DeriveMethodNameFromRoute(method, cleanPath),
				Summary:     fmt.Sprintf("%s %s", method, cleanPath),
				Responses:   make(map[string]*openapi.Response),
			}

			switch method {
			case "GET":
				pathItem.Get = op
			case "POST":
				pathItem.Post = op
			case "PUT":
				pathItem.Put = op
			case "DELETE":
				pathItem.Delete = op
			case "PATCH":
				pathItem.Patch = op
			}
		}

		// 1. Ingest & Union Query Parameters
		for _, q := range entry.Request.QueryString {
			if q.Name == "" {
				continue
			}

			hasParam := false
			for _, p := range op.Parameters {
				if p.In == "query" && p.Name == q.Name {
					hasParam = true
					break
				}
			}

			if !hasParam {
				op.Parameters = append(op.Parameters, &openapi.Parameter{
					Name:   q.Name,
					In:     "query",
					Schema: &openapi.Schema{Type: openapi.TypeArray{"string"}},
				})
			}
		}

		// 2. Ingest & Union Request Body (and register DTO struct in Components)
		if entry.Request.PostData != nil && len(entry.Request.PostData.Text) > 0 {
			schema := inferSchemaFromJSON([]byte(entry.Request.PostData.Text))
			if schema != nil {
				var schemaRef *openapi.Schema
				if schema.IsType("object") || len(schema.Properties) > 0 {
					dtoName := op.OperationID + "Request"
					if existing, exists := doc.Components.Schemas[dtoName]; exists && existing != nil {
						schema = MergeSchemas(existing, schema)
						doc.Components.Schemas[dtoName] = schema
					} else {
						doc.Components.Schemas[dtoName] = schema
					}

					schemaRef = &openapi.Schema{
						Ref: "#/components/schemas/" + dtoName,
					}
				} else {
					schemaRef = schema
				}

				if op.RequestBody == nil {
					op.RequestBody = &openapi.RequestBody{
						Content: map[string]*openapi.MediaType{
							"application/json": {
								Schema: schemaRef,
							},
						},
					}
				} else {
					if op.RequestBody.Content == nil {
						op.RequestBody.Content = make(map[string]*openapi.MediaType)
					}
					op.RequestBody.Content["application/json"] = &openapi.MediaType{
						Schema: schemaRef,
					}
				}
			}
		}

		// 3. Ingest & Union Response Body & Status Codes (and register DTO struct in Components)
		statusStr := strconv.Itoa(entry.Response.Status)
		if entry.Response.Status == 0 {
			statusStr = "200"
		}

		var respSchema *openapi.Schema
		if entry.Response.Content != nil && len(entry.Response.Content.Text) > 0 {
			respSchema = inferSchemaFromJSON([]byte(entry.Response.Content.Text))
		}

		var respSchemaRef *openapi.Schema
		if respSchema != nil {
			switch {
			case respSchema.IsType("object") || len(respSchema.Properties) > 0:
				dtoName := op.OperationID + "Response"
				if existing, exists := doc.Components.Schemas[dtoName]; exists && existing != nil {
					respSchema = MergeSchemas(existing, respSchema)
					doc.Components.Schemas[dtoName] = respSchema
				} else {
					doc.Components.Schemas[dtoName] = respSchema
				}

				respSchemaRef = &openapi.Schema{
					Ref: "#/components/schemas/" + dtoName,
				}

			case respSchema.IsType("array") && respSchema.Items != nil && len(respSchema.Items.Properties) > 0:
				itemDtoName := op.OperationID + "Item"
				if existing, exists := doc.Components.Schemas[itemDtoName]; exists && existing != nil {
					respSchema.Items = MergeSchemas(existing, respSchema.Items)
					doc.Components.Schemas[itemDtoName] = respSchema.Items
				} else {
					doc.Components.Schemas[itemDtoName] = respSchema.Items
				}

				respSchema.Items = &openapi.Schema{
					Ref: "#/components/schemas/" + itemDtoName,
				}
				respSchemaRef = respSchema

			default:
				respSchemaRef = respSchema
			}
		}

		if op.Responses == nil {
			op.Responses = make(map[string]*openapi.Response)
		}

		existingResp := op.Responses[statusStr]
		if existingResp == nil {
			newResp := &openapi.Response{
				Description: "Response " + statusStr,
				Content:     make(map[string]*openapi.MediaType),
			}
			if respSchemaRef != nil {
				newResp.Content["application/json"] = &openapi.MediaType{
					Schema: respSchemaRef,
				}
			}
			op.Responses[statusStr] = newResp
		} else if respSchemaRef != nil {
			if existingResp.Content == nil {
				existingResp.Content = make(map[string]*openapi.MediaType)
			}
			existingResp.Content["application/json"] = &openapi.MediaType{
				Schema: respSchemaRef,
			}
		}

		// 4. Ingest operation-specific request headers
		var opHeaders []map[string]string
		if op.Extensions != nil {
			if raw, ok := op.Extensions["x-vortex-headers"].([]map[string]string); ok {
				opHeaders = raw
			}
		}

		opHeaderMap := make(map[string]string)
		for _, h := range opHeaders {
			opHeaderMap[h["name"]] = h["value"]
		}

		for _, h := range entry.Request.Headers {
			name := strings.TrimSpace(h.Name)
			val := strings.TrimSpace(h.Value)

			if isMeaningfulClientHeader(name) && val != "" {
				val = sanitizeHeaderValue(name, val)
				if _, exists := opHeaderMap[name]; !exists {
					opHeaderMap[name] = val
					opHeaders = append(opHeaders, map[string]string{
						"name":  name,
						"value": val,
					})
				}
			}
		}

		if len(opHeaders) > 0 {
			if op.Extensions == nil {
				op.Extensions = make(map[string]any)
			}

			op.Extensions["x-vortex-headers"] = opHeaders
		}

		var requiredCredentials []string
		if op.Extensions != nil {
			if raw, ok := op.Extensions["x-required-credentials"].([]string); ok {
				requiredCredentials = raw
			}
		}

		credSet := make(map[string]bool)
		for _, c := range requiredCredentials {
			credSet[c] = true
		}

		for _, h := range entry.Request.Headers {
			name := strings.TrimSpace(h.Name)
			val := strings.TrimSpace(h.Value)
			lower := strings.ToLower(name)

			if isCredentialHeader(lower) && val != "" {
				sanVal := sanitizeHeaderValue(name, val)

				label := fmt.Sprintf("%s: %s", name, sanVal)
				if !credSet[label] {
					credSet[label] = true
					requiredCredentials = append(requiredCredentials, label)
				}
			}
		}

		if len(requiredCredentials) > 0 {
			if op.Extensions == nil {
				op.Extensions = make(map[string]any)
			}

			op.Extensions["x-required-credentials"] = requiredCredentials
		}
	}

	// 4. Extract BaseURL and common client headers across transactions
	headerCounts := make(map[string]map[string]int)
	totalEntries := len(har.Log.Entries)

	var detectedHost string

	for _, entry := range har.Log.Entries {
		if u, err := url.Parse(entry.Request.URL); err == nil && u.Host != "" && detectedHost == "" {
			detectedHost = u.Scheme + "://" + u.Host
		}

		for _, h := range entry.Request.Headers {
			name := strings.TrimSpace(h.Name)

			val := strings.TrimSpace(h.Value)
			if isMeaningfulClientHeader(name) && val != "" {
				val = sanitizeHeaderValue(name, val)
				if headerCounts[name] == nil {
					headerCounts[name] = make(map[string]int)
				}

				headerCounts[name][val]++
			}
		}
	}

	if detectedHost != "" {
		doc.Servers = append(doc.Servers, openapi.Server{
			URL:         detectedHost,
			Description: "API Server (inferred from captured traffic)",
		})
	}

	var commonHeaders []map[string]string
	for name, valMap := range headerCounts {
		for val, count := range valMap {
			if count >= 1 && (totalEntries <= 2 || count*2 >= totalEntries) {
				commonHeaders = append(commonHeaders, map[string]string{
					"name":  name,
					"value": val,
				})
			}
		}
	}

	if len(commonHeaders) > 0 {
		slices.SortFunc(commonHeaders, func(a, b map[string]string) int {
			return cmp.Compare(a["name"], b["name"])
		})

		if doc.Info.Extensions == nil {
			doc.Info.Extensions = make(map[string]any)
		}

		doc.Info.Extensions["x-vortex-headers"] = commonHeaders
	}

	var allCredHeaders []string

	allCredSet := make(map[string]bool)
	for _, entry := range har.Log.Entries {
		for _, h := range entry.Request.Headers {
			name := strings.TrimSpace(h.Name)

			lower := strings.ToLower(name)
			if isCredentialHeader(lower) && h.Value != "" && !allCredSet[lower] {
				allCredSet[lower] = true

				allCredHeaders = append(allCredHeaders, name)
			}
		}
	}

	if len(allCredHeaders) > 0 {
		if doc.Info.Extensions == nil {
			doc.Info.Extensions = make(map[string]any)
		}

		doc.Info.Extensions["x-required-credentials"] = allCredHeaders
	}

	UnifyComponentsSchemas(doc)

	return doc, nil
}

func isIgnoredHAREndpoint(cleanPath, rawURL string) bool {
	lowerPath := strings.ToLower(cleanPath)
	lowerURL := strings.ToLower(rawURL)

	// 1. Static file extensions
	for _, ext := range []string{".js", ".css", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico", ".woff", ".woff2", ".ttf", ".webp", ".mp4", ".map"} {
		if strings.HasSuffix(lowerPath, ext) {
			return true
		}
	}

	// 2. Telemetry, analytics, and noise tracking endpoints
	noisePatterns := []string{
		"/gen_204", "/csi", "/log", "/batch", "/survey/", "/cookienotificationbar",
		"/static/proxy.html", "/pagead/", "google-analytics.com", "doubleclick.net",
		"/a/acg8", // Google user profile photos
	}
	for _, p := range noisePatterns {
		if strings.Contains(lowerPath, p) || strings.Contains(lowerURL, p) {
			return true
		}
	}

	return false
}

func isCredentialHeader(name string) bool {
	lower := strings.ToLower(name)
	if lower == "authorization" || lower == "proxy-authorization" {
		return true
	}

	return strings.Contains(lower, "api-key") || strings.Contains(lower, "apikey") ||
		strings.Contains(lower, "token") || strings.Contains(lower, "secret") ||
		strings.Contains(lower, "auth") || strings.Contains(lower, "signature") ||
		strings.Contains(lower, "session-id") || strings.Contains(lower, "visit-id") ||
		strings.Contains(lower, "instanceid") || strings.Contains(lower, "connection-id")
}

func isMeaningfulClientHeader(name string) bool {
	if strings.HasPrefix(name, ":") {
		return false
	}

	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, "sec-") {
		return false
	}

	if isCredentialHeader(name) {
		return false
	}

	switch lower {
	case "host", "content-length", "content-type", "connection", "proxy-connection",
		"accept-encoding", "transfer-encoding", "cookie", "accept-language", "priority",
		"cache-control", "pragma", "referer", "origin", "x-clientdetails",
		"x-javascript-user-agent", "x-requested-with", "x-referer", "x-origin",
		"x-goog-encode-response-if-executable", "if-none-match":
		return false
	default:
		return true
	}
}

func sanitizeHeaderValue(name, val string) string {
	lowerName := strings.ToLower(name)

	// Authorization tokens
	if lowerName == "authorization" {
		if strings.HasPrefix(val, "Bearer ") || strings.HasPrefix(val, "bearer ") {
			return "Bearer ${AUTH_TOKEN}"
		}

		if strings.HasPrefix(val, "*:") {
			return "*:${PRODUCTION_TOKEN}"
		}

		return "${AUTH_TOKEN}"
	}

	if isCredentialHeader(name) {
		clean := strings.ToLower(name)
		clean = strings.TrimPrefix(clean, "x-")
		clean = strings.TrimPrefix(clean, "x_")
		clean = strings.ReplaceAll(clean, "-", "_")
		clean = strings.ReplaceAll(clean, ".", "_")

		return "${" + strings.ToUpper(clean) + "}"
	}

	return val
}

// MergeSchemas recursively unions properties and items of two OpenAPI schemas.
func MergeSchemas(s1, s2 *openapi.Schema) *openapi.Schema {
	if s1 == nil {
		return s2
	}

	if s2 == nil {
		return s1
	}

	if len(s1.Type) > 0 && len(s2.Type) > 0 {
		if s1.IsType("object") && s2.IsType("object") {
			if s1.Properties == nil {
				s1.Properties = make(map[string]*openapi.Schema)
			}

			for k, v := range s2.Properties {
				if existingProp, exists := s1.Properties[k]; exists && existingProp != nil && v != nil {
					s1.Properties[k] = MergeSchemas(existingProp, v)
				} else {
					s1.Properties[k] = v
				}
			}

			return s1
		}

		if s1.IsType("array") && s2.IsType("array") && s1.Items != nil && s2.Items != nil {
			s1.Items = MergeSchemas(s1.Items, s2.Items)
			return s1
		}
	}

	return s1
}

func inferSchemaFromJSON(data []byte) *openapi.Schema {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return &openapi.Schema{Type: openapi.TypeArray{"string"}}
	}

	return valueToSchema(raw)
}

func valueToSchema(v any) *openapi.Schema {
	if v == nil {
		return &openapi.Schema{Type: openapi.TypeArray{"string"}}
	}

	switch val := v.(type) {
	case bool:
		return &openapi.Schema{Type: openapi.TypeArray{"boolean"}}
	case float64:
		if val == float64(int64(val)) {
			return &openapi.Schema{Type: openapi.TypeArray{"integer"}, Format: "int64"}
		}

		return &openapi.Schema{Type: openapi.TypeArray{"number"}, Format: "double"}

	case string:
		if _, err := time.Parse(time.RFC3339, val); err == nil && len(val) >= 19 {
			return &openapi.Schema{Type: openapi.TypeArray{"string"}, Format: "date-time"}
		}

		return &openapi.Schema{Type: openapi.TypeArray{"string"}}

	case []any:
		arr := &openapi.Schema{Type: openapi.TypeArray{"array"}}
		if len(val) > 0 {
			arr.Items = valueToSchema(val[0])
		} else {
			arr.Items = &openapi.Schema{Type: openapi.TypeArray{"string"}}
		}

		return arr

	case map[string]any:
		obj := &openapi.Schema{
			Type:       openapi.TypeArray{"object"},
			Properties: make(map[string]*openapi.Schema),
		}

		keys := generic.Keys(val)
		slices.Sort(keys)

		for _, k := range keys {
			obj.Properties[k] = valueToSchema(val[k])
		}

		return obj

	default:
		return &openapi.Schema{Type: openapi.TypeArray{"string"}}
	}
}

// UnifyComponentsSchemas deduplicates identical schemas in Components.Schemas
// by mapping structurally equivalent models to a single unified canonical DTO.
func UnifyComponentsSchemas(doc *openapi.Document) {
	if doc == nil || doc.Components == nil || len(doc.Components.Schemas) <= 1 {
		return
	}

	type schemaGroup struct {
		canonicalName string
		aliases       []string
	}

	sigToGroup := make(map[string]*schemaGroup)

	// Collect schema names deterministically
	names := generic.Keys(doc.Components.Schemas)
	slices.Sort(names)

	for _, name := range names {
		schema := doc.Components.Schemas[name]
		if schema == nil || len(schema.Properties) < 2 {
			continue
		}

		sig := computeSchemaFingerprint(schema)
		if sig == "" {
			continue
		}

		if grp, exists := sigToGroup[sig]; exists {
			grp.aliases = append(grp.aliases, name)
		} else {
			sigToGroup[sig] = &schemaGroup{
				canonicalName: name,
				aliases:       nil,
			}
		}
	}

	// Build rename map: oldName -> canonicalName
	replacements := make(map[string]string)
	for _, grp := range sigToGroup {
		for _, alias := range grp.aliases {
			replacements[alias] = grp.canonicalName
			delete(doc.Components.Schemas, alias)
		}
	}

	if len(replacements) == 0 {
		return
	}

	// Re-point all references in paths
	if doc.Paths != nil {
		for _, pathItem := range doc.Paths {
			if pathItem == nil {
				continue
			}

			for _, op := range pathItem.OperationsMap() {
				if op == nil {
					continue
				}

				if op.RequestBody != nil {
					for _, media := range op.RequestBody.Content {
						if media != nil {
							rewriteSchemaRef(media.Schema, replacements)
						}
					}
				}

				if op.Responses != nil {
					for _, resp := range op.Responses {
						if resp != nil {
							for _, media := range resp.Content {
								if media != nil {
									rewriteSchemaRef(media.Schema, replacements)
								}
							}
						}
					}
				}
			}
		}
	}
}

func computeSchemaFingerprint(s *openapi.Schema) string {
	if s == nil || len(s.Properties) == 0 {
		return ""
	}

	keys := generic.Keys(s.Properties)
	slices.Sort(keys)

	var sb strings.Builder
	for _, k := range keys {
		prop := s.Properties[k]

		typeStr := "unknown"
		if prop != nil && len(prop.Type) > 0 {
			typeStr = prop.Type.Primary()
		}

		sb.WriteString(k)
		sb.WriteString(":")
		sb.WriteString(typeStr)
		sb.WriteString(";")
	}

	return sb.String()
}

func rewriteSchemaRef(s *openapi.Schema, replacements map[string]string) {
	if s == nil {
		return
	}

	if s.Ref != "" {
		const prefix = "#/components/schemas/"
		if strings.HasPrefix(s.Ref, prefix) {
			oldName := strings.TrimPrefix(s.Ref, prefix)
			if canonical, ok := replacements[oldName]; ok {
				s.Ref = prefix + canonical
			}
		}
	}

	if s.Items != nil {
		rewriteSchemaRef(s.Items, replacements)
	}

	for _, p := range s.Properties {
		rewriteSchemaRef(p, replacements)
	}
}
