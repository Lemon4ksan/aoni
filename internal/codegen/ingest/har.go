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

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/lemon4ksan/foundation/generic"
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
func HARToOpenAPI(data []byte) (*openapi3.T, error) {
	var har HARLog
	if err := json.Unmarshal(data, &har); err != nil {
		return nil, fmt.Errorf("parsing HAR JSON: %w", err)
	}

	doc := &openapi3.T{
		OpenAPI: "3.0.3",
		Info: &openapi3.Info{
			Title:       "API Specification (Captured from Traffic)",
			Version:     "1.0.0",
			Description: "Synthesized automatically by Vortex HAR Traffic Ingestion Engine",
		},
		Paths: openapi3.NewPaths(),
		Components: &openapi3.Components{
			Schemas: make(openapi3.Schemas),
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
		pathItem := doc.Paths.Value(cleanPath)
		if pathItem == nil {
			pathItem = &openapi3.PathItem{}
			doc.Paths.Set(cleanPath, pathItem)
		}

		var op *openapi3.Operation
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
			op = openapi3.NewOperation()
			op.OperationID = DeriveMethodNameFromRoute(method, cleanPath)

			op.Summary = fmt.Sprintf("%s %s", method, cleanPath)
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

			param := op.Parameters.GetByInAndName(openapi3.ParameterInQuery, q.Name)
			if param == nil {
				param = openapi3.NewQueryParameter(q.Name).WithSchema(openapi3.NewStringSchema())
				op.AddParameter(param)
			}
		}

		// 2. Ingest & Union Request Body (and register DTO struct in Components)
		if entry.Request.PostData != nil && len(entry.Request.PostData.Text) > 0 {
			schema := inferSchemaFromJSON([]byte(entry.Request.PostData.Text))
			if schema != nil {
				var schemaRef *openapi3.SchemaRef
				if (schema.Type != nil && schema.Type.Includes("object")) || len(schema.Properties) > 0 {
					dtoName := op.OperationID + "Request"
					if existing, exists := doc.Components.Schemas[dtoName]; exists && existing.Value != nil {
						existing.Value = MergeSchemas(existing.Value, schema)
						schema = existing.Value
					} else {
						doc.Components.Schemas[dtoName] = &openapi3.SchemaRef{Value: schema}
					}

					schemaRef = &openapi3.SchemaRef{
						Ref:   "#/components/schemas/" + dtoName,
						Value: schema,
					}
				} else {
					schemaRef = &openapi3.SchemaRef{Value: schema}
				}

				if op.RequestBody == nil || op.RequestBody.Value == nil {
					reqBody := openapi3.NewRequestBody().WithContent(openapi3.NewContentWithJSONSchemaRef(schemaRef))
					op.RequestBody = &openapi3.RequestBodyRef{Value: reqBody}
				} else {
					op.RequestBody.Value.Content["application/json"] = openapi3.NewMediaType().WithSchemaRef(schemaRef)
				}
			}
		}

		// 3. Ingest & Union Response Body & Status Codes (and register DTO struct in Components)
		statusStr := strconv.Itoa(entry.Response.Status)
		if entry.Response.Status == 0 {
			statusStr = "200"
		}

		statusInt, _ := strconv.Atoi(statusStr)

		var respSchema *openapi3.Schema
		if entry.Response.Content != nil && len(entry.Response.Content.Text) > 0 {
			respSchema = inferSchemaFromJSON([]byte(entry.Response.Content.Text))
		}

		var respSchemaRef *openapi3.SchemaRef
		if respSchema != nil {
			switch {
			case (respSchema.Type != nil && respSchema.Type.Includes("object")) || len(respSchema.Properties) > 0:
				dtoName := op.OperationID + "Response"
				if existing, exists := doc.Components.Schemas[dtoName]; exists && existing.Value != nil {
					existing.Value = MergeSchemas(existing.Value, respSchema)
					respSchema = existing.Value
				} else {
					doc.Components.Schemas[dtoName] = &openapi3.SchemaRef{Value: respSchema}
				}

				respSchemaRef = &openapi3.SchemaRef{
					Ref:   "#/components/schemas/" + dtoName,
					Value: respSchema,
				}

			case respSchema.Type != nil && respSchema.Type.Includes("array") && respSchema.Items != nil &&
				respSchema.Items.Value != nil && len(respSchema.Items.Value.Properties) > 0:
				itemDtoName := op.OperationID + "Item"
				if existing, exists := doc.Components.Schemas[itemDtoName]; exists && existing.Value != nil {
					existing.Value = MergeSchemas(existing.Value, respSchema.Items.Value)
					respSchema.Items.Value = existing.Value
				} else {
					doc.Components.Schemas[itemDtoName] = &openapi3.SchemaRef{Value: respSchema.Items.Value}
				}

				respSchema.Items = &openapi3.SchemaRef{
					Ref:   "#/components/schemas/" + itemDtoName,
					Value: respSchema.Items.Value,
				}
				respSchemaRef = &openapi3.SchemaRef{Value: respSchema}

			default:
				respSchemaRef = &openapi3.SchemaRef{Value: respSchema}
			}
		}

		respRef := op.Responses.Value(statusStr)
		if respRef == nil {
			resp := openapi3.NewResponse().WithDescription("Response " + statusStr)
			if respSchemaRef != nil {
				resp.WithContent(openapi3.NewContentWithJSONSchemaRef(respSchemaRef))
			}

			op.AddResponse(statusInt, resp)
		} else if respRef.Value != nil && respSchemaRef != nil {
			respRef.Value.Content["application/json"] = openapi3.NewMediaType().WithSchemaRef(respSchemaRef)
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
		doc.Servers = append(doc.Servers, &openapi3.Server{
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
func MergeSchemas(s1, s2 *openapi3.Schema) *openapi3.Schema {
	if s1 == nil {
		return s2
	}

	if s2 == nil {
		return s1
	}

	if s1.Type != nil && s2.Type != nil {
		if s1.Type.Includes("object") && s2.Type.Includes("object") {
			if s1.Properties == nil {
				s1.Properties = make(openapi3.Schemas)
			}

			for k, v := range s2.Properties {
				if existingProp, exists := s1.Properties[k]; exists && existingProp.Value != nil && v.Value != nil {
					existingProp.Value = MergeSchemas(existingProp.Value, v.Value)
				} else {
					s1.Properties[k] = v
				}
			}

			return s1
		}

		if s1.Type.Includes("array") && s2.Type.Includes("array") && s1.Items != nil && s2.Items != nil &&
			s1.Items.Value != nil &&
			s2.Items.Value != nil {
			s1.Items.Value = MergeSchemas(s1.Items.Value, s2.Items.Value)
			return s1
		}
	}

	return s1
}

func inferSchemaFromJSON(data []byte) *openapi3.Schema {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return openapi3.NewStringSchema()
	}

	return valueToSchema(raw)
}

func valueToSchema(v any) *openapi3.Schema {
	if v == nil {
		return openapi3.NewStringSchema()
	}

	switch val := v.(type) {
	case bool:
		return openapi3.NewBoolSchema()
	case float64:
		if val == float64(int64(val)) {
			return openapi3.NewInt64Schema()
		}

		return openapi3.NewFloat64Schema()

	case string:
		if _, err := time.Parse(time.RFC3339, val); err == nil && len(val) >= 19 {
			return openapi3.NewDateTimeSchema()
		}

		return openapi3.NewStringSchema()

	case []any:
		arr := openapi3.NewArraySchema()
		if len(val) > 0 {
			arr.Items = &openapi3.SchemaRef{Value: valueToSchema(val[0])}
		} else {
			arr.Items = &openapi3.SchemaRef{Value: openapi3.NewStringSchema()}
		}

		return arr

	case map[string]any:
		obj := openapi3.NewObjectSchema()

		keys := generic.Keys(val)
		slices.Sort(keys)

		for _, k := range keys {
			obj.Properties[k] = &openapi3.SchemaRef{Value: valueToSchema(val[k])}
		}

		return obj

	default:
		return openapi3.NewStringSchema()
	}
}

// UnifyComponentsSchemas deduplicates identical schemas in Components.Schemas
// by mapping structurally equivalent models to a single unified canonical DTO.
func UnifyComponentsSchemas(doc *openapi3.T) {
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
		ref := doc.Components.Schemas[name]
		if ref == nil || ref.Value == nil || len(ref.Value.Properties) < 2 {
			continue
		}

		sig := computeSchemaFingerprint(ref.Value)
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
		for _, pathItem := range doc.Paths.Map() {
			if pathItem == nil {
				continue
			}

			for _, op := range pathItem.Operations() {
				if op == nil {
					continue
				}

				if op.RequestBody != nil && op.RequestBody.Value != nil {
					for _, media := range op.RequestBody.Value.Content {
						rewriteSchemaRef(media.Schema, replacements)
					}
				}

				if op.Responses != nil {
					for _, respRef := range op.Responses.Map() {
						if respRef != nil && respRef.Value != nil {
							for _, media := range respRef.Value.Content {
								rewriteSchemaRef(media.Schema, replacements)
							}
						}
					}
				}
			}
		}
	}
}

func computeSchemaFingerprint(s *openapi3.Schema) string {
	if s == nil || len(s.Properties) == 0 {
		return ""
	}

	keys := generic.Keys(s.Properties)
	slices.Sort(keys)

	var sb strings.Builder
	for _, k := range keys {
		prop := s.Properties[k]

		typeStr := "unknown"
		if prop != nil && prop.Value != nil && prop.Value.Type != nil && len(*prop.Value.Type) > 0 {
			typeStr = (*prop.Value.Type)[0]
		}

		sb.WriteString(k)
		sb.WriteString(":")
		sb.WriteString(typeStr)
		sb.WriteString(";")
	}

	return sb.String()
}

func rewriteSchemaRef(ref *openapi3.SchemaRef, replacements map[string]string) {
	if ref == nil {
		return
	}

	if ref.Ref != "" {
		const prefix = "#/components/schemas/"
		if strings.HasPrefix(ref.Ref, prefix) {
			oldName := strings.TrimPrefix(ref.Ref, prefix)
			if canonical, ok := replacements[oldName]; ok {
				ref.Ref = prefix + canonical
			}
		}
	}

	if ref.Value != nil {
		if ref.Value.Items != nil {
			rewriteSchemaRef(ref.Value.Items, replacements)
		}

		for _, p := range ref.Value.Properties {
			rewriteSchemaRef(p, replacements)
		}
	}
}
