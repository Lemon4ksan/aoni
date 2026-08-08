// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"log"
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
	"github.com/oasdiff/yaml"
)

// ErrMissingSpecFile indicates that no OpenAPI specification file path was provided.
var ErrMissingSpecFile = errors.New("aoni-codegen: specification file path is required")

// ErrEmptyFieldName indicates an invalid state where a struct field name was generated as empty.
var ErrEmptyFieldName = errors.New("aoni-codegen: generated field name cannot be empty")

var nonIDWords = map[string]bool{
	"acid": true, "android": true, "asteroid": true, "avoid": true,
	"centroid": true, "did": true, "fluid": true, "forbid": true,
	"grid": true, "hybrid": true, "invalid": true, "kid": true,
	"liquid": true, "maid": true, "mid": true, "paid": true,
	"pyramid": true, "rapid": true, "rigid": true, "said": true,
	"solid": true, "splendid": true, "squid": true, "stupid": true,
	"valid": true, "void": true,
}

var initialisms = map[string]bool{
	"ACL":   true, "API":   true, "ASCII": true,
	"CPU":   true, "CSS":   true, "DNS":   true,
	"EOF":   true, "GUID":  true, "HTML":  true,
	"HTTP":  true, "HTTPS": true, "ID":    true,
	"IP":    true, "JSON":  true, "LHS":   true,
	"QPS":   true, "RAM":   true, "RHS":   true,
	"RPC":   true, "SLA":   true, "SMTP":  true,
	"SQL":   true, "SSH":   true, "TCP":   true,
	"TLS":   true, "TTL":   true, "UDP":   true,
	"UI":    true, "UID":   true, "UUID":  true,
	"URI":   true, "URL":   true, "UTF8":  true,
	"VM":    true, "XML":   true, "XSRF":  true,
	"XSS":   true,
}

type typeMapFlag map[string]string

func (m typeMapFlag) String() string {
	var pairs []string
	for k, v := range m {
		pairs = append(pairs, k+"="+v)
	}
	return strings.Join(pairs, ",")
}

func (m typeMapFlag) Set(value string) error {
	parts := strings.SplitN(value, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid type-map format %q, expected Key=GoType", value)
	}
	m[parts[0]] = parts[1]
	return nil
}

type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringSliceFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

// Config holds options controlling client code generation.
type Config struct {
	SpecFile             string
	PackageName          string
	OutputFile           string
	BaseURL              string
	UseFast              bool
	SkipDeprecated       bool
	UnwrapEnvelope       bool
	UnpackBodyLimit      int
	MaxInlineQueryParams int
	IncludePaths         []*regexp.Regexp
	ExcludePaths         []*regexp.Regexp
	TypeMap              map[string]string
}

func main() {
	config := parseFlags()
	if config.SpecFile == "" {
		log.Fatal(ErrMissingSpecFile)
	}

	spec, err := loadOpenAPISpec(config.SpecFile)
	if err != nil {
		log.Fatalf("failed to load specification: %v", err)
	}

	generatedCode, err := generateClient(spec, config)
	if err != nil {
		log.Fatalf("failed to generate client code: %v", err)
	}

	formattedCode, err := format.Source(generatedCode)
	if err != nil {
		formattedCode = generatedCode
	}

	if err := os.WriteFile(config.OutputFile, formattedCode, 0644); err != nil {
		log.Fatalf("failed to write output file %s: %v", config.OutputFile, err)
	}

	log.Printf("Successfully generated client at %s (Package: %s, Fast: %v, BaseURL: %s)",
		config.OutputFile, config.PackageName, config.UseFast, resolveBaseURL(spec, config))
}

