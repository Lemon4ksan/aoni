// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package openapi

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/lemon4ksan/foundation/generic"
)

var (
	reUglyVerbPrefix = regexp.MustCompile(`^(?i)(?:get|post|put|delete)?i(?:get|post|put|delete|list)?`)
	reVersionSuffix  = regexp.MustCompile(`(?i)_?v[0-9]+$`)
	reVersionPath    = regexp.MustCompile(`(?i)^v[0-9]+$`)
	reTrailingVerb   = regexp.MustCompile(`(?i)_(get|post|put|delete|patch|options|head)$`)
)

var commonVerbs = []string{"Get", "Create", "Update", "Patch", "Delete", "List", "Fetch", "Find", "Head", "Options"}

// Quirk (Effective Go / Go Code Review Comments): Words in names that are initialisms
// or acronyms (e.g. "URL", "HTTP", "ID") must have a consistent case (all uppercase).
var initialisms = map[string]bool{
	"ACL": true, "API": true, "ASCII": true,
	"CPU": true, "CSS": true, "DNS": true,
	"EOF": true, "GUID": true, "HTML": true,
	"HTTP": true, "HTTPS": true, "ID": true,
	"IP": true, "JSON": true, "LHS": true,
	"QPS": true, "RAM": true, "RHS": true,
	"RPC": true, "SLA": true, "SMTP": true,
	"SQL": true, "SSH": true, "TCP": true,
	"TLS": true, "TTL": true, "UDP": true,
	"UI": true, "UID": true, "UUID": true,
	"URI": true, "URL": true, "UTF8": true,
	"VM": true, "XML": true, "XSRF": true,
	"XSS": true,
}

// Quirk (Go Language Specification §Keywords): Reserved Go keywords cannot be used
// as identifiers (parameter or variable names) without causing compiler syntax errors.
var goKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
}

// isHexHash detects auto-generated compiler hashes (e.g., "7f8b9c0d1e2f3a4b...") in operation IDs.
func isHexHash(s string) bool {
	if len(s) < 16 {
		return false
	}

	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}

	return true
}

// SanitizeMethodName transforms raw operation IDs or URL routes into clean, idiomatic Go method names.
//
// Quirk: Real-world OpenAPI specs generated from C# / Java / TypeScript frameworks often contain
// redundant service name prefixes (e.g. "UserService_GetUser"), internal interface prefixes ("iGetUsers"),
// or trailing HTTP verbs ("get_users_GET"). We normalize these to concise Go method names.
func SanitizeMethodName(rawOpID, httpMethod, rawPath, serviceName string) string {
	cleaned := strings.TrimSpace(rawOpID)
	if cleaned == "" {
		return DeriveMethodNameFromRoute(httpMethod, rawPath)
	}

	if reUglyVerbPrefix.MatchString(cleaned) && len(cleaned) > 5 {
		cleaned = reUglyVerbPrefix.ReplaceAllString(cleaned, "Get")
	}

	if serviceName != "" {
		trimmedSvc := strings.TrimSuffix(strings.TrimSuffix(serviceName, "API"), "Service")
		cleaned = strings.TrimPrefix(cleaned, serviceName+"_")
		cleaned = strings.TrimPrefix(cleaned, serviceName)
		if trimmedSvc != "" {
			cleaned = strings.TrimPrefix(cleaned, trimmedSvc+"_")
			cleaned = strings.TrimPrefix(cleaned, trimmedSvc)
		}
	}

	cleaned = reVersionSuffix.ReplaceAllString(cleaned, "")
	cleaned = reTrailingVerb.ReplaceAllString(cleaned, "")

	name := toPascalCase(cleaned)
	if len(name) > 0 {
		return name
	}

	return DeriveMethodNameFromRoute(httpMethod, rawPath)
}

