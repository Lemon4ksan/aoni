// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package openapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"net/url"
	"os"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi2"
	"github.com/getkin/kin-openapi/openapi2conv"
	"github.com/getkin/kin-openapi/openapi3"
	"gopkg.in/yaml.v3"

	"github.com/lemon4ksan/aoni/internal/codegen/ingest"
)

// ImportConfig controls how OpenAPI specifications are imported into aoni declarative contracts.
type ImportConfig struct {
	SpecFile       string
	SpecData       []byte
	PackageName    string
	ServiceName    string
	OutputFile     string
	ModelsFile     string
	BaseURL        string
	SkipDeprecated bool
	SplitModels    bool
	IncludePaths   []string
	ExcludePaths   []string
	TypeMap        map[string]string
}

// ImportResult captures the outcome of an OpenAPI contract generation pass.
type ImportResult struct {
	ContractCode  []byte
	ModelsCode    []byte
	ServicesCount int
	MethodsCount  int
	StructsCount  int
}

// Import loads an OpenAPI specification and translates it into declarative aoni Go contracts.
func Import(cfg ImportConfig) (*ImportResult, error) {
	spec, err := LoadSpec(cfg.SpecFile, cfg.SpecData)
	if err != nil {
		return nil, fmt.Errorf("loading spec: %w", err)
	}

	code, err := GenerateContract(spec, cfg)
	if err != nil {
		return nil, fmt.Errorf("generating contract: %w", err)
	}

	res := &ImportResult{
		ContractCode:  code,
		ServicesCount: 1,
	}

	if spec.Paths != nil {
		res.MethodsCount = len(spec.Paths.Map())
	}
	if spec.Components != nil {
		res.StructsCount = len(spec.Components.Schemas)
	}

	return res, nil
}

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

// LoadSpec loads and normalizes an OpenAPI 2.0, 3.0, or 3.1 specification.
func LoadSpec(filename string, data []byte) (*openapi3.T, error) {
	if len(data) == 0 {
		var err error

		data, err = os.ReadFile(filename)
		if err != nil {
			return nil, fmt.Errorf("failed reading spec file %s: %w", filename, err)
		}
	}

	data = sanitizeSpecData(data)

	var versionDetector struct {
		Swagger string `json:"swagger" yaml:"swagger"`
		OpenAPI string `json:"openapi" yaml:"openapi"`
	}

	if err := yaml.Unmarshal(data, &versionDetector); err == nil {
		if strings.HasPrefix(versionDetector.Swagger, "2.") || versionDetector.Swagger == "2.0" {
			var doc2 openapi2.T
			if err := json.Unmarshal(data, &doc2); err != nil {
				if errYaml := yaml.Unmarshal(data, &doc2); errYaml != nil {
					return nil, fmt.Errorf("failed parsing Swagger 2.0 spec: %w", err)
				}
			}

			doc3, err := openapi2conv.ToV3(&doc2)
			if err != nil {
				return nil, fmt.Errorf("failed converting Swagger 2.0 to OpenAPI 3.0: %w", err)
			}

			return doc3, nil
		}
	}

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true

	doc3, err := loader.LoadFromData(data)
	if err != nil {
		return nil, fmt.Errorf("failed parsing OpenAPI 3.x spec: %w", err)
	}

	return doc3, nil
}

func sanitizeSpecData(data []byte) []byte {
	var rawNode any
	if err := yaml.Unmarshal(data, &rawNode); err != nil {
		return data
	}

	sanitizeMapNode(rawNode)

	cleaned, err := json.Marshal(rawNode)
	if err != nil {
		return data
	}

	return cleaned
}

