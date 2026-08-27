// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package emitter

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
)

// formatType converts GoTypeIR into a clean Go type representation.
func formatType(t ir.GoTypeIR) string {
	if t.Name != "" {
		return t.Name
	}

	return "any"
}

// formatMethodParams builds the Go parameter list for a method signature.
func formatMethodParams(params []*ir.ParamIR) string {
	parts := make([]string, 0, len(params))
	for _, p := range params {
		tName := formatType(p.GoType)
		parts = append(parts, fmt.Sprintf("%s %s", p.GoName, tName))
	}

	return strings.Join(parts, ", ")
}

// formatMethodReturns builds the return signature of a method.
func formatMethodReturns(ret *ir.ReturnIR) string {
	if ret == nil || ret.IsVoid {
		return "error"
	}

	tName := formatType(ret.SuccessType)

	if strings.HasPrefix(tName, "func(") {
		return tName
	}

	if ret.HasRawResponse {
		return fmt.Sprintf("(%s, *http.Response, error)", tName)
	}

	if ret.IsStreamChan {
		elem := strings.TrimPrefix(tName, "<-chan ")
		return fmt.Sprintf("(<-chan %s, <-chan error, error)", elem)
	}

	return fmt.Sprintf("(%s, error)", tName)
}

func lowerFirst(s string) string {
	if s == "" {
		return ""
	}

	runes := []rune(s)

	allUpper := true
	for _, r := range runes {
		if !unicode.IsUpper(r) {
			allUpper = false
			break
		}
	}

	if allUpper {
		return strings.ToLower(s)
	}

	i := 0
	for i < len(runes) && unicode.IsUpper(runes[i]) {
		i++
	}

	if i > 1 {
		if i == len(runes) {
			return strings.ToLower(s)
		}

		return strings.ToLower(string(runes[:i-1])) + string(runes[i-1:])
	}

	return strings.ToLower(string(runes[:1])) + string(runes[1:])
}

// toPascalCase converts snake_case or lowercase identifier to PascalCase (e.g. "purchase_eresult" -> "PurchaseEResult", "success" -> "Success").
func toPascalCase(s string) string {
	if s == "" {
		return ""
	}

	parts := strings.Split(s, "_")

	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}

		switch strings.ToLower(p) {
		case "id":
			b.WriteString("ID")
		case "url":
			b.WriteString("URL")
		case "api":
			b.WriteString("API")
		case "sku":
			b.WriteString("SKU")
		case "eresult":
			b.WriteString("EResult")
		default:
			b.WriteString(strings.ToUpper(p[:1]))
			b.WriteString(p[1:])
		}
	}

	return b.String()
}

func cleanIdentifier(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(unicode.ToLower(r))
		}
	}

	return b.String()
}
