// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package openapi

import (
	"bytes"
	"fmt"
	"go/format"
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
	baseURL := resolveBaseURL(spec, cfg)

	writeBaseURLConstant(&bodyBuf, baseURL, cfg.ServiceName)

	if len(spec.Paths) > 0 {
		if err := writeServiceInterface(&bodyBuf, spec, cfg, baseURL); err != nil {
			return nil, err
		}
	}

	if spec.Components != nil && len(spec.Components.Schemas) > 0 {
		writeSchemas(&bodyBuf, spec.Components.Schemas, cfg)
	}

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "package %s\n\n", pkgName)
	writeImports(&buf, bodyBuf.Bytes(), cfg.TypeMap, true)
	buf.Write(bodyBuf.Bytes())

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

	// 1. API Contract (api.go)
	var apiBody bytes.Buffer
	baseURL := resolveBaseURL(spec, cfg)

	writeBaseURLConstant(&apiBody, baseURL, cfg.ServiceName)

	if len(spec.Paths) > 0 {
		if err := writeServiceInterface(&apiBody, spec, cfg, baseURL); err != nil {
			return nil, nil, err
		}
	}

	var apiBuf bytes.Buffer
	fmt.Fprintf(&apiBuf, "package %s\n\n", pkgName)
	writeImports(&apiBuf, apiBody.Bytes(), cfg.TypeMap, true)
	apiBuf.Write(apiBody.Bytes())

	apiSource, err = format.Source(apiBuf.Bytes())
	if err != nil {
		return nil, nil, fmt.Errorf("failed formatting api.go: %w\nSource:\n%s", err, apiBuf.String())
	}

	// 2. Models DTOs (models.go)
	if spec.Components != nil && len(spec.Components.Schemas) > 0 {
		var modelsBody bytes.Buffer
		writeSchemas(&modelsBody, spec.Components.Schemas, cfg)

		var modelsBuf bytes.Buffer
		fmt.Fprintf(&modelsBuf, "package %s\n\n", pkgName)
		writeImports(&modelsBuf, modelsBody.Bytes(), cfg.TypeMap, false)
		modelsBuf.Write(modelsBody.Bytes())

		modelsSource, err = format.Source(modelsBuf.Bytes())
		if err != nil {
			return nil, nil, fmt.Errorf("failed formatting models.go: %w\nSource:\n%s", err, modelsBuf.String())
		}
	}

	return apiSource, modelsSource, nil
}

func writeBaseURLConstant(buf *bytes.Buffer, baseURL, serviceName string) {
	if baseURL == "" {
		return
	}

	constName := "BaseURL"
	if serviceName != "" && serviceName != "API" {
		constName = serviceName + "BaseURL"
	}

	fmt.Fprintf(buf, "// %s is the default API base endpoint.\nconst %s = %q\n\n", constName, constName, baseURL)
}

func writeImports(buf *bytes.Buffer, body []byte, typeMap map[string]string, includeAoni bool) {
	hasTime := bytes.Contains(body, []byte("time.Time"))
	customImports := collectCustomImports(body, typeMap)

	if !includeAoni && !hasTime && len(customImports) == 0 {
		return
	}

	buf.WriteString("import (\n")
	if includeAoni {
		buf.WriteString("\t\"context\"\n")
	}
	if hasTime {
		buf.WriteString("\t\"time\"\n")
	}

	for _, imp := range customImports {
		fmt.Fprintf(buf, "\t%q\n", imp)
	}

	if includeAoni {
		buf.WriteString("\n\t\"github.com/lemon4ksan/aoni\"\n")
	}
	buf.WriteString(")\n\n")
}

func collectCustomImports(body []byte, typeMap map[string]string) []string {
	var imports []string
	for _, rawType := range typeMap {
		idx := strings.LastIndex(rawType, "/")
		if idx == -1 {
			continue
		}
		dotIdx := strings.LastIndex(rawType, ".")
		if dotIdx <= idx {
			continue
		}

		pkgPath := rawType[:dotIdx]
		short := path.Base(pkgPath) + "." + rawType[dotIdx+1:]

		if bytes.Contains(body, []byte(short)) && !slices.Contains(imports, pkgPath) {
			imports = append(imports, pkgPath)
		}
	}
	slices.Sort(imports)
	return imports
}

func resolveBaseURL(spec *Document, cfg ImportConfig) string {
	if cfg.BaseURL != "" {
		return cfg.BaseURL
	}
	if len(spec.Servers) > 0 && spec.Servers[0].URL != "" {
		return spec.Servers[0].URL
	}
	return ""
}

