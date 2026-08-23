// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package openapi

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode"

	"github.com/lemon4ksan/foundation/generic"
	"gopkg.in/yaml.v3"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
)

// ExportConfig controls OpenAPI 3.1 specification generation from aoni IR.
type ExportConfig struct {
	ServiceName string
	Title       string
	Version     string
	Description string
	BaseURL     string
	AsYAML      bool
	Vortex      bool // If true, include x-vortex vendor extensions for lossless aoni round-tripping
}

// ExportOpenAPI generates a standard OpenAPI 3.1 JSON or YAML document from aoni RootIR.
//
// References:
//   - OpenAPI 3.1.0 Specification: https://spec.openapis.org/oas/v3.1.0
//   - OpenAPI 3.1.0 §4.8.1 OpenAPI Object: https://spec.openapis.org/oas/v3.1.0#openapi-object
//   - OpenAPI 3.1.0 §4.8.7 Components Object: https://spec.openapis.org/oas/v3.1.0#components-object
//   - OpenAPI 3.1.0 §4.8.9 Path Item Object: https://spec.openapis.org/oas/v3.1.0#path-item-object
//   - OpenAPI 3.1.0 §4.8.10 Operation Object: https://spec.openapis.org/oas/v3.1.0#operation-object
//   - RFC 9110 §HTTP Semantics: https://datatracker.ietf.org/doc/html/rfc9110
func ExportOpenAPI(root *ir.RootIR, cfg ExportConfig) ([]byte, error) {
	if root == nil {
		return nil, errors.New("root IR cannot be nil")
	}

	services := filterTargetServices(root.Services, cfg.ServiceName)
	validStructs := collectValidStructs(root.Structs)

	doc := &Document{
		OpenAPI:    "3.1.0",
		Info:       buildDocumentInfo(services, root.PackageName, cfg),
		Servers:    buildDocumentServers(services, cfg.BaseURL),
		Paths:      make(map[string]*PathItem),
		Components: buildDocumentComponents(root.Structs, validStructs),
	}

	buildDocumentPaths(doc, services, cfg, validStructs)

	return encodeDocument(doc, cfg.AsYAML)
}

func filterTargetServices(services []*ir.ServiceIR, targetName string) []*ir.ServiceIR {
	if targetName == "" {
		return services
	}

	filtered := generic.Filter(services, func(s *ir.ServiceIR) bool {
		return strings.EqualFold(s.Name, targetName)
	})

	if len(filtered) > 0 {
		return filtered
	}

	return services
}

func buildDocumentInfo(services []*ir.ServiceIR, pkgName string, cfg ExportConfig) *Info {
	serviceName := ""
	if len(services) > 0 {
		serviceName = services[0].Name
	}

	title := generic.Coalesce(cfg.Title, cleanAPITitle(serviceName, pkgName))
	version := generic.Coalesce(cfg.Version, resolveServiceVersion(services))
	desc := generic.Coalesce(cfg.Description, resolveServiceDescription(services))

	info := &Info{
		Title:       title,
		Version:     version,
		Description: desc,
	}

	if len(services) > 0 && services[0].Summary != "" {
		info.Summary = services[0].Summary
	}

	if cfg.Vortex && len(services) > 0 {
		applyServiceVendorExtensions(info, services[0])
	}

	return info
}

func resolveServiceVersion(services []*ir.ServiceIR) string {
	if len(services) > 0 && services[0].Version != "" {
		return services[0].Version
	}

	return "1.0.0"
}

func resolveServiceDescription(services []*ir.ServiceIR) string {
	if len(services) > 0 && services[0].Description != "" {
		return services[0].Description
	}

	return ""
}

func applyServiceVendorExtensions(info *Info, svc *ir.ServiceIR) {
	if svc.Persona != "" {
		info.Extensions = setExtension(info.Extensions, "x-vortex-persona", svc.Persona)
	}

	if svc.TLSSpec != "" {
		info.Extensions = setExtension(info.Extensions, "x-vortex-tlsspec", svc.TLSSpec)
	}

	if svc.DefaultCasing != "" {
		info.Extensions = setExtension(info.Extensions, "x-vortex-casing", string(svc.DefaultCasing))
	}

	if svc.Engine != "" {
		info.Extensions = setExtension(info.Extensions, "x-vortex-engine", string(svc.Engine))
	}

	if svc.Source != "" {
		info.Extensions = setExtension(info.Extensions, "x-vortex-source", svc.Source)
	}
}

func buildDocumentServers(services []*ir.ServiceIR, cfgBaseURL string) []Server {
	baseURL := cfgBaseURL
	if baseURL == "" && len(services) > 0 {
		baseURL = services[0].BaseURL
	}

	if baseURL == "" {
		return nil
	}

	return []Server{{
		URL:         baseURL,
		Description: "Default API server",
	}}
}

