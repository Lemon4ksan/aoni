// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package openapi

import (
	"bytes"
	"fmt"
	"go/format"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/lemon4ksan/foundation/generic"
)

// GenerateContract translates an OpenAPI document into a clean, declarative aoni Go contract.
func GenerateContract(spec *Document, cfg ImportConfig) ([]byte, error) {
	pkgName := cfg.PackageName
	if pkgName == "" {
		pkgName = "api"
	}

	var bodyBuf bytes.Buffer

	// 1. Base URL
	baseURL := resolveBaseURL(spec, cfg)
	if baseURL != "" {
		constName := "BaseURL"
		if cfg.ServiceName != "" && cfg.ServiceName != "API" {
			constName = cfg.ServiceName + "BaseURL"
		}

		fmt.Fprintf(
			&bodyBuf,
			"// %s is the default API base endpoint.\nconst %s = %q\n\n",
			constName,
			constName,
			baseURL,
		)
	}

	// 2. Generate Service Interface (API) at the TOP
	if len(spec.Paths) > 0 {
		if err := writeServiceInterface(&bodyBuf, spec, cfg, baseURL); err != nil {
			return nil, err
		}
	}

	// 3. Generate Schemas / Models (DTOs) BELOW the interface
	if spec.Components != nil && len(spec.Components.Schemas) > 0 {
		writeSchemas(&bodyBuf, spec.Components.Schemas, cfg)
	}

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "package %s\n\n", pkgName)

	buf.WriteString("import (\n")
	buf.WriteString("\t\"context\"\n")

	if bytes.Contains(bodyBuf.Bytes(), []byte("time.Time")) {
		buf.WriteString("\t\"time\"\n")
	}

	var customImports []string

	for _, rawType := range cfg.TypeMap {
		if idx := strings.LastIndex(rawType, "/"); idx != -1 {
			dotIdx := strings.LastIndex(rawType, ".")
			if dotIdx > idx {
				pkgPath := rawType[:dotIdx]
				short := path.Base(pkgPath) + "." + rawType[dotIdx+1:]

				if bytes.Contains(bodyBuf.Bytes(), []byte(short)) {
					if !slices.Contains(customImports, pkgPath) {
						customImports = append(customImports, pkgPath)
					}
				}
			}
		}
	}

	slices.Sort(customImports)

	for _, imp := range customImports {
		fmt.Fprintf(&buf, "\t%q\n", imp)
	}

	buf.WriteString("\n\t\"github.com/lemon4ksan/aoni\"\n")
	buf.WriteString(")\n\n")

	buf.Write(bodyBuf.Bytes())

	// Format output with standard go/format
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return buf.Bytes(), fmt.Errorf("failed to format generated Go source: %w\nSource:\n%s", err, buf.String())
	}

	return formatted, nil
}