func writeServiceInterface(buf *bytes.Buffer, spec *Document, cfg ImportConfig, baseURL string) error {
	serviceName := cfg.ServiceName
	if serviceName == "" {
		serviceName = "API"
	}

	writeServiceHeader(buf, spec, cfg, serviceName, baseURL)
	fmt.Fprintf(buf, "type %s interface {\n", serviceName)

	pathKeys := generic.Keys(spec.Paths)
	slices.Sort(pathKeys)

	usedMethodNames := make(map[string]int)
	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}

	for _, pathStr := range pathKeys {
		pathItem := spec.Paths[pathStr]
		if pathItem == nil || !isPathAllowed(pathStr, cfg) {
			continue
		}

		ops := pathItem.OperationsMap()
		for _, httpMethod := range methods {
			op := ops[httpMethod]
			if op == nil || (cfg.SkipDeprecated && op.Deprecated) {
				continue
			}
			writeOperationMethod(buf, spec, pathStr, httpMethod, pathItem, op, cfg, usedMethodNames)
		}
	}

	fmt.Fprintf(buf, "}\n")
	return nil
}

func writeServiceHeader(buf *bytes.Buffer, spec *Document, cfg ImportConfig, serviceName, baseURL string) {
	casing := "snake_case"
	if ext := extString(spec.InfoExtensions(), "x-vortex-casing"); ext != "" {
		casing = ext
	}

	fmt.Fprintf(buf, "// %s provides the API client contract.\n//\n", serviceName)
	fmt.Fprintf(buf, "// @aoni:service casing=%s\n", casing)

	if spec.Info != nil && spec.Info.Version != "" {
		fmt.Fprintf(buf, "// @version %q\n", spec.Info.Version)
	}

	if cfg.SpecFile != "" {
		fmt.Fprintf(buf, "// @source %q\n", cfg.SpecFile)
	}

	exts := spec.InfoExtensions()
	if persona := extString(exts, "x-vortex-persona"); persona != "" {
		fmt.Fprintf(buf, "// @persona %q\n", persona)
	}
	if tlsSpec := extString(exts, "x-vortex-tlsspec"); tlsSpec != "" {
		fmt.Fprintf(buf, "// @tls_spec %q\n", tlsSpec)
	}
	if engine := extString(exts, "x-vortex-engine"); engine != "" {
		fmt.Fprintf(buf, "// @engine %s\n", engine)
	}

	for _, h := range extHeaders(exts, "x-vortex-headers") {
		if h.Name != "" && h.Value != "" {
			fmt.Fprintf(buf, "// @header %q %q\n", h.Name, h.Value)
		}
	}

	if baseURL != "" {
		fmt.Fprintf(buf, "// @base_url %q\n", baseURL)
	}
}

func (d *Document) InfoExtensions() map[string]any {
	if d == nil || d.Info == nil {
		return nil
	}
	return d.Info.Extensions
}

func isPathAllowed(pathStr string, cfg ImportConfig) bool {
	if len(cfg.IncludePaths) > 0 && !matchesAnyPattern(pathStr, cfg.IncludePaths) {
		return false
	}
	if len(cfg.ExcludePaths) > 0 && matchesAnyPattern(pathStr, cfg.ExcludePaths) {
		return false
	}
	return true
}

func matchesAnyPattern(target string, patterns []string) bool {
	for _, pattern := range patterns {
		if re, err := regexp.Compile(pattern); err == nil && re.MatchString(target) {
			return true
		}
	}
	return false
}

func writeOperationMethod(
	buf *bytes.Buffer,
	spec *Document,
	pathStr, httpMethod string,
	pathItem *PathItem,
	op *Operation,
	cfg ImportConfig,
	usedNames map[string]int,
) {
	methodName := buildMethodName(pathStr, httpMethod, op, usedNames)

	writeOperationDocumentation(buf, methodName, op)
	writeOperationDirectives(buf, pathStr, httpMethod, op, spec, pathItem)

	params := extractOperationParameters(pathStr, pathItem, op)
	isForm := isFormRequestBody(op)

	if httpMethod != "GET" && !isForm && len(params.query) > 0 {
		fmt.Fprintf(buf, "\t// @query casing=snake_case\n")
	}

	paramSig := buildParameterSignatures(params, op, isForm, httpMethod, cfg)
	returnType := determineReturnType(op, cfg)

	writeMethodSignature(buf, methodName, paramSig, returnType)
}

func writeOperationDocumentation(buf *bytes.Buffer, methodName string, op *Operation) {
	summary := op.Summary
	if summary == "" {
		summary = op.Description
	}

	if summary != "" {
		fmt.Fprintf(buf, "\t// %s — %s\n\t//\n", methodName, strings.ReplaceAll(summary, "\n", " "))
	}

	credsList := extStringList(op.Extensions, "x-required-credentials")
	if len(credsList) > 0 {
		fmt.Fprintf(buf, "\t// Security & Session Requirements (captured from traffic):\n")
		for _, cred := range credsList {
			fmt.Fprintf(buf, "\t//   - %s\n", cred)
		}
		fmt.Fprintf(buf, "\t//\n")
	}
}