func collectValidStructs(structs []*ir.StructIR) map[string]bool {
	valid := make(map[string]bool)
	for _, s := range structs {
		if !unicode.IsUpper(rune(s.Name[0])) || isInternalStruct(s) {
			continue
		}

		valid[s.Name] = true
	}

	valid["ErrorResponse"] = true
	valid["RateLimitError"] = true

	return valid
}

func isInternalStruct(s *ir.StructIR) bool {
	for _, f := range s.Fields {
		tName := f.Type.Name
		if strings.Contains(tName, "sync.") || strings.Contains(tName, "chan ") ||
			tName == "event.Bus" || tName == "bus.Bus" || tName == "log.Logger" || tName == "aoni.WebSocketDialer" {
			return true
		}
	}

	return false
}

func buildDocumentComponents(structs []*ir.StructIR, validStructs map[string]bool) *Components {
	comp := &Components{
		Schemas: make(map[string]*Schema),
	}

	for sName := range validStructs {
		s := findStructByName(structs, sName)
		if s == nil {
			continue
		}

		comp.Schemas[s.Name] = convertStructToSchema(s, validStructs)
	}

	ensureStandardErrorSchemas(comp.Schemas)

	return comp
}

func convertStructToSchema(s *ir.StructIR, validStructs map[string]bool) *Schema {
	schema := &Schema{
		Type:       TypeArray{"object"},
		Properties: make(map[string]*Schema),
	}

	if s.Description != "" {
		schema.Description = s.Description
	} else if desc := cleanDocSummary(s.Doc); desc != "" {
		schema.Description = desc
	}

	if s.Deprecation != nil {
		schema.Deprecated = true
	}

	for _, f := range s.Fields {
		wireKey := strings.TrimSuffix(generic.Coalesce(f.WireName, f.GoName), ",omitempty")
		schema.Properties[wireKey] = mapGoTypeToSchema(f.Type.Name, validStructs)
	}

	return schema
}

func ensureStandardErrorSchemas(schemas map[string]*Schema) {
	if _, ok := schemas["ErrorResponse"]; !ok {
		schemas["ErrorResponse"] = &Schema{
			Type: TypeArray{"object"},
			Properties: map[string]*Schema{
				"error":   {Type: TypeArray{"string"}},
				"message": {Type: TypeArray{"string"}},
			},
		}
	}

	if _, ok := schemas["RateLimitError"]; !ok {
		schemas["RateLimitError"] = &Schema{
			Type: TypeArray{"object"},
			Properties: map[string]*Schema{
				"error":      {Type: TypeArray{"string"}},
				"retryAfter": {Type: TypeArray{"integer"}, Format: "int32"},
			},
		}
	}
}

func buildDocumentPaths(doc *Document, services []*ir.ServiceIR, cfg ExportConfig, validStructs map[string]bool) {
	for _, svc := range services {
		for _, m := range svc.Methods {
			if m.Operation != ir.OpHTTP && m.HTTPMethod == "" {
				continue
			}

			rawPath := resolveMethodRoutePath(m)

			pathItem := doc.Paths[rawPath]
			if pathItem == nil {
				pathItem = &PathItem{}
				doc.Paths[rawPath] = pathItem
			}

			op := buildMethodOperation(m, svc, cfg, validStructs)
			setPathItemOp(pathItem, m.HTTPMethod, op)
		}
	}
}

func resolveMethodRoutePath(m *ir.MethodIR) string {
	rawPath := "/"
	if m.Path != nil && m.Path.RawTemplate != "" {
		rawPath = m.Path.RawTemplate
		if !strings.HasPrefix(rawPath, "/") {
			rawPath = "/" + rawPath
		}
	}

	return rawPath
}

func buildMethodOperation(
	m *ir.MethodIR,
	svc *ir.ServiceIR,
	cfg ExportConfig,
	validStructs map[string]bool,
) *Operation {
	op := &Operation{
		OperationID: generic.Coalesce(m.OperationID, m.Name),
		Summary:     resolveMethodSummary(m),
		Description: m.Description,
		Tags:        resolveMethodTags(m, svc),
		Responses:   make(map[string]*Response),
	}

	if m.Deprecation != nil {
		op.Deprecated = true
		if m.Deprecation.Reason != "" && op.Description == "" {
			op.Description = "Deprecated: " + m.Deprecation.Reason
		}
	}

	if cfg.Vortex {
		applyMethodVendorExtensions(op, m)
	}

	pathVars := buildPathParameters(op, m.Path)
	buildOperationBodyAndQueryParams(op, m, validStructs, pathVars)
	buildOperationResponses(op, m, validStructs, len(pathVars) > 0)

	return op
}