// GenerateSplitContract generates separate api.go (interface) and models.go (DTOs) files.
func GenerateSplitContract(spec *Document, cfg ImportConfig) (apiSource, modelsSource []byte, err error) {
	pkgName := cfg.PackageName
	if pkgName == "" {
		pkgName = "api"
	}

	// --- 1. API Contract (api.go) ---
	var apiBody bytes.Buffer

	baseURL := resolveBaseURL(spec, cfg)
	if baseURL != "" {
		constName := "BaseURL"
		if cfg.ServiceName != "" && cfg.ServiceName != "API" {
			constName = cfg.ServiceName + "BaseURL"
		}

		fmt.Fprintf(
			&apiBody,
			"// %s is the default API base endpoint.\nconst %s = %q\n\n",
			constName,
			constName,
			baseURL,
		)
	}

	if len(spec.Paths) > 0 {
		if err := writeServiceInterface(&apiBody, spec, cfg, baseURL); err != nil {
			return nil, nil, err
		}
	}

	var apiBuf bytes.Buffer
	fmt.Fprintf(&apiBuf, "package %s\n\n", pkgName)
	apiBuf.WriteString("import (\n")
	apiBuf.WriteString("\t\"context\"\n")

	if bytes.Contains(apiBody.Bytes(), []byte("time.Time")) {
		apiBuf.WriteString("\t\"time\"\n")
	}

	var customImports []string
	for _, rawType := range cfg.TypeMap {
		if idx := strings.LastIndex(rawType, "/"); idx != -1 {
			dotIdx := strings.LastIndex(rawType, ".")
			if dotIdx > idx {
				pkgPath := rawType[:dotIdx]

				short := path.Base(pkgPath) + "." + rawType[dotIdx+1:]
				if bytes.Contains(apiBody.Bytes(), []byte(short)) {
					if !slices.Contains(customImports, pkgPath) {
						customImports = append(customImports, pkgPath)
					}
				}
			}
		}
	}

	slices.Sort(customImports)

	for _, imp := range customImports {
		fmt.Fprintf(&apiBuf, "\t%q\n", imp)
	}

	apiBuf.WriteString("\n\t\"github.com/lemon4ksan/aoni\"\n")
	apiBuf.WriteString(")\n\n")
	apiBuf.Write(apiBody.Bytes())

	apiSource, err = format.Source(apiBuf.Bytes())
	if err != nil {
		return nil, nil, fmt.Errorf("failed formatting api.go: %w\nSource:\n%s", err, apiBuf.String())
	}

	// --- 2. Models (models.go) ---
	if spec.Components != nil && len(spec.Components.Schemas) > 0 {
		var modelsBody bytes.Buffer
		writeSchemas(&modelsBody, spec.Components.Schemas, cfg)

		var modelsBuf bytes.Buffer
		fmt.Fprintf(&modelsBuf, "package %s\n\n", pkgName)

		hasTime := bytes.Contains(modelsBody.Bytes(), []byte("time.Time"))

		var modelCustomImports []string
		for _, rawType := range cfg.TypeMap {
			if idx := strings.LastIndex(rawType, "/"); idx != -1 {
				dotIdx := strings.LastIndex(rawType, ".")
				if dotIdx > idx {
					pkgPath := rawType[:dotIdx]

					short := path.Base(pkgPath) + "." + rawType[dotIdx+1:]
					if bytes.Contains(modelsBody.Bytes(), []byte(short)) {
						if !slices.Contains(modelCustomImports, pkgPath) {
							modelCustomImports = append(modelCustomImports, pkgPath)
						}
					}
				}
			}
		}

		slices.Sort(modelCustomImports)

		if hasTime || len(modelCustomImports) > 0 {
			modelsBuf.WriteString("import (\n")

			if hasTime {
				modelsBuf.WriteString("\t\"time\"\n")
			}

			for _, imp := range modelCustomImports {
				fmt.Fprintf(&modelsBuf, "\t%q\n", imp)
			}

			modelsBuf.WriteString(")\n\n")
		}

		modelsBuf.Write(modelsBody.Bytes())

		modelsSource, err = format.Source(modelsBuf.Bytes())
		if err != nil {
			return nil, nil, fmt.Errorf("failed formatting models.go: %w\nSource:\n%s", err, modelsBuf.String())
		}
	}

	return apiSource, modelsSource, nil
}

func resolveBaseURL(spec *Document, cfg ImportConfig) string {
	if cfg.BaseURL != "" {
		return cfg.BaseURL
	}

	if len(spec.Servers) > 0 && spec.Servers[0].URL != "" {
		raw := spec.Servers[0].URL
		if strings.HasPrefix(raw, "/") {
			return raw
		}

		if u, err := url.Parse(raw); err == nil && u.Scheme != "" {
			return raw
		}

		return raw
	}

	return ""
}

