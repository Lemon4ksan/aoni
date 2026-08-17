// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package jsbundle

import (
	"bytes"
	"fmt"
	"go/format"
	"strings"
	"unicode"
)

// ContractOptions configures the generation of a Go @aoni:service contract from JS scan results.
type ContractOptions struct {
	PackageName string
	ServiceName string
	SourceSpec  string
	BaseURL     string
	Engine      string
}

// GenerateContract compiles discovered endpoints and message schemas into formatted Go source code.
func GenerateContract(scan *ScanResult, opts ContractOptions) ([]byte, error) {
	if scan == nil {
		return nil, fmt.Errorf("scan result is nil")
	}

	pkgName := opts.PackageName
	if pkgName == "" {
		pkgName = "api"
	}

	svcName := opts.ServiceName
	if svcName == "" {
		svcName = "API"
	}

	baseURL := opts.BaseURL
	if baseURL == "" {
		baseURL = "https://api.example.com"
	}

	engine := opts.Engine
	if engine == "" {
		engine = "fast"
	}

	var b bytes.Buffer

	// Package header & imports
	b.WriteString(fmt.Sprintf("package %s\n\n", pkgName))
	b.WriteString("import (\n\t\"context\"\n\n\t\"github.com/lemon4ksan/aoni\"\n)\n\n")

	// Service base URL constant
	b.WriteString(fmt.Sprintf("// %sBaseURL is the default API base endpoint.\n", svcName))
	b.WriteString(fmt.Sprintf("const %sBaseURL = %q\n\n", svcName, baseURL))

	// Service Interface Doc & Directives
	b.WriteString(fmt.Sprintf("// %s provides client operations for the service.\n//\n", svcName))
	b.WriteString("// @aoni:service casing=snake_case\n")
	b.WriteString("// @version \"1.0.0\"\n")
	if opts.SourceSpec != "" {
		b.WriteString(fmt.Sprintf("// @source %q\n", opts.SourceSpec))
	}
	b.WriteString(fmt.Sprintf("// @engine %s\n", engine))
	b.WriteString("// @header \"accept\" \"*/*\"\n")
	b.WriteString(fmt.Sprintf("// @base_url %q\n", baseURL))
	b.WriteString(fmt.Sprintf("type %s interface {\n", svcName))

	seenMethods := make(map[string]bool)

	for _, ep := range scan.Endpoints {
		methodName := DeriveMethodName(ep.Path)
		if seenMethods[methodName] {
			continue
		}
		seenMethods[methodName] = true

		cleanPath := strings.TrimPrefix(ep.Path, "/")
		httpVerb := strings.ToLower(ep.HTTPMethod)
		if httpVerb == "" {
			httpVerb = "post"
		}

		b.WriteString(fmt.Sprintf("\t// %s — %s %s\n", methodName, ep.HTTPMethod, ep.Path))
		b.WriteString("\t//\n")
		b.WriteString(fmt.Sprintf("\t// @%s %q\n", httpVerb, cleanPath))

		// Check if we have a typed tuple return or request
		tupleName := methodName + "Tuple"
		if msg, ok := scan.Messages[methodName]; ok && len(msg.Fields) > 0 {
			b.WriteString(fmt.Sprintf("\t%s(ctx context.Context, req any, mods ...aoni.RequestModifier) (*%s, error)\n\n", methodName, tupleName))
		} else {
			b.WriteString(fmt.Sprintf("\t%s(ctx context.Context, req any, mods ...aoni.RequestModifier) (any, error)\n\n", methodName))
		}
	}

	b.WriteString("}\n\n")

	definedStructs := make(map[string]bool)
	for id, msg := range scan.Messages {
		if len(msg.Fields) == 0 {
			continue
		}
		structName := SanitizeIdentifier(id)
		if !strings.HasSuffix(structName, "Tuple") {
			structName += "Tuple"
		}
		definedStructs[structName] = true
	}

	referencedStructs := make(map[string]bool)

	// Emit @aoni:tuple declarations for discovered message descriptors
	for id, msg := range scan.Messages {
		if len(msg.Fields) == 0 {
			continue
		}
		structName := SanitizeIdentifier(id)
		if !strings.HasSuffix(structName, "Tuple") {
			structName += "Tuple"
		}

		b.WriteString("// @aoni:tuple\n")
		b.WriteString(fmt.Sprintf("type %s struct {\n", structName))
		for idx, f := range msg.Fields {
			fieldName := f.Name
			if fieldName == "" || strings.HasPrefix(fieldName, "Field") {
				fieldName = fmt.Sprintf("Field%d", idx)
			}
			goType := f.GoType
			if goType == "" {
				goType = "string"
			} else if f.IsNested && f.SubMsgType != "" {
				subName := SanitizeIdentifier(f.SubMsgType)
				if !strings.HasSuffix(subName, "Tuple") {
					subName += "Tuple"
				}
				goType = subName
				referencedStructs[subName] = true
			}
			b.WriteString(fmt.Sprintf("\t%s %s `aoni:\"%d\"`\n", SanitizeIdentifier(fieldName), goType, idx))
		}
		b.WriteString("}\n\n")
	}

	// Emit any referenced sub-message structs that had no explicit fields scanned
	for subName := range referencedStructs {
		if !definedStructs[subName] {
			b.WriteString("// @aoni:tuple\n")
			b.WriteString(fmt.Sprintf("type %s struct {\n\tField0 any `aoni:\"0\"`\n}\n\n", subName))
			definedStructs[subName] = true
		}
	}

	formatted, err := format.Source(b.Bytes())
	if err != nil {
		// Fallback to unformatted if syntax error
		return b.Bytes(), nil
	}

	return formatted, nil
}

// DeriveMethodName converts an RPC path or REST route into a clean, idiomatic Go method name.
func DeriveMethodName(path string) string {
	parts := strings.Split(path, "/")
	raw := parts[len(parts)-1]

	// Handle colons (e.g. v1internal:fetchUserInfo)
	if colonIdx := strings.Index(raw, ":"); colonIdx != -1 {
		raw = raw[colonIdx+1:]
	}

	// If method is inside a known sub-service, prefix with service name
	if len(parts) > 2 {
		svc := parts[len(parts)-2]
		for _, prefix := range []string{"Waa", "CloudSql", "AppletControl", "AppletImport", "Documentation", "OAuth"} {
			if strings.Contains(svc, prefix) && !strings.HasPrefix(raw, prefix) {
				raw = prefix + raw
				break
			}
		}
	}

	return SanitizeIdentifier(raw)
}

// SanitizeIdentifier ensures a string is a valid, exported Go identifier.
func SanitizeIdentifier(s string) string {
	if s == "" {
		return "Item"
	}

	var b strings.Builder
	capitalize := true

	for _, r := range s {
		if r == '_' || r == '-' || r == '.' || r == '$' {
			capitalize = true
			continue
		}

		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if capitalize {
				b.WriteRune(unicode.ToUpper(r))
				capitalize = false
			} else {
				b.WriteRune(r)
			}
		}
	}

	res := b.String()
	if res == "" || unicode.IsDigit(rune(res[0])) {
		res = "V" + res
	}

	return res
}
