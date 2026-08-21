// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package openapi

import (
	"fmt"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ingest"
)

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

var goKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
}

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

func buildMethodName(pathStr, httpMethod string, op *openapi3.Operation, used map[string]int) string {
	base := ingest.SanitizeMethodName(op.OperationID, httpMethod, pathStr, "")
	if base == "" || isHexHash(base) {
		base = ingest.DeriveMethodNameFromRoute(httpMethod, pathStr)
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

	var res string
	for init := range initialisms {
		if strings.HasPrefix(pascal, init) && len(pascal) > len(init) {
			res = strings.ToLower(init) + pascal[len(init):]
			break
		}
	}

	if res == "" {
		res = strings.ToLower(pascal[:1]) + pascal[1:]
	}

	if goKeywords[res] {
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

	return res
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
	words := splitWords(s)
	for i, w := range words {
		words[i] = strings.ToLower(w)
	}

	return strings.Join(words, "_")
}