func writeServiceInterface(buf *bytes.Buffer, spec *Document, cfg ImportConfig, baseURL string) error {
	serviceName := cfg.ServiceName
	if serviceName == "" {
		serviceName = "API"
	}

	casing := "snake_case"
	if spec.Info != nil && spec.Info.Extensions != nil {
		if ext, ok := spec.Info.Extensions["x-vortex-casing"]; ok {
			if cStr, ok := ext.(string); ok && cStr != "" {
				casing = cStr
			}
		}
	}

	fmt.Fprintf(buf, "// %s provides the API client contract.\n//\n", serviceName)
	fmt.Fprintf(buf, "// @aoni:service casing=%s\n", casing)

	if spec.Info != nil {
		if spec.Info.Version != "" {
			fmt.Fprintf(buf, "// @version %q\n", spec.Info.Version)
		}
	}

	if cfg.SpecFile != "" {
		fmt.Fprintf(buf, "// @source %q\n", cfg.SpecFile)
	}

	if spec.Info != nil && spec.Info.Extensions != nil {
		if ext, ok := spec.Info.Extensions["x-vortex-persona"]; ok {
			if pStr, ok := ext.(string); ok && pStr != "" {
				fmt.Fprintf(buf, "// @persona %q\n", pStr)
			}
		}

		if ext, ok := spec.Info.Extensions["x-vortex-tlsspec"]; ok {
			if tStr, ok := ext.(string); ok && tStr != "" {
				fmt.Fprintf(buf, "// @tls_spec %q\n", tStr)
			}
		}

		if ext, ok := spec.Info.Extensions["x-vortex-engine"]; ok {
			if eStr, ok := ext.(string); ok && eStr != "" {
				fmt.Fprintf(buf, "// @engine %s\n", eStr)
			}
		}

		if headersRaw, ok := spec.Info.Extensions["x-vortex-headers"]; ok {
			if hList, ok := headersRaw.([]map[string]string); ok {
				for _, h := range hList {
					if h["name"] != "" && h["value"] != "" {
						fmt.Fprintf(buf, "// @header %q %q\n", h["name"], h["value"])
					}
				}
			} else if hListAny, ok := headersRaw.([]any); ok {
				for _, item := range hListAny {
					if hMap, ok := item.(map[string]any); ok {
						name, _ := hMap["name"].(string)

						val, _ := hMap["value"].(string)
						if name != "" && val != "" {
							fmt.Fprintf(buf, "// @header %q %q\n", name, val)
						}
					}
				}
			}
		}
	}

	if baseURL != "" {
		fmt.Fprintf(buf, "// @base_url %q\n", baseURL)
	}

	fmt.Fprintf(buf, "type %s interface {\n", serviceName)

	pathKeys := generic.Keys(spec.Paths)
	slices.Sort(pathKeys)

	usedMethodNames := make(map[string]int)

	for _, pathStr := range pathKeys {
		pathItem := spec.Paths[pathStr]
		if pathItem == nil {
			continue
		}

		if !isPathAllowed(pathStr, cfg) {
			continue
		}

		ops := pathItem.OperationsMap()
		methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}
		for _, httpMethod := range methods {
			op := ops[httpMethod]
			if op == nil {
				continue
			}

			if cfg.SkipDeprecated && op.Deprecated {
				continue
			}

			writeOperationMethod(buf, spec, pathStr, httpMethod, pathItem, op, cfg, usedMethodNames)
		}
	}

	fmt.Fprintf(buf, "}\n")

	return nil
}

func isPathAllowed(pathStr string, cfg ImportConfig) bool {
	if len(cfg.IncludePaths) > 0 {
		matched := false
		for _, pattern := range cfg.IncludePaths {
			if re, err := regexp.Compile(pattern); err == nil && re.MatchString(pathStr) {
				matched = true
				break
			}
		}

		if !matched {
			return false
		}
	}

	if len(cfg.ExcludePaths) > 0 {
		for _, pattern := range cfg.ExcludePaths {
			if re, err := regexp.Compile(pattern); err == nil && re.MatchString(pathStr) {
				return false
			}
		}
	}

	return true
}