func writeOperationDirectives(buf *bytes.Buffer, pathStr, httpMethod string, op *Operation, spec *Document, pathItem *PathItem) {
	cleanPath := strings.TrimPrefix(pathStr, "/")
	fmt.Fprintf(buf, "\t// @%s %q\n", strings.ToLower(httpMethod), cleanPath)

	methodName := buildMethodName(pathStr, httpMethod, op, nil)
	if op.OperationID != "" && op.OperationID != methodName {
		fmt.Fprintf(buf, "\t// @bind %q\n", op.OperationID)
	}

	if op.Deprecated {
		fmt.Fprintf(buf, "\t// @deprecated\n")
	}

	writeMethodExtensionDirectives(buf, op.Extensions, spec)

	if isFormRequestBody(op) {
		fmt.Fprintf(buf, "\t// @form casing=snake_case\n")
	}
}

func writeMethodExtensionDirectives(buf *bytes.Buffer, exts map[string]any, spec *Document) {
	if exts == nil {
		return
	}

	if unwrap := extString(exts, "x-vortex-unwrap"); unwrap != "" {
		fmt.Fprintf(buf, "\t// @unwrap %q\n", unwrap)
	}
	if callFn := extString(exts, "x-vortex-call"); callFn != "" {
		fmt.Fprintf(buf, "\t// @call %q\n", callFn)
	}
	if extBool(exts, "x-vortex-idempotent") {
		fmt.Fprintf(buf, "\t// @idempotent\n")
	}
	if extBool(exts, "x-vortex-coalesce") {
		fmt.Fprintf(buf, "\t// @coalesce\n")
	}
	if extBool(exts, "x-vortex-etag") {
		fmt.Fprintf(buf, "\t// @etag\n")
	}

	sinceVal := resolveSinceVersion(exts, spec)
	if sinceVal != "" {
		fmt.Fprintf(buf, "\t// @since %q\n", sinceVal)
	}

	writeMethodHeaders(buf, exts, spec)
}

func resolveSinceVersion(exts map[string]any, spec *Document) string {
	if since := extString(exts, "x-vortex-since"); since != "" {
		return since
	}
	if spec != nil && spec.Info != nil && spec.Info.Version != "" {
		return spec.Info.Version
	}
	return ""
}

func writeMethodHeaders(buf *bytes.Buffer, exts map[string]any, spec *Document) {
	seen := make(map[string]bool)
	for _, h := range extHeaders(exts, "x-vortex-headers") {
		if h.Name == "" || h.Value == "" || isGlobalHeader(spec, h.Name, h.Value) {
			continue
		}
		headerKey := strings.ToLower(h.Name)
		if !seen[headerKey] {
			seen[headerKey] = true
			fmt.Fprintf(buf, "\t// @header %q %q\n", h.Name, h.Value)
		}
	}
}

func isFormRequestBody(op *Operation) bool {
	if op == nil || op.RequestBody == nil {
		return false
	}
	c := op.RequestBody.Content
	return c["application/x-www-form-urlencoded"] != nil || c["multipart/form-data"] != nil
}

func buildParameterSignatures(
	params operationParameters,
	op *Operation,
	isForm bool,
	httpMethod string,
	cfg ImportConfig,
) []string {
	var sigs []string
	sigs = append(sigs, "ctx context.Context")

	for _, p := range params.path {
		pName := toCamelCase(p.Name)
		pType := resolveParamType(p, cfg)
		sigs = append(sigs, fmt.Sprintf("%s %s", pName, pType))
	}

	for _, p := range params.query {
		pName := toCamelCase(p.Name)
		pType := resolveParamType(p, cfg)
		sig := fmt.Sprintf("%s %s", pName, pType)

		expectedSnake := toSnakeCase(pName)
		if p.Name != "" && (httpMethod != "GET" || (p.Name != expectedSnake && p.Name != pName)) {
			sig += fmt.Sprintf(" // @query %q", p.Name)
		}
		sigs = append(sigs, sig)
	}

	if !isForm && op.RequestBody != nil {
		if bodyParam := resolveRequestBodyParam(op.RequestBody, cfg); bodyParam != "" {
			sigs = append(sigs, bodyParam)
		}
	}

	sigs = append(sigs, "mods ...aoni.RequestModifier")
	return sigs
}