func resolveMethodSummary(m *ir.MethodIR) string {
	if m.Summary != "" {
		return m.Summary
	}

	return cleanDocSummary(m.Doc)
}

func resolveMethodTags(m *ir.MethodIR, svc *ir.ServiceIR) []string {
	if len(m.Tags) > 0 {
		return m.Tags
	}

	if len(svc.Tags) > 0 {
		return svc.Tags
	}

	return []string{svc.Name}
}

func applyMethodVendorExtensions(op *Operation, m *ir.MethodIR) {
	if m.UnwrapField != "" {
		op.Extensions = setExtension(op.Extensions, "x-vortex-unwrap", m.UnwrapField)
	}

	if m.CallFunc != "" {
		op.Extensions = setExtension(op.Extensions, "x-vortex-call", m.CallFunc)
	}

	if m.Idempotent {
		op.Extensions = setExtension(op.Extensions, "x-vortex-idempotent", true)
	}

	if m.Coalesce {
		op.Extensions = setExtension(op.Extensions, "x-vortex-coalesce", true)
	}

	if m.ETag {
		op.Extensions = setExtension(op.Extensions, "x-vortex-etag", true)
	}

	if m.FormCasing != "" {
		op.Extensions = setExtension(op.Extensions, "x-vortex-form-casing", string(m.FormCasing))
	}

	if m.QueryCasing != "" {
		op.Extensions = setExtension(op.Extensions, "x-vortex-query-casing", string(m.QueryCasing))
	}

	if m.Since != "" {
		op.Extensions = setExtension(op.Extensions, "x-vortex-since", m.Since)
	}
}

func buildPathParameters(op *Operation, p *ir.PathIR) map[string]bool {
	pathVars := make(map[string]bool)
	if p == nil {
		return pathVars
	}

	for _, seg := range p.Segments {
		if !seg.IsVariable {
			continue
		}

		pathVars[seg.VarName] = true
		pathVars[strings.ToLower(seg.VarName)] = true

		op.Parameters = append(op.Parameters, &Parameter{
			Name:     seg.VarName,
			In:       "path",
			Required: true,
			Schema:   &Schema{Type: TypeArray{"string"}},
		})
	}

	return pathVars
}

func buildOperationBodyAndQueryParams(
	op *Operation,
	m *ir.MethodIR,
	validStructs map[string]bool,
	pathVars map[string]bool,
) {
	for _, param := range m.Params {
		if param.Location == ir.LocContext || param.Location == ir.LocModifiers ||
			param.GoName == "ctx" || param.GoName == "mods" ||
			pathVars[param.GoName] || pathVars[strings.ToLower(param.GoName)] || param.Location == ir.LocPath {
			continue
		}

		typeName := strings.TrimPrefix(param.GoType.Name, "*")

		if m.PayloadKind == ir.PayloadJSON && (param.Location == ir.LocBody || isComplexType(typeName)) {
			op.RequestBody = &RequestBody{
				Content: map[string]*MediaType{
					"application/json": {
						Schema: mapGoTypeToSchema(typeName, validStructs),
					},
				},
			}

			continue
		}

		if m.PayloadKind == ir.PayloadForm &&
			(param.Location == ir.LocBody || param.Location == ir.LocFormFields || isComplexType(typeName)) {
			op.RequestBody = &RequestBody{
				Content: map[string]*MediaType{
					"application/x-www-form-urlencoded": {
						Schema: mapGoTypeToSchema(typeName, validStructs),
					},
				},
			}

			continue
		}

		qName := generic.Coalesce(param.WireKey, param.GoName)
		q := &Parameter{
			Name:   qName,
			In:     "query",
			Schema: mapGoTypeToSchema(typeName, validStructs),
		}
		applySmartQueryDefaults(q, qName)
		op.Parameters = append(op.Parameters, q)
	}
}

func applySmartQueryDefaults(q *Parameter, qName string) {
	if q.Schema == nil {
		return
	}

	switch qName {
	case "limit":
		q.Schema.Default = 10
	case "offset":
		q.Schema.Default = 0
	case "header":
		q.Schema.Default = true
	case "height":
		q.Schema.Default = 500
	case "width":
		q.Schema.Default = "100%"
	}
}