func writeOperationMethod(
	buf *bytes.Buffer,
	spec *Document,
	pathStr string,
	httpMethod string,
	pathItem *PathItem,
	op *Operation,
	cfg ImportConfig,
	usedNames map[string]int,
) {
	methodName := buildMethodName(pathStr, httpMethod, op, usedNames)

	// Documentation
	summary := op.Summary
	if summary == "" {
		summary = op.Description
	}

	if summary != "" {
		fmt.Fprintf(buf, "\t// %s — %s\n\t//\n", methodName, strings.ReplaceAll(summary, "\n", " "))
	}

	if op.Extensions != nil {
		if credsRaw, ok := op.Extensions["x-required-credentials"]; ok {
			var credsList []string
			if listStr, ok := credsRaw.([]string); ok {
				credsList = listStr
			} else if listAny, ok := credsRaw.([]any); ok {
				for _, item := range listAny {
					if s, ok := item.(string); ok {
						credsList = append(credsList, s)
					}
				}
			}

			if len(credsList) > 0 {
				fmt.Fprintf(buf, "\t// Security & Session Requirements (captured from traffic):\n")

				for _, cred := range credsList {
					fmt.Fprintf(buf, "\t//   - %s\n", cred)
				}

				fmt.Fprintf(buf, "\t//\n")
			}
		}
	}

	// Route directive: // @get "path", // @post "path"
	cleanPath := strings.TrimPrefix(pathStr, "/")
	fmt.Fprintf(buf, "\t// @%s %q\n", strings.ToLower(httpMethod), cleanPath)

	if op.OperationID != "" && op.OperationID != methodName {
		fmt.Fprintf(buf, "\t// @bind %q\n", op.OperationID)
	}

	if op.Deprecated {
		fmt.Fprintf(buf, "\t// @deprecated\n")
	}

	if op.Extensions != nil {
		if unwrap, ok := op.Extensions["x-vortex-unwrap"]; ok {
			if uStr, ok := unwrap.(string); ok && uStr != "" {
				fmt.Fprintf(buf, "\t// @unwrap %q\n", uStr)
			}
		}

		if callFn, ok := op.Extensions["x-vortex-call"]; ok {
			if cStr, ok := callFn.(string); ok && cStr != "" {
				fmt.Fprintf(buf, "\t// @call %q\n", cStr)
			}
		}

		if idem, ok := op.Extensions["x-vortex-idempotent"]; ok {
			if isIdem, ok := idem.(bool); ok && isIdem {
				fmt.Fprintf(buf, "\t// @idempotent\n")
			}
		}

		if coal, ok := op.Extensions["x-vortex-coalesce"]; ok {
			if isCoal, ok := coal.(bool); ok && isCoal {
				fmt.Fprintf(buf, "\t// @coalesce\n")
			}
		}

		if etag, ok := op.Extensions["x-vortex-etag"]; ok {
			if isETag, ok := etag.(bool); ok && isETag {
				fmt.Fprintf(buf, "\t// @etag\n")
			}
		}

		sinceVal := ""
		if op.Extensions != nil {
			if since, ok := op.Extensions["x-vortex-since"]; ok {
				if sStr, ok := since.(string); ok && sStr != "" {
					sinceVal = sStr
				}
			}
		}
		if sinceVal == "" && spec != nil && spec.Info != nil && spec.Info.Version != "" {
			sinceVal = spec.Info.Version
		}

		if sinceVal != "" {
			fmt.Fprintf(buf, "\t// @since %q\n", sinceVal)
		}

		if headersRaw, ok := op.Extensions["x-vortex-headers"]; ok {
			seenMethodHeaders := make(map[string]bool)
			if hList, ok := headersRaw.([]map[string]string); ok {
				for _, h := range hList {
					if h["name"] != "" && h["value"] != "" && !isGlobalHeader(spec, h["name"], h["value"]) {
						headerKey := strings.ToLower(h["name"])
						if !seenMethodHeaders[headerKey] {
							seenMethodHeaders[headerKey] = true

							fmt.Fprintf(buf, "\t// @header %q %q\n", h["name"], h["value"])
						}
					}
				}
			} else if hListAny, ok := headersRaw.([]any); ok {
				for _, item := range hListAny {
					if hMap, ok := item.(map[string]any); ok {
						name, _ := hMap["name"].(string)

						val, _ := hMap["value"].(string)
						if name != "" && val != "" && !isGlobalHeader(spec, name, val) {
							headerKey := strings.ToLower(name)
							if !seenMethodHeaders[headerKey] {
								seenMethodHeaders[headerKey] = true

								fmt.Fprintf(buf, "\t// @header %q %q\n", name, val)
							}
						}
					}
				}
			}
		}
	}

	// Payload directive
	isForm := false
	if op.RequestBody != nil {
		content := op.RequestBody.Content
		if content["application/x-www-form-urlencoded"] != nil || content["multipart/form-data"] != nil {
			isForm = true

			fmt.Fprintf(buf, "\t// @form casing=snake_case\n")
		}
	}

	// Collect parameters
	params := extractOperationParameters(pathStr, pathItem, op)

	// Emit @query casing=snake_case for non-GET methods with query parameters
	if httpMethod != "GET" && !isForm && len(params.query) > 0 {
		fmt.Fprintf(buf, "\t// @query casing=snake_case\n")
	}

	var paramSig []string

	paramSig = append(paramSig, "ctx context.Context")

	for _, p := range params.path {
		pName := toCamelCase(p.Name)

		pType := "string"
		if cfg.TypeMap != nil {
			if mapped, ok := cfg.TypeMap[p.Name]; ok {
				pType = shortTypeName(mapped)
			} else if mapped, ok := cfg.TypeMap[strings.ToLower(p.Name)]; ok {
				pType = shortTypeName(mapped)
			}
		}

		if pType == "string" && p.Schema != nil {
			pType = mapSchemaType(p.Schema, cfg)
		}

		paramSig = append(paramSig, fmt.Sprintf("%s %s", pName, pType))
	}

	for _, p := range params.query {
		pName := toCamelCase(p.Name)

		pType := "string"
		if cfg.TypeMap != nil {
			if mapped, ok := cfg.TypeMap[p.Name]; ok {
				pType = shortTypeName(mapped)
			} else if mapped, ok := cfg.TypeMap[strings.ToLower(p.Name)]; ok {
				pType = shortTypeName(mapped)
			}
		}

		if pType == "string" && p.Schema != nil {
			pType = mapSchemaType(p.Schema, cfg)
		}

		sig := fmt.Sprintf("%s %s", pName, pType)

		expectedSnake := toSnakeCase(pName)
		if p.Name != "" && (httpMethod != "GET" || (p.Name != expectedSnake && p.Name != pName)) {
			sig += fmt.Sprintf(" // @query %q", p.Name)
		}

		paramSig = append(paramSig, sig)
	}

	// Request Body parameter if JSON
	if !isForm && op.RequestBody != nil {
		jsonContent := op.RequestBody.Content["application/json"]
		if jsonContent != nil && jsonContent.Schema != nil {
			bodyType := "any"
			if jsonContent.Schema.Ref != "" {
				bodyType = toPascalCase(path.Base(jsonContent.Schema.Ref))
			} else {
				bodyType = mapSchemaType(jsonContent.Schema, cfg)
			}

			paramSig = append(paramSig, "req "+bodyType)
		}
	}

	paramSig = append(paramSig, "mods ...aoni.RequestModifier")

	// Determine return type
	returnType := determineReturnType(op, cfg)

	hasComments := false
	for _, p := range paramSig {
		if strings.Contains(p, "//") {
			hasComments = true
			break
		}
	}

	if len(paramSig) > 4 || hasComments {
		if returnType == "" {
			fmt.Fprintf(buf, "\t%s(\n", methodName)

			for _, p := range paramSig {
				if idx := strings.Index(p, "//"); idx != -1 {
					codePart := strings.TrimSpace(p[:idx])
					commentPart := p[idx:]
					fmt.Fprintf(buf, "\t\t%s, %s\n", codePart, commentPart)
				} else {
					fmt.Fprintf(buf, "\t\t%s,\n", p)
				}
			}

			fmt.Fprintf(buf, "\t) error\n\n")
		} else {
			fmt.Fprintf(buf, "\t%s(\n", methodName)

			for _, p := range paramSig {
				if idx := strings.Index(p, "//"); idx != -1 {
					codePart := strings.TrimSpace(p[:idx])
					commentPart := p[idx:]
					fmt.Fprintf(buf, "\t\t%s, %s\n", codePart, commentPart)
				} else {
					fmt.Fprintf(buf, "\t\t%s,\n", p)
				}
			}

			fmt.Fprintf(buf, "\t) (%s, error)\n\n", returnType)
		}
	} else {
		if returnType == "" {
			fmt.Fprintf(buf, "\t%s(%s) error\n\n", methodName, strings.Join(paramSig, ", "))
		} else {
			fmt.Fprintf(buf, "\t%s(%s) (%s, error)\n\n", methodName, strings.Join(paramSig, ", "), returnType)
		}
	}
}