func resolveParamType(p *Parameter, cfg ImportConfig) string {
	if cfg.TypeMap != nil {
		if mapped, ok := cfg.TypeMap[p.Name]; ok {
			return shortTypeName(mapped)
		}
		if mapped, ok := cfg.TypeMap[strings.ToLower(p.Name)]; ok {
			return shortTypeName(mapped)
		}
	}

	if p.Schema != nil {
		return mapSchemaType(p.Schema, cfg)
	}

	return "string"
}

func resolveRequestBodyParam(reqBody *RequestBody, cfg ImportConfig) string {
	jsonContent := reqBody.Content["application/json"]
	if jsonContent == nil || jsonContent.Schema == nil {
		return ""
	}

	if jsonContent.Schema.Ref != "" {
		bodyType := toPascalCase(path.Base(jsonContent.Schema.Ref))
		return "req " + bodyType
	}

	return "req " + mapSchemaType(jsonContent.Schema, cfg)
}

func writeMethodSignature(buf *bytes.Buffer, methodName string, paramSig []string, returnType string) {
	hasComments := slices.ContainsFunc(paramSig, func(p string) bool {
		return strings.Contains(p, "//")
	})

	isMultiline := len(paramSig) > 4 || hasComments

	if isMultiline {
		writeMultilineSignature(buf, methodName, paramSig, returnType)
		return
	}

	writeSingleLineSignature(buf, methodName, paramSig, returnType)
}

func writeMultilineSignature(buf *bytes.Buffer, methodName string, paramSig []string, returnType string) {
	fmt.Fprintf(buf, "\t%s(\n", methodName)
	for _, p := range paramSig {
		if idx := strings.Index(p, "//"); idx != -1 {
			codePart := strings.TrimSpace(p[:idx])
			commentPart := p[idx:]
			fmt.Fprintf(buf, "\t\t%s, %s\n", codePart, commentPart)
			continue
		}
		fmt.Fprintf(buf, "\t\t%s,\n", p)
	}

	if returnType == "" {
		fmt.Fprintf(buf, "\t) error\n\n")
		return
	}

	fmt.Fprintf(buf, "\t) (%s, error)\n\n", returnType)
}

func writeSingleLineSignature(buf *bytes.Buffer, methodName string, paramSig []string, returnType string) {
	paramsJoined := strings.Join(paramSig, ", ")
	if returnType == "" {
		fmt.Fprintf(buf, "\t%s(%s) error\n\n", methodName, paramsJoined)
		return
	}
	fmt.Fprintf(buf, "\t%s(%s) (%s, error)\n\n", methodName, paramsJoined, returnType)
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
	if resp == nil || resp.Content == nil {
		return ""
	}

	jsonContent := resp.Content["application/json"]
	if jsonContent == nil || jsonContent.Schema == nil {
		return "map[string]any"
	}

	if jsonContent.Schema.Ref != "" {
		return "*" + toPascalCase(path.Base(jsonContent.Schema.Ref))
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

type HeaderEntry struct {
	Name  string
	Value string
}

func extString(exts map[string]any, key string) string {
	if exts == nil {
		return ""
	}
	v, _ := exts[key].(string)
	return v
}

func extBool(exts map[string]any, key string) bool {
	if exts == nil {
		return false
	}
	v, _ := exts[key].(bool)
	return v
}

func extStringList(exts map[string]any, key string) []string {
	if exts == nil {
		return nil
	}
	switch raw := exts[key].(type) {
	case []string:
		return raw
	case []any:
		var res []string
		for _, item := range raw {
			if s, ok := item.(string); ok {
				res = append(res, s)
			}
		}
		return res
	default:
		return nil
	}
}

func extHeaders(exts map[string]any, key string) []HeaderEntry {
	if exts == nil {
		return nil
	}

	var res []HeaderEntry
	switch raw := exts[key].(type) {
	case []map[string]string:
		for _, h := range raw {
			res = append(res, HeaderEntry{Name: h["name"], Value: h["value"]})
		}
	case []any:
		for _, item := range raw {
			if hMap, ok := item.(map[string]any); ok {
				n, _ := hMap["name"].(string)
				v, _ := hMap["value"].(string)
				res = append(res, HeaderEntry{Name: n, Value: v})
			}
		}
	}
	return res
}

func isGlobalHeader(spec *Document, name, val string) bool {
	for _, h := range extHeaders(spec.InfoExtensions(), "x-vortex-headers") {
		if strings.EqualFold(h.Name, name) && h.Value == val {
			return true
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

	extractPathSegmentParams(pathStr, seen, &res.path)
	return res
}

func extractPathSegmentParams(pathStr string, seen map[string]bool, pathParams *[]*Parameter) {
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
			*pathParams = append(*pathParams, &Parameter{
				Name:     varName,
				In:       "path",
				Required: true,
				Schema:   &Schema{Type: TypeArray{"string"}},
			})
		}
	}
}