func sanitizeMapNode(node any) {
	switch v := node.(type) {
	case map[string]any:
		for key, val := range v {
			if key == "type" {
				if arr, ok := val.([]any); ok && len(arr) > 0 {
					var nonNull []any
					for _, item := range arr {
						if s, ok := item.(string); ok && s != "null" {
							nonNull = append(nonNull, s)
						}
					}

					if len(nonNull) > 0 {
						v["type"] = nonNull[0]
					} else {
						v["type"] = "string"
					}
				}
			}

			if key == "$ref" {
				if strVal, ok := val.(string); ok {
					if strings.HasPrefix(strVal, "#") && !strings.HasPrefix(strVal, "#/") {
						v[key] = "#/" + strVal[1:]
					}
				}
			} else {
				switch key {
				case "nullable", "deprecated", "readOnly", "writeOnly", "exclusiveMinimum", "exclusiveMaximum":
					if strVal, ok := val.(string); ok {
						if strings.EqualFold(strVal, "true") {
							v[key] = true
						} else if strings.EqualFold(strVal, "false") {
							v[key] = false
						}
					}

				case "type":
					if strVal, ok := val.(string); ok {
						if strings.EqualFold(strVal, "string|number") || strings.EqualFold(strVal, "number|string") {
							v[key] = "string"
						}
					}
				}
			}

			sanitizeMapNode(val)
		}

	case map[any]any:
		for k, val := range v {
			keyStr := fmt.Sprintf("%v", k)
			if keyStr == "$ref" {
				if strVal, ok := val.(string); ok {
					if strings.HasPrefix(strVal, "#") && !strings.HasPrefix(strVal, "#/") {
						v[k] = "#/" + strVal[1:]
					}
				}
			} else {
				switch keyStr {
				case "nullable", "deprecated", "readOnly", "writeOnly", "exclusiveMinimum", "exclusiveMaximum":
					if strVal, ok := val.(string); ok {
						if strings.EqualFold(strVal, "true") {
							v[k] = true
						} else if strings.EqualFold(strVal, "false") {
							v[k] = false
						}
					}

				case "type":
					if strVal, ok := val.(string); ok {
						if strings.EqualFold(strVal, "string|number") || strings.EqualFold(strVal, "number|string") {
							v[k] = "string"
						}
					}
				}
			}

			sanitizeMapNode(val)
		}

	case []any:
		for _, item := range v {
			sanitizeMapNode(item)
		}
	}
}