func determineReturnType(op *Operation, cfg ImportConfig) string {
	if op.Responses == nil {
		return "map[string]any"
	}

	resp := op.Responses["200"]
	if resp == nil {
		resp = op.Responses["201"]
	}
	if resp == nil {
		resp = op.Responses["default"]
	}

	if resp == nil {
		return ""
	}

	if resp.Content == nil {
		return "map[string]any"
	}

	jsonContent := resp.Content["application/json"]
	if jsonContent == nil || jsonContent.Schema == nil {
		return "map[string]any"
	}

	if jsonContent.Schema.Ref != "" {
		typeName := toPascalCase(path.Base(jsonContent.Schema.Ref))
		return "*" + typeName
	}

	s := jsonContent.Schema
	if s.IsType("array") {
		if s.Items != nil && s.Items.Ref != "" {
			return "[]*" + toPascalCase(path.Base(s.Items.Ref))
		}

		return "[]" + mapSchemaType(s.Items, cfg)
	}

	if s.IsType("object") && len(s.Properties) == 0 {
		return "map[string]any"
	}

	return mapSchemaType(s, cfg)
}

func isGlobalHeader(spec *Document, name, val string) bool {
	if spec == nil || spec.Info == nil || spec.Info.Extensions == nil {
		return false
	}

	headersRaw, ok := spec.Info.Extensions["x-vortex-headers"]
	if !ok {
		return false
	}

	if hList, ok := headersRaw.([]map[string]string); ok {
		for _, h := range hList {
			if strings.EqualFold(h["name"], name) && h["value"] == val {
				return true
			}
		}
	} else if hListAny, ok := headersRaw.([]any); ok {
		for _, item := range hListAny {
			if hMap, ok := item.(map[string]any); ok {
				n, _ := hMap["name"].(string)
				v, _ := hMap["value"].(string)

				if strings.EqualFold(n, name) && v == val {
					return true
				}
			}
		}
	}

	return false
}