func buildOperationResponses(
	op *Operation,
	m *ir.MethodIR,
	validStructs map[string]bool,
	hasPathParams bool,
) {
	resp := &Response{
		Description: "Successful operation",
		Content:     make(map[string]*MediaType),
	}

	if m.Return != nil && !m.Return.IsVoid && m.Return.SuccessType.Name != "" && m.Return.SuccessType.Name != "error" {
		returnTypeName := strings.TrimPrefix(m.Return.SuccessType.Name, "*")

		switch {
		case m.Return.IsDirectBytes || returnTypeName == "[]byte" || strings.Contains(strings.ToLower(m.Name), "image"):
			binSchema := &Schema{Type: TypeArray{"string"}, Format: "binary"}
			resp.Content["image/png"] = &MediaType{Schema: binSchema}
			resp.Content["application/octet-stream"] = &MediaType{Schema: binSchema}
		case strings.Contains(strings.ToLower(m.Name), "graph") || returnTypeName == "html":
			resp.Content["text/html"] = &MediaType{Schema: &Schema{Type: TypeArray{"string"}}}
		default:
			resp.Content["application/json"] = &MediaType{Schema: mapGoTypeToSchema(returnTypeName, validStructs)}
		}
	}

	op.Responses["200"] = resp

	op.Responses["400"] = &Response{
		Description: "Invalid input or malformed request",
		Content: map[string]*MediaType{
			"application/json": {
				Schema: &Schema{Ref: "#/components/schemas/ErrorResponse"},
			},
		},
	}

	if hasPathParams {
		op.Responses["404"] = &Response{
			Description: "Resource or item not found",
			Content: map[string]*MediaType{
				"application/json": {
					Schema: &Schema{Ref: "#/components/schemas/ErrorResponse"},
				},
			},
		}
	}

	op.Responses["429"] = &Response{
		Description: "Rate limit exceeded",
		Content: map[string]*MediaType{
			"application/json": {
				Schema: &Schema{Ref: "#/components/schemas/RateLimitError"},
			},
		},
	}
}

func encodeDocument(doc *Document, asYAML bool) ([]byte, error) {
	if asYAML {
		var yamlBuf strings.Builder

		enc := yaml.NewEncoder(&yamlBuf)
		enc.SetIndent(2)

		if err := enc.Encode(doc); err != nil {
			return nil, err
		}

		return []byte(yamlBuf.String()), nil
	}

	return json.MarshalIndent(doc, "", "  ")
}

func setExtension(exts map[string]any, key string, val any) map[string]any {
	if exts == nil {
		exts = make(map[string]any)
	}

	exts[key] = val

	return exts
}

func cleanAPITitle(serviceName, pkgName string) string {
	if serviceName != "" && serviceName != "API" {
		return strings.TrimSuffix(serviceName, "API") + " API"
	}

	if pkgName != "" {
		return toPascalCase(pkgName) + " API"
	}

	return "OpenAPI Specification"
}

func cleanDocSummary(docLines []string) string {
	for _, l := range docLines {
		trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "//"))
		if trimmed != "" && !strings.HasPrefix(trimmed, "@") && !strings.HasPrefix(trimmed, "Copyright") {
			return trimmed
		}
	}

	return ""
}

func findStructByName(structs []*ir.StructIR, name string) *ir.StructIR {
	for _, s := range structs {
		if s.Name == name {
			return s
		}
	}

	return nil
}

func isComplexType(goType string) bool {
	switch goType {
	case "string",
		"int",
		"int64",
		"int32",
		"uint",
		"uint64",
		"uint32",
		"float64",
		"float32",
		"bool",
		"[]byte",
		"time.Time",
		"any":
		return false
	default:
		return true
	}
}

func mapGoTypeToSchema(goType string, validStructs map[string]bool) *Schema {
	cleanType := strings.TrimPrefix(goType, "*")

	if strings.HasPrefix(cleanType, "[]") {
		elemType := strings.TrimPrefix(cleanType, "[]")

		return &Schema{
			Type:  TypeArray{"array"},
			Items: mapGoTypeToSchema(elemType, validStructs),
		}
	}

	if strings.HasPrefix(cleanType, "map[") {
		return &Schema{
			Type: TypeArray{"object"},
		}
	}

	if validStructs[cleanType] {
		return &Schema{
			Ref: "#/components/schemas/" + cleanType,
		}
	}

	switch cleanType {
	case "string":
		return &Schema{Type: TypeArray{"string"}}
	case "int", "int32", "uint", "uint32":
		return &Schema{Type: TypeArray{"integer"}, Format: "int32"}
	case "int64", "uint64":
		return &Schema{Type: TypeArray{"integer"}, Format: "int64"}
	case "float32":
		return &Schema{Type: TypeArray{"number"}, Format: "float"}
	case "float64":
		return &Schema{Type: TypeArray{"number"}, Format: "double"}
	case "bool":
		return &Schema{Type: TypeArray{"boolean"}}
	case "time.Time":
		return &Schema{Type: TypeArray{"string"}, Format: "date-time"}
	case "[]byte":
		return &Schema{Type: TypeArray{"string"}, Format: "binary"}
	default:
		return &Schema{Type: TypeArray{"string"}}
	}
}