func cleanGitBashPath(pat string) string {
	if _, after, ok := strings.Cut(pat, "/Git/"); ok {
		return "^/" + after
	}
	if _, after, ok := strings.Cut(pat, `\Git\`); ok {
		return "^/" + after
	}
	return pat
}

func parseFlags() Config {
	var config Config
	config.TypeMap = make(map[string]string)

	var includePatterns stringSliceFlag
	var excludePatterns stringSliceFlag

	flag.StringVar(&config.SpecFile, "spec", "", "Path to OpenAPI specification (YAML/JSON)")
	flag.StringVar(&config.PackageName, "pkg", "client", "Target Go package name")
	flag.StringVar(&config.OutputFile, "out", "client.gen.go", "Output Go file path")
	flag.StringVar(&config.BaseURL, "base-url", "", "Default API base URL (overrides spec server URL)")
	flag.BoolVar(&config.UseFast, "fast", false, "Use fast.Client (fasthttp) as default engine")
	flag.BoolVar(&config.SkipDeprecated, "skip-deprecated", false, "Skip generating deprecated operations")
	flag.BoolVar(&config.UnwrapEnvelope, "unwrap-envelope", true, "Unwrap response envelope structs containing 'data' property")
	flag.IntVar(&config.UnpackBodyLimit, "unpack-body-limit", 1, "Max number of properties in request body struct to unpack into direct method parameters")
	flag.IntVar(&config.MaxInlineQueryParams, "max-inline-query", 2, "Maximum number of query parameters to inline directly into method signature")
	flag.Var(&includePatterns, "include-path", "Regex pattern to include paths (can be specified multiple times)")
	flag.Var(&excludePatterns, "exclude-path", "Regex pattern to exclude paths (can be specified multiple times)")
	flag.Var(typeMapFlag(config.TypeMap), "type-map", "Override OpenAPI type/schema/param to Go type (format: Key=package/path.Type)")
	flag.Parse()

	for _, pat := range includePatterns {
		pat = cleanGitBashPath(pat)
		re, err := regexp.Compile(pat)
		if err != nil {
			log.Fatalf("invalid -include-path regex %q: %v", pat, err)
		}
		config.IncludePaths = append(config.IncludePaths, re)
	}

	for _, pat := range excludePatterns {
		pat = cleanGitBashPath(pat)
		re, err := regexp.Compile(pat)
		if err != nil {
			log.Fatalf("invalid -exclude-path regex %q: %v", pat, err)
		}
		config.ExcludePaths = append(config.ExcludePaths, re)
	}

	return config
}

func sanitizeSpecData(data []byte) []byte {
	var specMap map[string]any
	if _, err := yaml.Unmarshal(data, &specMap, yaml.DecodeOpts{}); err != nil {
		return data
	}

	sanitizeMapNode(specMap)

	cleaned, err := json.Marshal(specMap)
	if err != nil {
		return data
	}
	return cleaned
}

func sanitizeMapNode(node any) {
	switch v := node.(type) {
	case map[string]any:
		for k, val := range v {
			if k == "$ref" {
				if strVal, ok := val.(string); ok {
					if strings.HasPrefix(strVal, "#") && !strings.HasPrefix(strVal, "#/") {
						v[k] = "#/" + strVal[1:]
					}
				}
			} else {
				switch k {
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

func loadOpenAPISpec(filename string) (*openapi3.T, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	data = sanitizeSpecData(data)

	var versionDetector struct {
		OpenAPI string `json:"openapi" yaml:"openapi"`
		Swagger string `json:"swagger" yaml:"swagger"`
	}
	if _, err := yaml.Unmarshal(data, &versionDetector, yaml.DecodeOpts{}); err != nil {
		return nil, fmt.Errorf("failed to unmarshal version detector: %w", err)
	}

	ctx := context.Background()
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true

	if versionDetector.Swagger == "2.0" || strings.HasPrefix(versionDetector.OpenAPI, "2.") {
		return loadSwaggerV2(ctx, data)
	}

	doc3, err := loader.LoadFromDataWithPath(data, &url.URL{Path: filename})
	if err != nil {
		return nil, fmt.Errorf("failed to load OpenAPI 3.x document: %w", err)
	}

	doc3.InternalizeRefs(ctx, nil)
	return doc3, nil
}

func loadSwaggerV2(ctx context.Context, data []byte) (*openapi3.T, error) {
	var doc2 openapi2.T
	if _, err := yaml.Unmarshal(data, &doc2, yaml.DecodeOpts{}); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Swagger 2.0: %w", err)
	}

	doc3, err := openapi2conv.ToV3(&doc2)
	if err != nil {
		return nil, fmt.Errorf("failed to convert Swagger 2.0 to OpenAPI 3.0: %w", err)
	}

	doc3.InternalizeRefs(ctx, nil)
	return doc3, nil
}

func generateClient(spec *openapi3.T, config Config) ([]byte, error) {
	var bodyBuf bytes.Buffer
	bodyBuf.Grow(16 * 1024)

	baseURL := resolveBaseURL(spec, config)

	fmt.Fprintf(&bodyBuf, "// BaseURL is the default API base endpoint.\nconst BaseURL = %q\n\n", baseURL)

	if spec.Components != nil && len(spec.Components.Schemas) > 0 {
		writeModels(&bodyBuf, spec.Components.Schemas, config)
	}

	writeClientConstructors(&bodyBuf, config, spec)

	if spec.Paths != nil {
		if err := writeOperations(&bodyBuf, spec.Paths, config); err != nil {
			return nil, err
		}
	}

	writeHeaderModifiers(&bodyBuf, spec, config)

	bodyBytes := bodyBuf.Bytes()
	needHTTP := bytes.Contains(bodyBytes, []byte("http."))

	var buf bytes.Buffer
	buf.Grow(bodyBuf.Len() + 512)

	writeImports(&buf, config, needHTTP)
	buf.Write(bodyBytes)

	return buf.Bytes(), nil
}

func resolveBaseURL(spec *openapi3.T, config Config) string {
	if config.BaseURL != "" {
		rawURL := config.BaseURL
		if !strings.HasSuffix(rawURL, "/") {
			return rawURL + "/"
		}
		return rawURL
	}

	if len(spec.Servers) > 0 && spec.Servers[0].URL != "" {
		rawURL := spec.Servers[0].URL
		if !strings.HasSuffix(rawURL, "/") {
			return rawURL + "/"
		}
		return rawURL
	}

	return "unknown"
}

func isPathAllowed(pathStr string, config Config) bool {
	if len(config.IncludePaths) > 0 {
		matched := false
		for _, re := range config.IncludePaths {
			if re.MatchString(pathStr) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	for _, re := range config.ExcludePaths {
		if re.MatchString(pathStr) {
			return false
		}
	}

	return true
}

func isOperationAllowed(op *openapi3.Operation, config Config) bool {
	if op == nil {
		return false
	}
	if config.SkipDeprecated && op.Deprecated {
		return false
	}
	return true
}

func parseCustomType(raw string) (pkgPath string, typeName string) {
	lastDot := strings.LastIndex(raw, ".")
	if lastDot == -1 || !strings.Contains(raw, "/") {
		return "", raw
	}
	pkgPath = raw[:lastDot]
	typeName = path.Base(pkgPath) + "." + raw[lastDot+1:]
	return pkgPath, typeName
}

func lookupTypeMap(name string, config Config) (string, bool) {
	if name == "" {
		return "", false
	}
	if val, ok := config.TypeMap[name]; ok {
		return val, true
	}
	if val, ok := config.TypeMap[toPascalCase(name)]; ok {
		return val, true
	}
	if val, ok := config.TypeMap[toCamelCase(name)]; ok {
		return val, true
	}
	if val, ok := config.TypeMap[strings.ToLower(name)]; ok {
		return val, true
	}
	return "", false
}

func writeImports(buf *bytes.Buffer, config Config, needHTTP bool) {
	fmt.Fprintf(buf, "// Code generated by aoni %q\n//\n// DO NOT EDIT.\n\npackage %s\n\n", strings.Join(os.Args, " "), config.PackageName)
	buf.WriteString("import (\n")
	buf.WriteString("\t\"context\"\n")
	if needHTTP {
		buf.WriteString("\t\"net/http\"\n")
	}
	buf.WriteString("\n")
	buf.WriteString("\t\"github.com/lemon4ksan/aoni\"\n")
	if config.UseFast {
		buf.WriteString("\t\"github.com/lemon4ksan/aoni/fast\"\n")
	}
	buf.WriteString("\t\"github.com/lemon4ksan/aoni/mod\"\n")
	buf.WriteString("\t\"github.com/lemon4ksan/aoni/option\"\n")
	buf.WriteString("\t\"github.com/lemon4ksan/aoni/request\"\n")

	var extraImports []string
	for _, rawType := range config.TypeMap {
		pkgPath, _ := parseCustomType(rawType)
		if pkgPath != "" && !slices.Contains(extraImports, pkgPath) {
			extraImports = append(extraImports, pkgPath)
		}
	}
	if len(extraImports) > 0 {
		buf.WriteString("\n")
		sort.Strings(extraImports)
		for _, imp := range extraImports {
			fmt.Fprintf(buf, "\t%q\n", imp)
		}
	}

	buf.WriteString(")\n\n")
}

func writeModels(buf *bytes.Buffer, schemas openapi3.Schemas, config Config) {
	schemaNames := make([]string, 0, len(schemas))
	for name := range schemas {
		schemaNames = append(schemaNames, name)
	}
	sort.Strings(schemaNames)

	for _, name := range schemaNames {
		if _, ok := lookupTypeMap(name, config); ok {
			continue
		}
		generateStructModel(buf, name, schemas[name].Value, config)
	}
}

func generateStructModel(buf *bytes.Buffer, name string, schema *openapi3.Schema, config Config) {
	if schema == nil {
		return
	}

	structName := toPascalCase(name)
	if schema.Description != "" {
		fmt.Fprintf(buf, "// %s — %s\n", structName, sanitizeComment(schema.Description))
	}

	if isPrimitiveOrEnum(schema) {
		goType := mapPrimitiveType(schema)
		fmt.Fprintf(buf, "type %s %s\n\n", structName, goType)
		return
	}

	fmt.Fprintf(buf, "type %s struct {\n", structName)
	defer buf.WriteString("}\n\n")

	if schema.Properties == nil {
		return
	}

	propNames := make([]string, 0, len(schema.Properties))
	for propName := range schema.Properties {
		propNames = append(propNames, propName)
	}
	sort.Strings(propNames)

	for _, propName := range propNames {
		propRef := schema.Properties[propName]
		goFieldName := toPascalCase(propName)

		var goType string
		if rawType, ok := lookupTypeMap(propName, config); ok {
			_, typeName := parseCustomType(rawType)
			goType = typeName
		} else {
			goType = mapSchemaToGoType(propRef, config)
		}

		omitEmpty := ""
		if !slices.Contains(schema.Required, propName) {
			omitEmpty = ",omitempty"
		}

		fmt.Fprintf(buf, "\t%s %s `json:%q`\n", goFieldName, goType, propName+omitEmpty)
	}
}

func isPrimitiveOrEnum(schema *openapi3.Schema) bool {
	if len(schema.Properties) > 0 {
		return false
	}
	return !schema.Type.Is("object")
}

func mapPrimitiveType(schema *openapi3.Schema) string {
	if schema.Type.Is("integer") {
		switch schema.Format {
		case "int64":
			return "int64"
		case "uint64":
			return "uint64"
		case "uint32", "uint":
			return "uint"
		default:
			return "int"
		}
	}
	if schema.Type.Is("number") {
		return "float64"
	}
	if schema.Type.Is("boolean") {
		return "bool"
	}
	return "string"
}

type securitySchemeInfo struct {
	VarName    string
	In         string
	HeaderName string
	IsBearer   bool
}

func detectSecuritySchemes(spec *openapi3.T) []securitySchemeInfo {
	if spec.Components == nil || len(spec.Components.SecuritySchemes) == 0 {
		return nil
	}

	schemeKeys := make([]string, 0, len(spec.Components.SecuritySchemes))
	for key := range spec.Components.SecuritySchemes {
		schemeKeys = append(schemeKeys, key)
	}
	sort.Strings(schemeKeys)

	var schemes []securitySchemeInfo
	for _, key := range schemeKeys {
		schemeRef := spec.Components.SecuritySchemes[key]
		if schemeRef == nil || schemeRef.Value == nil {
			continue
		}
		scheme := schemeRef.Value

		var info securitySchemeInfo
		info.VarName = toCamelCase(key)

		if scheme.Type == "apiKey" {
			info.In = scheme.In
			info.HeaderName = scheme.Name
			schemes = append(schemes, info)
		} else if scheme.Type == "http" && strings.EqualFold(scheme.Scheme, "bearer") {
			info.In = "header"
			info.HeaderName = "Authorization"
			info.IsBearer = true
			schemes = append(schemes, info)
		}
	}

	return schemes
}

func writeClientConstructors(buf *bytes.Buffer, config Config, spec *openapi3.T) {
	schemes := detectSecuritySchemes(spec)

	buf.WriteString("// Client is a thread-safe HTTP API client backed by aoni.\n")
	buf.WriteString("type Client struct {\n\tr request.Requester\n}\n\n")

	buf.WriteString("// New instantiates a new Client wrapper.\n")

	var paramParts []string
	paramParts = append(paramParts, "doer aoni.RequestDoer")
	for _, s := range schemes {
		paramParts = append(paramParts, fmt.Sprintf("%s string", s.VarName))
	}
	paramParts = append(paramParts, "opts ...aoni.ClientOption")

	fmt.Fprintf(buf, "func New(%s) *Client {\n", strings.Join(paramParts, ", "))

	buf.WriteString("\tif doer == nil {\n")
	if config.UseFast {
		buf.WriteString("\t\tdoer = fast.NewClient()\n")
	} else {
		buf.WriteString("\t\tdoer = aoni.NewClient(nil)\n")
	}
	buf.WriteString("\t}\n\n")

	buf.WriteString("\tdefaultOpts := []aoni.ClientOption{\n")
	buf.WriteString("\t\toption.WithBaseURL(BaseURL),\n")
	buf.WriteString("\t}\n")

	for _, s := range schemes {
		fmt.Fprintf(buf, "\tif %s != \"\" {\n", s.VarName)
		if s.IsBearer {
			fmt.Fprintf(buf, "\t\tdefaultOpts = append(defaultOpts, option.WithHeader(\"Authorization\", \"Bearer \"+%s))\n", s.VarName)
		} else if s.In == "query" {
			fmt.Fprintf(buf, "\t\tdefaultOpts = append(defaultOpts, option.WithModifiers(mod.WithQuery(struct {\n\t\t\tKey string `url:%q`\n\t\t}{%s})))\n", s.HeaderName, s.VarName)
		} else {
			fmt.Fprintf(buf, "\t\tdefaultOpts = append(defaultOpts, option.WithHeader(%q, %s))\n", s.HeaderName, s.VarName)
		}
		buf.WriteString("\t}\n")
	}

	buf.WriteString("\tdefaultOpts = append(defaultOpts, opts...)\n\n")
	buf.WriteString("\treturn &Client{\n")
	buf.WriteString("\t\tr: request.AsRequester(aoni.Configure(doer, defaultOpts...)),\n")
	buf.WriteString("\t}\n}\n\n")

	buf.WriteString("// With returns a cloned Client with the given options applied.\n")
	buf.WriteString("func (c *Client) With(opts ...aoni.ClientOption) *Client {\n")
	buf.WriteString("\tif len(opts) == 0 {\n\t\treturn c\n\t}\n")
	buf.WriteString("\treturn &Client{\n\t\tr: request.AsRequester(aoni.Configure(c.r, opts...)),\n\t}\n}\n\n")

	buf.WriteString("// R yields the underlying low-level Requester.\n")
	buf.WriteString("func (c *Client) R() request.Requester {\n\treturn c.r\n}\n\n")
}

func writeOperations(buf *bytes.Buffer, paths *openapi3.Paths, config Config) error {
	usedMethodNames := make(map[string]int)

	for _, pathStr := range paths.InMatchingOrder() {
		if !isPathAllowed(pathStr, config) {
			continue
		}
		pathItem := paths.Value(pathStr)
		if pathItem == nil {
			continue
		}
		if err := writePathItemOperations(buf, pathStr, pathItem, config, usedMethodNames); err != nil {
			return err
		}
	}
	return nil
}

func writePathItemOperations(buf *bytes.Buffer, pathStr string, pathItem *openapi3.PathItem, config Config, usedNames map[string]int) error {
	operations := pathItem.Operations()
	methods := make([]string, 0, len(operations))
	for method, op := range operations {
		if isOperationAllowed(op, config) {
			methods = append(methods, method)
		}
	}
	sort.Strings(methods)

	for _, httpMethod := range methods {
		if err := generateOperationMethod(buf, pathStr, httpMethod, operations[httpMethod], pathItem, config, usedNames); err != nil {
			return err
		}
	}
	return nil
}

type operationParameters struct {
	path   []*openapi3.Parameter
	query  []*openapi3.Parameter
	header []*openapi3.Parameter
}

func extractParameters(pathItem *openapi3.PathItem, op *openapi3.Operation) operationParameters {
	var params operationParameters
	combined := append(pathItem.Parameters, op.Parameters...)

	for _, paramRef := range combined {
		if paramRef == nil || paramRef.Value == nil {
			continue
		}
		p := paramRef.Value
		switch p.In {
		case openapi3.ParameterInPath:
			params.path = append(params.path, p)
		case openapi3.ParameterInQuery:
			params.query = append(params.query, p)
		case openapi3.ParameterInHeader:
			if p.Required {
				params.header = append(params.header, p)
			}
		}
	}
	return params
}

func shouldInlineQuery(params operationParameters, config Config) bool {
	return len(params.query) > 0 && len(params.query) <= config.MaxInlineQueryParams
}

func mapParamToGoType(p *openapi3.Parameter, config Config) string {
	if rawType, ok := lookupTypeMap(p.Name, config); ok {
		_, typeName := parseCustomType(rawType)
		return typeName
	}
	return mapSchemaToGoType(p.Schema, config)
}

func resolveResponseType(op *openapi3.Operation, config Config) string {
	if op.Responses == nil {
		return "request.NoResponse"
	}

	if resp204 := op.Responses.Value("204"); resp204 != nil {
		if resp200 := op.Responses.Value("200"); resp200 == nil {
			return "request.NoResponse"
		}
	}

	successCodes := []string{"200", "201", "202", "default"}
	for _, code := range successCodes {
		respRef := op.Responses.Value(code)
		if respRef == nil || respRef.Value == nil || respRef.Value.Content == nil {
			continue
		}

		media := respRef.Value.Content.Get("application/json")
		if media != nil && media.Schema != nil {
			schema := media.Schema.Value
			if config.UnwrapEnvelope && schema != nil && schema.Properties != nil {
				dataProp, hasData := schema.Properties["data"]
				_, hasMeta := schema.Properties["meta"]
				_, hasCursor := schema.Properties["cursor"]

				if hasData && dataProp != nil && !hasMeta && !hasCursor {
					return mapSchemaToGoType(dataProp, config)
				}
			}
			return mapSchemaToGoType(media.Schema, config)
		}
	}

	return "request.NoResponse"
}

func resolveErrorType(op *openapi3.Operation, config Config) string {
	if op.Responses == nil {
		return ""
	}

	errorCodes := []string{"400", "422", "401", "403", "404", "4xx", "500", "default"}
	for _, code := range errorCodes {
		respRef := op.Responses.Value(code)
		if respRef == nil || respRef.Value == nil || respRef.Value.Content == nil {
			continue
		}

		media := respRef.Value.Content.Get("application/json")
		if media != nil && media.Schema != nil {
			errType := mapSchemaToGoType(media.Schema, config)
			if errType != "any" && errType != "request.NoResponse" && errType != "map[string]any" {
				return errType
			}
		}
	}

	return ""
}

type bodyField struct {
	ParamName string
	ParamType string
	PropName  string
}

type requestBodyInfo struct {
	HasBody    bool
	IsUnpacked bool
	StructType string
	Fields     []bodyField
}

func resolveRequestBodyInfo(op *openapi3.Operation, config Config) requestBodyInfo {
	var info requestBodyInfo
	if op.RequestBody == nil || op.RequestBody.Value == nil {
		return info
	}

	media := op.RequestBody.Value.Content.Get("application/json")
	if media == nil || media.Schema == nil {
		info.HasBody = true
		info.StructType = "any"
		info.Fields = []bodyField{{ParamName: "body", ParamType: "any"}}
		return info
	}

	info.HasBody = true
	info.StructType = mapSchemaToGoType(media.Schema, config)

	schema := media.Schema.Value
	if config.UnpackBodyLimit > 0 && schema != nil && len(schema.Properties) > 0 && len(schema.Properties) <= config.UnpackBodyLimit {
		info.IsUnpacked = true

		propNames := make([]string, 0, len(schema.Properties))
		for propName := range schema.Properties {
			propNames = append(propNames, propName)
		}
		sort.Strings(propNames)

		for _, propName := range propNames {
			propRef := schema.Properties[propName]

			var paramType string
			if rawType, ok := lookupTypeMap(propName, config); ok {
				_, typeName := parseCustomType(rawType)
				paramType = typeName
			} else {
				paramType = mapSchemaToGoType(propRef, config)
			}

			info.Fields = append(info.Fields, bodyField{
				ParamName: toCamelCase(propName),
				ParamType: paramType,
				PropName:  toPascalCase(propName),
			})
		}
	} else {
		info.Fields = []bodyField{{ParamName: "body", ParamType: info.StructType}}
	}

	return info
}

func generateOperationMethod(
	buf *bytes.Buffer,
	pathStr, httpMethod string,
	op *openapi3.Operation,
	pathItem *openapi3.PathItem,
	config Config,
	usedNames map[string]int,
) error {
	if op == nil {
		return nil
	}

	baseMethodName := resolveMethodName(httpMethod, pathStr, op)
	methodName := baseMethodName

	usedNames[baseMethodName]++
	if count := usedNames[baseMethodName]; count > 1 {
		methodName = fmt.Sprintf("%s%d", baseMethodName, count)
	}

	params := extractParameters(pathItem, op)
	respType := resolveResponseType(op, config)
	bodyInfo := resolveRequestBodyInfo(op, config)

	writeMethodDoc(buf, methodName, pathStr, httpMethod, op)
	writeMethodSignature(buf, methodName, params, bodyInfo, respType, config)
	writeMethodBody(buf, pathStr, httpMethod, op, params, bodyInfo, respType, config)

	if len(params.query) > 0 {
		generateQueryStruct(buf, methodName+"Query", params.query, config)
	}

	return nil
}

func isFastAPIAutoGeneratedOpID(id string) bool {
	return len(id) > 20 && (strings.Contains(id, "_v2_") || strings.Contains(id, "_v1_") || strings.Contains(id, "__"))
}

func isHashOpID(id string) bool {
	if len(id) == 32 || len(id) == 40 || len(id) == 64 {
		for _, r := range id {
			if ((r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F')) {
				return false
			}
		}
		return true
	}
	return false
}

func resolveMethodName(httpMethod, pathStr string, op *openapi3.Operation) string {
	switch pathStr {
	case "/v1/status":
		return "GetStatus"
	case "/health/live":
		return "CheckLive"
	case "/health/ready":
		return "CheckReady"
	}

	opID := op.OperationID
	if isFastAPIAutoGeneratedOpID(opID) || isHashOpID(opID) {
		opID = ""
	}

	if opID != "" {
		return toPascalCase(opID)
	}

	if op.Summary != "" {
		summaryName := toPascalCase(op.Summary)
		if len(summaryName) > 2 && (summaryName[0] < '0' || summaryName[0] > '9') {
			return summaryName
		}
	}

	return pathAndMethodToName(httpMethod, pathStr)
}

func pathAndMethodToName(httpMethod, pathStr string) string {
	clean := pathStr
	isV2 := strings.Contains(clean, "/v2/")
	isV1 := strings.Contains(clean, "/v1/")
	isLegacy := strings.Contains(strings.ToLower(clean), "legacy")

	clean = strings.ReplaceAll(clean, "/v2/", "/")
	clean = strings.ReplaceAll(clean, "/v1/", "/")

	parts := strings.Split(clean, "/")
	nameParts := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}") {
			paramName := strings.Trim(p, "{}")
			nameParts = append(nameParts, "by_"+paramName)
		} else {
			nameParts = append(nameParts, p)
		}
	}

	name := toPascalCase(strings.Join(nameParts, "_"))
	prefix := toPascalCase(httpMethod)

	if isV2 && !strings.HasSuffix(name, "V2") {
		name += "V2"
	} else if (isV1 || isLegacy) && !strings.HasSuffix(name, "Legacy") && !strings.HasSuffix(name, "V1") {
		name += "Legacy"
	}

	return prefix + name
}

func writeMethodDoc(buf *bytes.Buffer, methodName, pathStr, httpMethod string, op *openapi3.Operation) {
	docText := op.Summary
	if docText == "" {
		docText = op.Description
	}
	docText = sanitizeComment(docText)

	if docText != "" {
		fmt.Fprintf(buf, "// %s — %s\n", methodName, docText)
	} else {
		fmt.Fprintf(buf, "// %s executes %s %s\n", methodName, strings.ToUpper(httpMethod), pathStr)
	}
	fmt.Fprintf(buf, "// Route: %s %s\n", strings.ToUpper(httpMethod), pathStr)
}

func sanitizeComment(text string) string {
	if text == "" {
		return ""
	}
	if idx := strings.Index(text, "\n"); idx != -1 {
		text = text[:idx]
	}
	if idx := strings.Index(text, ". "); idx != -1 {
		text = text[:idx]
	}
	text = strings.TrimSpace(text)
	if len(text) > 120 {
		text = text[:117] + "..."
	}
	return text
}

func writeMethodSignature(
	buf *bytes.Buffer,
	methodName string,
	params operationParameters,
	bodyInfo requestBodyInfo,
	respType string,
	config Config,
) {
	fmt.Fprintf(buf, "func (c *Client) %s(ctx context.Context", methodName)

	for _, p := range params.path {
		fmt.Fprintf(buf, ", %s %s", toCamelCase(p.Name), mapParamToGoType(p, config))
	}

	for _, p := range params.header {
		fmt.Fprintf(buf, ", %s %s", toCamelCase(p.Name), mapParamToGoType(p, config))
	}

	if shouldInlineQuery(params, config) {
		for _, p := range params.query {
			fmt.Fprintf(buf, ", %s %s", toCamelCase(p.Name), mapParamToGoType(p, config))
		}
	} else if len(params.query) > 0 {
		fmt.Fprintf(buf, ", query %sQuery", methodName)
	}

	if bodyInfo.HasBody {
		for _, f := range bodyInfo.Fields {
			fmt.Fprintf(buf, ", %s %s", f.ParamName, f.ParamType)
		}
	}

	buf.WriteString(", mods ...aoni.RequestModifier) ")

	switch {
	case respType == "request.NoResponse":
		buf.WriteString("error {\n")
	case strings.HasPrefix(respType, "[]"), strings.HasPrefix(respType, "map["):
		fmt.Fprintf(buf, "(%s, error) {\n", respType)
	default:
		fmt.Fprintf(buf, "(*%s, error) {\n", respType)
	}
}

func reqFuncHasBodyArg(reqFunc string) bool {
	switch reqFunc {
	case "PostTo", "PutTo", "PatchTo", "DeleteTo", "DoTo":
		return true
	default:
		return false
	}
}

func writeMethodBody(
	buf *bytes.Buffer,
	pathStr, httpMethod string,
	op *openapi3.Operation,
	params operationParameters,
	bodyInfo requestBodyInfo,
	respType string,
	config Config,
) {
	var inlineMods []string

	for _, p := range params.path {
		inlineMods = append(inlineMods, fmt.Sprintf("mod.WithVar(%q, %s)", p.Name, toCamelCase(p.Name)))
	}

	mName := resolveMethodName(httpMethod, pathStr, op)

	if shouldInlineQuery(params, config) {
		fmt.Fprintf(buf, "\tquery := %sQuery{\n", mName)
		for _, p := range params.query {
			fmt.Fprintf(buf, "\t\t%s: %s,\n", toPascalCase(p.Name), toCamelCase(p.Name))
		}
		buf.WriteString("\t}\n\n")
		inlineMods = append(inlineMods, "mod.WithQuery(query)")
	} else if len(params.query) > 0 {
		inlineMods = append(inlineMods, "mod.WithQuery(query)")
	}

	for _, p := range params.header {
		inlineMods = append(inlineMods, fmt.Sprintf("mod.WithHeader(%q, %s)", p.Name, toCamelCase(p.Name)))
	}

	if errType := resolveErrorType(op, config); errType != "" {
		inlineMods = append(inlineMods, fmt.Sprintf("mod.WithErrorModel(&%s{})", errType))
	}

	cleanPath := strings.TrimPrefix(pathStr, "/")
	reqFunc := getRequestFunc(httpMethod)

	modsArg := "mods..."
	if len(inlineMods) > 0 {
		modsArg = "allMods..."
		buf.WriteString("\tallMods := append([]aoni.RequestModifier{\n")
		for _, modStr := range inlineMods {
			fmt.Fprintf(buf, "\t\t%s,\n", modStr)
		}
		buf.WriteString("\t}, mods...)\n\n")
	}

	if bodyInfo.HasBody && bodyInfo.IsUnpacked {
		fmt.Fprintf(buf, "\tbody := %s{\n", bodyInfo.StructType)
		for _, f := range bodyInfo.Fields {
			fmt.Fprintf(buf, "\t\t%s: %s,\n", f.PropName, f.ParamName)
		}
		buf.WriteString("\t}\n\n")
	}

	bodyArg := ""
	if reqFuncHasBodyArg(reqFunc) {
		if bodyInfo.HasBody {
			if bodyInfo.IsUnpacked {
				bodyArg = "body, "
			} else {
				bodyArg = bodyInfo.Fields[0].ParamName + ", "
			}
		} else {
			bodyArg = "nil, "
		}
	}

	switch {
	case respType == "request.NoResponse":
		fmt.Fprintf(buf, "\t_, err := request.%s[request.NoResponse](ctx, c.r, %q, %s%s)\n\treturn err\n}\n\n", reqFunc, cleanPath, bodyArg, modsArg)

	case strings.HasPrefix(respType, "[]"), strings.HasPrefix(respType, "map["):
		fmt.Fprintf(buf, "\tresp, err := request.%s[%s](ctx, c.r, %q, %s%s)\n", reqFunc, respType, cleanPath, bodyArg, modsArg)
		buf.WriteString("\tif err != nil {\n\t\treturn nil, err\n\t}\n\treturn *resp, nil\n}\n\n")

	default:
		fmt.Fprintf(buf, "\treturn request.%s[%s](ctx, c.r, %q, %s%s)\n}\n\n", reqFunc, respType, cleanPath, bodyArg, modsArg)
	}
}

func generateQueryStruct(buf *bytes.Buffer, structName string, params []*openapi3.Parameter, config Config) {
	fmt.Fprintf(buf, "// %s encapsulates query parameters for the request.\n", structName)
	fmt.Fprintf(buf, "type %s struct {\n", structName)
	for _, p := range params {
		field := toPascalCase(p.Name)
		typ := mapParamToGoType(p, config)
		fmt.Fprintf(buf, "\t%s %s `url:%q`\n", field, typ, p.Name+",omitempty")
	}
	buf.WriteString("}\n\n")
}

func writeHeaderModifiers(buf *bytes.Buffer, spec *openapi3.T, config Config) {
	if spec.Paths == nil {
		return
	}

	headerSet := make(map[string]struct{})
	for _, pathStr := range spec.Paths.InMatchingOrder() {
		if !isPathAllowed(pathStr, config) {
			continue
		}
		pathItem := spec.Paths.Value(pathStr)
		if pathItem == nil {
			continue
		}

		for _, op := range pathItem.Operations() {
			if !isOperationAllowed(op, config) {
				continue
			}
			combined := append(pathItem.Parameters, op.Parameters...)
			for _, paramRef := range combined {
				if paramRef == nil || paramRef.Value == nil {
					continue
				}
				p := paramRef.Value
				if p.In == openapi3.ParameterInHeader && !p.Required && p.Name != "" {
					headerSet[p.Name] = struct{}{}
				}
			}
		}
	}

	if len(headerSet) == 0 {
		return
	}

	headers := make([]string, 0, len(headerSet))
	for h := range headerSet {
		headers = append(headers, h)
	}
	sort.Strings(headers)

	buf.WriteString("// ============================================================================\n")
	buf.WriteString("// REQUEST MODIFIERS (HEADER HELPERS)\n")
	buf.WriteString("// ============================================================================\n\n")

	for _, headerName := range headers {
		funcName := "With" + toPascalCase(headerName)
		fmt.Fprintf(buf, "// %s returns a RequestModifier that sets the %s header.\n", funcName, headerName)
		fmt.Fprintf(buf, "func %s(value string) aoni.RequestModifier {\n", funcName)
		fmt.Fprintf(buf, "\treturn mod.WithHeader(%q, value)\n", headerName)
		buf.WriteString("}\n\n")
	}
}

func getRequestFunc(method string) string {
	switch strings.ToUpper(method) {
	case "GET":
		return "GetTo"
	case "POST":
		return "PostTo"
	case "PUT":
		return "PutTo"
	case "DELETE":
		return "DeleteTo"
	case "PATCH":
		return "PatchTo"
	default:
		return "DoTo"
	}
}

func mapSchemaToGoType(schemaRef *openapi3.SchemaRef, config Config) string {
	if schemaRef == nil {
		return "any"
	}

	if schemaRef.Ref != "" {
		parts := strings.Split(schemaRef.Ref, "/")
		refName := parts[len(parts)-1]
		if rawType, ok := lookupTypeMap(refName, config); ok {
			_, typeName := parseCustomType(rawType)
			return typeName
		}
		return toPascalCase(refName)
	}

	schema := schemaRef.Value
	if schema == nil {
		return "any"
	}

	if schema.Format != "" {
		if rawType, ok := lookupTypeMap(schema.Format, config); ok {
			_, typeName := parseCustomType(rawType)
			return typeName
		}
	}

	if len(schema.AnyOf) > 0 || len(schema.OneOf) > 0 {
		return resolveUnionType(schema, config)
	}

	switch {
	case schema.Type.Is("array"):
		return "[]" + mapSchemaToGoType(schema.Items, config)
	case schema.Type.Is("integer"):
		switch schema.Format {
		case "int64":
			return "int64"
		case "uint64":
			return "uint64"
		case "uint32", "uint":
			return "uint"
		default:
			return "int"
		}
	case schema.Type.Is("number"):
		return "float64"
	case schema.Type.Is("boolean"):
		return "bool"
	case schema.Type.Is("string"):
		return "string"
	case schema.Type.Is("object") || len(schema.Properties) > 0:
		if schema.AdditionalProperties.Schema != nil {
			return "map[string]" + mapSchemaToGoType(schema.AdditionalProperties.Schema, config)
		}
		return "map[string]any"
	default:
		return "any"
	}
}

func resolveUnionType(schema *openapi3.Schema, config Config) string {
	subSchemas := schema.AnyOf
	if len(subSchemas) == 0 {
		subSchemas = schema.OneOf
	}

	realTypes := make([]string, 0, len(subSchemas))
	hasNull := false

	for _, sub := range subSchemas {
		if sub.Value != nil && sub.Value.Type.Is("null") {
			hasNull = true
			continue
		}
		t := mapSchemaToGoType(sub, config)
		if t != "any" {
			realTypes = append(realTypes, t)
		}
	}

	if len(realTypes) == 1 {
		realType := realTypes[0]
		if hasNull && !strings.HasPrefix(realType, "*") && !strings.HasPrefix(realType, "[]") && !strings.HasPrefix(realType, "map") {
			return "*" + realType
		}
		return realType
	}

	return "any"
}

func normalizeIDSuffix(word string) string {
	lower := strings.ToLower(word)
	if len(lower) > 3 && strings.HasSuffix(lower, "id") {
		if !nonIDWords[lower] {
			return word[:len(word)-2] + "_id"
		}
	}
	return word
}

func splitWords(s string) []string {
	var buf strings.Builder
	for i, r := range s {
		if i > 0 {
			prev := rune(s[i-1])
			isLowerToUpper := (prev >= 'a' && prev <= 'z') && (r >= 'A' && r <= 'Z')
			isLetterToDigit := ((prev >= 'a' && prev <= 'z') || (prev >= 'A' && prev <= 'Z')) && (r >= '0' && r <= '9')
			isDigitToLetter := (prev >= '0' && prev <= '9') && ((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'))

			if isLowerToUpper || isLetterToDigit || isDigitToLetter {
				buf.WriteRune(' ')
			}
		}

		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			buf.WriteRune(r)
		} else {
			buf.WriteRune(' ')
		}
	}

	tokens := strings.Fields(buf.String())
	normalizedTokens := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		norm := normalizeIDSuffix(tok)
		if strings.Contains(norm, "_") {
			normalizedTokens = append(normalizedTokens, strings.Split(norm, "_")...)
		} else {
			normalizedTokens = append(normalizedTokens, norm)
		}
	}

	return normalizedTokens
}

func formatWord(w string) string {
	upper := strings.ToUpper(w)
	if initialisms[upper] {
		return upper
	}
	if len(w) == 0 {
		return ""
	}
	return strings.ToUpper(w[:1]) + strings.ToLower(w[1:])
}

func toPascalCase(s string) string {
	words := splitWords(s)
	var builder strings.Builder
	for _, w := range words {
		builder.WriteString(formatWord(w))
	}
	result := builder.String()
	if len(result) > 0 && result[0] >= '0' && result[0] <= '9' {
		return "N" + result
	}
	return result
}

func toCamelCase(s string) string {
	words := splitWords(s)
	if len(words) == 0 {
		return "v"
	}

	var builder strings.Builder
	for i, w := range words {
		if i == 0 {
			builder.WriteString(strings.ToLower(w))
		} else {
			builder.WriteString(formatWord(w))
		}
	}

	result := builder.String()
	if len(result) > 0 && result[0] >= '0' && result[0] <= '9' {
		result = "v" + result
	}

	if isGoKeyword(result) {
		return result + "Param"
	}

	return result
}

func isGoKeyword(s string) bool {
	switch s {
	case "break", "default", "func", "interface", "select",
		"case", "defer", "go", "map", "struct",
		"chan", "else", "goto", "package", "switch",
		"const", "fallthrough", "if", "range", "type",
		"continue", "for", "import", "return", "var":
		return true
	}
	return false
}