type operationParameters struct {
	path   []*Parameter
	query  []*Parameter
	header []*Parameter
}

func extractOperationParameters(
	pathStr string,
	pathItem *PathItem,
	op *Operation,
) operationParameters {
	var res operationParameters

	var combined []*Parameter
	if pathItem != nil {
		combined = append(combined, pathItem.Parameters...)
	}
	if op != nil {
		combined = append(combined, op.Parameters...)
	}

	seen := make(map[string]bool)
	for _, p := range combined {
		if p == nil {
			continue
		}

		key := p.In + ":" + p.Name
		if seen[key] {
			continue
		}

		seen[key] = true

		switch p.In {
		case "path":
			res.path = append(res.path, p)
		case "query":
			res.query = append(res.query, p)
		case "header":
			res.header = append(res.header, p)
		}
	}

	// Ensure all {var} path segments from pathStr are represented in res.path
	rem := pathStr
	for {
		start := strings.Index(rem, "{")
		if start == -1 {
			break
		}

		end := strings.Index(rem[start:], "}")
		if end == -1 {
			break
		}

		varName := rem[start+1 : start+end]
		rem = rem[start+end+1:]

		key := "path:" + varName
		if !seen[key] && !seen["path:"+strings.ToLower(varName)] {
			seen[key] = true

			res.path = append(res.path, &Parameter{
				Name:     varName,
				In:       "path",
				Required: true,
				Schema:   &Schema{Type: TypeArray{"string"}},
			})
		}
	}

	return res
}
