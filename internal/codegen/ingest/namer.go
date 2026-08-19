// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package ingest implements generic specification detection and intelligent naming normalizers.
package ingest

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	// Regex matching redundant generated verb prefixes (e.g. "GetIGet", "PostIPost").
	reUglyVerbPrefix = regexp.MustCompile(`^(?i)(?:get|post|put|delete)?i(?:get|post|put|delete|list)?`)

	// Regex matching version suffixes like "_v0001", "_v0002", "_v1", "_v2", "V1", "V4".
	reVersionSuffix = regexp.MustCompile(`(?i)_?v[0-9]+$`)

	// Regex matching version path segments like "v1", "v2", "v0001".
	reVersionPath = regexp.MustCompile(`(?i)^v[0-9]+$`)

	// Regex matching trailing HTTP verbs in snake or camel case.
	reTrailingVerb = regexp.MustCompile(`(?i)_(get|post|put|delete|patch|options|head)$`)

	// Regex for non-alphanumeric characters.
	reNonAlphanumeric = regexp.MustCompile(`[^a-zA-Z0-9]+`)
)

// Common Go initialisms that should remain capitalized according to Go CodeReviewComments.
var commonInitialisms = map[string]string{
	"API":   "API",
	"ID":    "ID",
	"IP":    "IP",
	"HTTP":  "HTTP",
	"HTTPS": "HTTPS",
	"JSON":  "JSON",
	"URL":   "URL",
	"URI":   "URI",
	"XML":   "XML",
	"TTL":   "TTL",
	"RPC":   "RPC",
	"UUID":  "UUID",
	"SKU":   "SKU",
	"DTO":   "DTO",
}

// SanitizeMethodName transforms raw operation IDs or URL routes into clean, idiomatic Go method names.
func SanitizeMethodName(rawOpID, httpMethod, rawPath, serviceName string) string {
	cleaned := strings.TrimSpace(rawOpID)

	if cleaned != "" {
		// 1. Strip redundant generated IGet / IPost prefixes
		if reUglyVerbPrefix.MatchString(cleaned) && len(cleaned) > 5 {
			cleaned = reUglyVerbPrefix.ReplaceAllString(cleaned, "Get")
		}

		// 2. Strip service name prefix if present (e.g. "UserService_GetBalance" -> "GetBalance")
		if serviceName != "" {
			trimmedSvc := strings.TrimSuffix(serviceName, "API")
			trimmedSvc = strings.TrimSuffix(trimmedSvc, "Service")
			cleaned = strings.TrimPrefix(cleaned, serviceName+"_")

			cleaned = strings.TrimPrefix(cleaned, serviceName)
			if trimmedSvc != "" {
				cleaned = strings.TrimPrefix(cleaned, trimmedSvc+"_")
				cleaned = strings.TrimPrefix(cleaned, trimmedSvc)
			}
		}

		// 3. Strip version suffix (e.g. "_v0001", "_v1", "_v2", "V1", "V4")
		cleaned = reVersionSuffix.ReplaceAllString(cleaned, "")

		// 4. Strip trailing HTTP verb suffix (e.g. "classifieds_alerts_get" -> "classifieds_alerts")
		cleaned = reTrailingVerb.ReplaceAllString(cleaned, "")

		// 5. Convert to PascalCase with proper Go acronyms
		name := ToPascalCase(cleaned)

		if len(name) > 0 {
			return name
		}
	}

	// Fallback: derive clean name from HTTP Method and Route Path
	return DeriveMethodNameFromRoute(httpMethod, rawPath)
}

// DeriveMethodNameFromRoute generates an idiomatic method name from HTTP verb and route path.
func DeriveMethodNameFromRoute(httpMethod, rawPath string) string {
	cleanPath := strings.Trim(rawPath, "/")
	parts := strings.Split(cleanPath, "/")

	var significant []string
	for _, p := range parts {
		if p == "" || p == "api" || reVersionPath.MatchString(p) {
			continue
		}

		// Strip variable braces like "{id}"
		if strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}") {
			varName := strings.Trim(p, "{}")
			significant = append(significant, "By", varName)
			continue
		}

		significant = append(significant, p)
	}

	if len(significant) == 0 {
		return ToPascalCase(httpMethod)
	}

	joined := strings.Join(significant, "_")
	pascal := ToPascalCase(joined)

	verb := strings.ToUpper(httpMethod)
	switch verb {
	case "GET":
		if !strings.HasPrefix(pascal, "Get") && !strings.HasPrefix(pascal, "List") &&
			!strings.HasPrefix(pascal, "Fetch") && !strings.HasPrefix(pascal, "Search") &&
			!strings.HasPrefix(pascal, "Find") && !strings.HasPrefix(pascal, "Check") {
			pascal = "Get" + pascal
		}

	case "POST":
		if !strings.HasPrefix(pascal, "Create") && !strings.HasPrefix(pascal, "Post") &&
			!strings.HasPrefix(pascal, "Add") && !strings.HasPrefix(pascal, "Set") &&
			!strings.HasPrefix(pascal, "Update") && !strings.HasPrefix(pascal, "Send") {
			pascal = "Create" + pascal
		}

	case "PUT", "PATCH":
		if !strings.HasPrefix(pascal, "Update") && !strings.HasPrefix(pascal, "Set") &&
			!strings.HasPrefix(pascal, "Modify") && !strings.HasPrefix(pascal, "Put") &&
			!strings.HasPrefix(pascal, "Patch") {
			pascal = "Update" + pascal
		}

	case "DELETE":
		if !strings.HasPrefix(pascal, "Delete") && !strings.HasPrefix(pascal, "Remove") &&
			!strings.HasPrefix(pascal, "Cancel") {
			pascal = "Delete" + pascal
		}
	}

	return pascal
}

// SanitizeTypeName cleans DTO and Schema names into idiomatic Go types.
func SanitizeTypeName(raw string) string {
	cleaned := strings.TrimSpace(raw)
	// Strip OpenAPI "#/components/schemas/" or "#/definitions/"
	if idx := strings.LastIndex(cleaned, "/"); idx != -1 {
		cleaned = cleaned[idx+1:]
	}

	cleaned = reVersionSuffix.ReplaceAllString(cleaned, "")

	return ToPascalCase(cleaned)
}

// ToPascalCase converts strings from snake_case, kebab-case, or space-separated into PascalCase with initialism awareness.
func ToPascalCase(s string) string {
	if s == "" {
		return ""
	}

	cleaned := reNonAlphanumeric.ReplaceAllLiteralString(s, " ")
	words := strings.Fields(cleaned)

	var sb strings.Builder
	for _, w := range words {
		upper := strings.ToUpper(w)
		if initVal, ok := commonInitialisms[upper]; ok {
			sb.WriteString(initVal)
			continue
		}

		runes := []rune(w)
		if len(runes) == 0 {
			continue
		}

		sb.WriteRune(unicode.ToUpper(runes[0]))

		for i := 1; i < len(runes); i++ {
			if i == 1 && unicode.IsUpper(runes[i]) && len(runes) > 2 {
				sb.WriteString(strings.ToLower(string(runes[1:])))
				break
			}

			sb.WriteRune(runes[i])
		}
	}

	res := sb.String()
	if len(res) > 0 && unicode.IsDigit(rune(res[0])) {
		res = "X" + res
	}

	return res
}