// DeriveMethodNameFromRoute generates an idiomatic method name from HTTP verb and route path.
//
// References:
//   - RFC 9110 §HTTP Method Definitions: https://datatracker.ietf.org/doc/html/rfc9110#name-method-definitions
//   - RFC 6570 §URI Template: https://datatracker.ietf.org/doc/html/rfc6570
func DeriveMethodNameFromRoute(httpMethod, rawPath string) string {
	cleanPath := strings.Trim(rawPath, "/")
	parts := strings.Split(cleanPath, "/")

	var significant []string
	for _, p := range parts {
		if p == "" || p == "api" || reVersionPath.MatchString(p) {
			continue
		}

		if strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}") {
			varName := strings.Trim(p, "{}")
			significant = append(significant, "By", varName)
			continue
		}

		significant = append(significant, p)
	}

	verb := strings.ToUpper(httpMethod)
	prefix := mapHTTPVerbPrefix(verb)

	if len(significant) == 0 {
		return prefix + "Root"
	}

	joined := strings.Join(significant, "_")
	pascal := toPascalCase(joined)

	if slices.ContainsFunc(commonVerbs, func(v string) bool { return strings.HasPrefix(pascal, v) }) {
		return pascal
	}

	return prefix + pascal
}

func mapHTTPVerbPrefix(verb string) string {
	switch verb {
	case "POST":
		return "Create"
	case "PUT":
		return "Update"
	case "PATCH":
		return "Patch"
	case "DELETE":
		return "Delete"
	case "HEAD":
		return "Head"
	case "OPTIONS":
		return "Options"
	default:
		return "Get"
	}
}

func buildMethodName(pathStr, httpMethod string, op *Operation, used map[string]int) string {
	opID := ""
	if op != nil {
		opID = op.OperationID
	}
	base := SanitizeMethodName(opID, httpMethod, pathStr, "")
	if base == "" || isHexHash(base) {
		base = DeriveMethodNameFromRoute(httpMethod, pathStr)
	}

	if used == nil {
		return base
	}

	if count, ok := used[base]; ok {
		used[base] = count + 1
		return fmt.Sprintf("%s%d", base, count+1)
	}

	used[base] = 1
	return base
}

func toPascalCase(s string) string {
	parts := splitWords(s)
	for i, part := range parts {
		upper := strings.ToUpper(part)
		if initialisms[upper] {
			parts[i] = upper
		} else {
			parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
		}
	}

	return strings.Join(parts, "")
}

func toCamelCase(s string) string {
	pascal := toPascalCase(s)
	if pascal == "" {
		return ""
	}

	for init := range initialisms {
		if strings.HasPrefix(pascal, init) && len(pascal) > len(init) {
			return sanitizeKeywordName(strings.ToLower(init) + pascal[len(init):])
		}
	}

	return sanitizeKeywordName(strings.ToLower(pascal[:1]) + pascal[1:])
}

// sanitizeKeywordName prevents Go keyword collisions in generated parameter names.
//
// Quirk: When an OpenAPI spec declares a parameter named "type", "select", "range", or "map",
// emitting it directly in a Go parameter list (`type string`) causes a compilation error.
// We map them to canonical idiomatic abbreviations.
func sanitizeKeywordName(res string) string {
	if !goKeywords[res] {
		return res
	}

	switch res {
	case "type":
		return "typ"
	case "select":
		return "selected"
	case "range":
		return "rng"
	case "map":
		return "mapping"
	case "func":
		return "fn"
	case "var":
		return "variable"
	case "const":
		return "constant"
	case "interface":
		return "iface"
	case "package":
		return "pkg"
	case "import":
		return "imp"
	case "default":
		return "def"
	default:
		return res + "Param"
	}
}

func splitWords(s string) []string {
	var (
		words []string
		cur   strings.Builder
	)

	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '_' || ch == '-' || ch == '.' || ch == '/' || ch == ' ' || ch == ':' {
			if cur.Len() > 0 {
				words = append(words, cur.String())
				cur.Reset()
			}
			continue
		}

		if i > 0 && ch >= 'A' && ch <= 'Z' {
			prev := s[i-1]
			if prev >= 'a' && prev <= 'z' {
				if cur.Len() > 0 {
					words = append(words, cur.String())
					cur.Reset()
				}
			}
		}

		cur.WriteByte(ch)
	}

	if cur.Len() > 0 {
		words = append(words, cur.String())
	}

	return words
}

func toSnakeCase(s string) string {
	words := generic.Map(splitWords(s), strings.ToLower)
	return strings.Join(words, "_")
}