// GenerateContract translates an OpenAPI document into a clean, declarative aoni Go contract.
func GenerateContract(spec *openapi3.T, cfg ImportConfig) ([]byte, error) {
	pkgName := cfg.PackageName
	if pkgName == "" {
		pkgName = "api"
	}

	var bodyBuf bytes.Buffer

	// 1. Base URL
	baseURL := resolveBaseURL(spec, cfg)
	if baseURL != "" {
		fmt.Fprintf(&bodyBuf, "// BaseURL is the default API base endpoint.\nconst BaseURL = %q\n\n", baseURL)
	}

	// 2. Generate Service Interface (API) at the TOP
	if spec.Paths != nil && len(spec.Paths.Map()) > 0 {
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

	sort.Strings(customImports)

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
func GenerateSplitContract(spec *openapi3.T, cfg ImportConfig) (apiSource, modelsSource []byte, err error) {
	pkgName := cfg.PackageName
	if pkgName == "" {
		pkgName = "api"
	}

	// --- 1. API Contract (api.go) ---
	var apiBody bytes.Buffer

	baseURL := resolveBaseURL(spec, cfg)
	if baseURL != "" {
		fmt.Fprintf(&apiBody, "// BaseURL is the default API base endpoint.\nconst BaseURL = %q\n\n", baseURL)
	}

	if spec.Paths != nil && len(spec.Paths.Map()) > 0 {
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

	sort.Strings(customImports)

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

		sort.Strings(modelCustomImports)

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

func resolveBaseURL(spec *openapi3.T, cfg ImportConfig) string {
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

func writeSchemas(buf *bytes.Buffer, schemas openapi3.Schemas, cfg ImportConfig) {
	keys := make([]string, 0, len(schemas))
	for k := range schemas {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	for _, k := range keys {
		ref := schemas[k]
		if ref == nil || ref.Value == nil {
			continue
		}

		writeSchemaModel(buf, k, ref.Value, cfg)
	}
}

func writeSchemaModel(buf *bytes.Buffer, rawName string, s *openapi3.Schema, cfg ImportConfig) {
	name := toPascalCase(rawName)

	if s.Description != "" {
		fmt.Fprintf(buf, "// %s — %s\n", name, strings.ReplaceAll(s.Description, "\n", " "))
	}

	if len(s.Enum) > 0 && (s.Type == nil || s.Type.Is("string")) {
		fmt.Fprintf(buf, "type %s string\n\n", name)
		fmt.Fprintf(buf, "const (\n")

		for _, enumVal := range s.Enum {
			valStr := fmt.Sprintf("%v", enumVal)
			constName := name + toPascalCase(valStr)
			fmt.Fprintf(buf, "\t%s %s = %q\n", constName, name, valStr)
		}

		fmt.Fprintf(buf, ")\n\n")

		return
	}

	if s.Type != nil && !s.Type.Is("object") && len(s.Properties) == 0 {
		goType := mapSchemaType(s, cfg)
		fmt.Fprintf(buf, "type %s %s\n\n", name, goType)
		return
	}

	// Struct DTO
	fmt.Fprintf(buf, "// @aoni:dto casing=snake_case omitempty=true\n")
	fmt.Fprintf(buf, "type %s struct {\n", name)

	propKeys := make([]string, 0, len(s.Properties))
	for pk := range s.Properties {
		propKeys = append(propKeys, pk)
	}

	sort.Strings(propKeys)

	requiredMap := make(map[string]bool, len(s.Required))
	for _, req := range s.Required {
		requiredMap[req] = true
	}

	for _, pk := range propKeys {
		propRef := s.Properties[pk]
		if propRef == nil || propRef.Value == nil {
			continue
		}

		propSchema := propRef.Value

		fieldName := toPascalCase(pk)
		if fieldName == "" {
			fieldName = "Field"
		}

		if fieldName == name {
			fieldName += "Val"
		}

		fieldType := mapSchemaType(propSchema, cfg)
		if propRef.Ref != "" {
			refName := toPascalCase(path.Base(propRef.Ref))
			if propSchema.Type != nil && propSchema.Type.Is("object") {
				fieldType = "*" + refName
			} else {
				fieldType = refName
			}
		}

		tag := fmt.Sprintf("`json:\"%s,omitempty\"`", pk)
		if requiredMap[pk] {
			tag = fmt.Sprintf("`json:\"%s\"`", pk)
		}

		if propSchema.Description != "" {
			fmt.Fprintf(buf, "\t// %s\n", strings.ReplaceAll(propSchema.Description, "\n", " "))
		}

		fmt.Fprintf(buf, "\t%s %s %s\n", fieldName, fieldType, tag)
	}

	fmt.Fprintf(buf, "}\n\n")
}

func shortTypeName(raw string) string {
	if idx := strings.LastIndex(raw, "/"); idx != -1 {
		dotIdx := strings.LastIndex(raw, ".")
		if dotIdx > idx {
			return path.Base(raw[:dotIdx]) + "." + raw[dotIdx+1:]
		}
	}

	return raw
}

func mapSchemaType(s *openapi3.Schema, cfg ImportConfig) string {
	if s == nil {
		return "any"
	}

	if cfg.TypeMap != nil && s.Title != "" {
		if mapped, ok := cfg.TypeMap[s.Title]; ok {
			return shortTypeName(mapped)
		}
	}

	if s.Type == nil {
		if len(s.Properties) > 0 {
			return "map[string]any"
		}

		return "any"
	}

	if s.Type.Is("string") {
		switch s.Format {
		case "date-time":
			return "time.Time"
		case "date":
			return "string"
		case "binary", "byte":
			return "[]byte"
		default:
			return "string"
		}
	}

	if s.Type.Is("integer") {
		switch s.Format {
		case "int64":
			return "int64"
		case "uint64":
			return "uint64"
		case "uint32", "uint":
			return "uint32"
		default:
			return "int"
		}
	}

	if s.Type.Is("number") {
		return "float64"
	}

	if s.Type.Is("boolean") {
		return "bool"
	}

	if s.Type.Is("array") {
		if s.Items != nil {
			if s.Items.Ref != "" {
				return "[]" + toPascalCase(path.Base(s.Items.Ref))
			}

			return "[]" + mapSchemaType(s.Items.Value, cfg)
		}

		return "[]any"
	}

	if s.Type.Is("object") {
		if s.AdditionalProperties.Schema != nil {
			valType := "any"
			if s.AdditionalProperties.Schema.Ref != "" {
				valType = toPascalCase(path.Base(s.AdditionalProperties.Schema.Ref))
			} else if s.AdditionalProperties.Schema.Value != nil {
				valType = mapSchemaType(s.AdditionalProperties.Schema.Value, cfg)
			}

			return "map[string]" + valType
		}

		return "map[string]any"
	}

	return "any"
}

func writeServiceInterface(buf *bytes.Buffer, spec *openapi3.T, cfg ImportConfig, baseURL string) error {
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
	}

	if baseURL != "" {
		fmt.Fprintf(buf, "// @base_url %q\n", baseURL)
	}

	fmt.Fprintf(buf, "type %s interface {\n", serviceName)

	pathKeys := make([]string, 0, len(spec.Paths.Map()))
	for p := range spec.Paths.Map() {
		pathKeys = append(pathKeys, p)
	}

	sort.Strings(pathKeys)

	usedMethodNames := make(map[string]int)

	for _, pathStr := range pathKeys {
		pathItem := spec.Paths.Find(pathStr)
		if pathItem == nil {
			continue
		}

		if !isPathAllowed(pathStr, cfg) {
			continue
		}

		ops := map[string]*openapi3.Operation{
			"GET":     pathItem.Get,
			"POST":    pathItem.Post,
			"PUT":     pathItem.Put,
			"DELETE":  pathItem.Delete,
			"PATCH":   pathItem.Patch,
			"HEAD":    pathItem.Head,
			"OPTIONS": pathItem.Options,
		}

		methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}
		for _, httpMethod := range methods {
			op := ops[httpMethod]
			if op == nil {
				continue
			}

			if cfg.SkipDeprecated && op.Deprecated {
				continue
			}

			writeOperationMethod(buf, pathStr, httpMethod, pathItem, op, cfg, usedMethodNames)
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
	pathStr string,
	httpMethod string,
	pathItem *openapi3.PathItem,
	op *openapi3.Operation,
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

		if since, ok := op.Extensions["x-vortex-since"]; ok {
			if sStr, ok := since.(string); ok && sStr != "" {
				fmt.Fprintf(buf, "\t// @since %q\n", sStr)
			}
		}
	}

	// Payload directive
	isForm := false
	if op.RequestBody != nil && op.RequestBody.Value != nil {
		content := op.RequestBody.Value.Content
		if content.Get("application/x-www-form-urlencoded") != nil || content.Get("multipart/form-data") != nil {
			isForm = true

			fmt.Fprintf(buf, "\t// @form casing=snake_case\n")
		} else if content.Get("application/json") != nil {
			fmt.Fprintf(buf, "\t// @json\n")
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

		if pType == "string" && p.Schema != nil && p.Schema.Value != nil {
			pType = mapSchemaType(p.Schema.Value, cfg)
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

		if pType == "string" && p.Schema != nil && p.Schema.Value != nil {
			pType = mapSchemaType(p.Schema.Value, cfg)
		}

		sig := fmt.Sprintf("%s %s", pName, pType)

		expectedSnake := toSnakeCase(pName)
		if p.Name != "" && p.Name != expectedSnake && p.Name != pName {
			sig += fmt.Sprintf(" // @query %q", p.Name)
		}

		paramSig = append(paramSig, sig)
	}

	// Request Body parameter if JSON
	if !isForm && op.RequestBody != nil && op.RequestBody.Value != nil {
		jsonContent := op.RequestBody.Value.Content.Get("application/json")
		if jsonContent != nil && jsonContent.Schema != nil {
			bodyType := "any"
			if jsonContent.Schema.Ref != "" {
				bodyType = toPascalCase(path.Base(jsonContent.Schema.Ref))
			} else if jsonContent.Schema.Value != nil {
				bodyType = mapSchemaType(jsonContent.Schema.Value, cfg)
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
				fmt.Fprintf(buf, "\t\t%s,\n", p)
			}

			fmt.Fprintf(buf, "\t) error\n\n")
		} else {
			fmt.Fprintf(buf, "\t%s(\n", methodName)

			for _, p := range paramSig {
				fmt.Fprintf(buf, "\t\t%s,\n", p)
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

func determineReturnType(op *openapi3.Operation, cfg ImportConfig) string {
	if op.Responses == nil {
		return "map[string]any"
	}

	respRef := op.Responses.Value("200")
	if respRef == nil {
		respRef = op.Responses.Value("201")
	}

	if respRef == nil {
		respRef = op.Responses.Value("default")
	}

	if respRef == nil || respRef.Value == nil {
		return ""
	}

	jsonContent := respRef.Value.Content.Get("application/json")
	if jsonContent == nil {
		return "map[string]any"
	}

	if jsonContent.Schema == nil {
		return "map[string]any"
	}

	if jsonContent.Schema.Ref != "" {
		typeName := toPascalCase(path.Base(jsonContent.Schema.Ref))
		return "*" + typeName
	}

	if jsonContent.Schema.Value != nil {
		s := jsonContent.Schema.Value
		if s.Type != nil && s.Type.Is("array") {
			if s.Items != nil && s.Items.Ref != "" {
				return "[]*" + toPascalCase(path.Base(s.Items.Ref))
			}

			return "[]" + mapSchemaType(s.Items.Value, cfg)
		}

		if s.Type != nil && s.Type.Is("object") && len(s.Properties) == 0 {
			return "map[string]any"
		}

		return mapSchemaType(s, cfg)
	}

	return "map[string]any"
}

type operationParameters struct {
	path   []*openapi3.Parameter
	query  []*openapi3.Parameter
	header []*openapi3.Parameter
}

func extractOperationParameters(
	pathStr string,
	pathItem *openapi3.PathItem,
	op *openapi3.Operation,
) operationParameters {
	var res operationParameters

	combined := append(slices.Clone(pathItem.Parameters), op.Parameters...)

	seen := make(map[string]bool)
	for _, pRef := range combined {
		if pRef == nil || pRef.Value == nil {
			continue
		}

		p := pRef.Value

		key := p.In + ":" + p.Name
		if seen[key] {
			continue
		}

		seen[key] = true

		switch p.In {
		case openapi3.ParameterInPath:
			res.path = append(res.path, p)
		case openapi3.ParameterInQuery:
			res.query = append(res.query, p)
		case openapi3.ParameterInHeader:
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

			res.path = append(res.path, &openapi3.Parameter{
				Name:     varName,
				In:       openapi3.ParameterInPath,
				Required: true,
				Schema:   openapi3.NewStringSchema().NewRef(),
			})
		}
	}

	return res
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

	for init := range initialisms {
		if strings.HasPrefix(pascal, init) && len(pascal) > len(init) {
			return strings.ToLower(init) + pascal[len(init):]
		}
	}

	return strings.ToLower(pascal[:1]) + pascal[1:]
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
