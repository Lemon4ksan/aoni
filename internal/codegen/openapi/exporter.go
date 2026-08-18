// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package openapi

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/lemon4ksan/foundation/generic"
	"gopkg.in/yaml.v3"

	"github.com/lemon4ksan/aoni/internal/codegen/ir"
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
func ExportOpenAPI(root *ir.RootIR, cfg ExportConfig) ([]byte, error) {
	if root == nil {
		return nil, errors.New("root IR cannot be nil")
	}

	services := root.Services
	if cfg.ServiceName != "" {
		filtered := generic.Filter(root.Services, func(s *ir.ServiceIR) bool {
			return strings.EqualFold(s.Name, cfg.ServiceName)
		})

		if len(filtered) > 0 {
			services = filtered
		}
	}

	serviceName := ""
	if len(services) > 0 {
		serviceName = services[0].Name
	}

	title := generic.Coalesce(cfg.Title, cleanAPITitle(serviceName, root.PackageName))

	defaultVersion := "1.0.0"
	if len(services) > 0 && services[0].Version != "" {
		defaultVersion = services[0].Version
	}

	version := generic.Coalesce(cfg.Version, defaultVersion)

	defaultDesc := ""
	if len(services) > 0 && services[0].Description != "" {
		defaultDesc = services[0].Description
	}

	description := generic.Coalesce(cfg.Description, defaultDesc)

	doc := &openapi3.T{
		OpenAPI: "3.1.0",
		Info: &openapi3.Info{
			Title:       title,
			Version:     version,
			Description: description,
		},
		Paths: openapi3.NewPaths(),
		Components: &openapi3.Components{
			Schemas: make(openapi3.Schemas),
		},
	}

	if len(services) > 0 && services[0].Summary != "" {
		doc.Info.Summary = services[0].Summary
	}

	// Servers
	baseURL := cfg.BaseURL
	if baseURL == "" && len(services) > 0 {
		baseURL = services[0].BaseURL
	}

	if baseURL != "" {
		doc.Servers = append(doc.Servers, &openapi3.Server{
			URL:         baseURL,
			Description: "Default API server",
		})
	}

	// Vendor extensions are only emitted if explicitly enabled (--vortex / cfg.Vortex)
	if cfg.Vortex && len(services) > 0 {
		firstSvc := services[0]
		if firstSvc.Persona != "" {
			doc.Info.Extensions = setExtension(doc.Info.Extensions, "x-vortex-persona", firstSvc.Persona)
		}

		if firstSvc.TLSSpec != "" {
			doc.Info.Extensions = setExtension(doc.Info.Extensions, "x-vortex-tlsspec", firstSvc.TLSSpec)
		}

		if firstSvc.DefaultCasing != "" {
			doc.Info.Extensions = setExtension(doc.Info.Extensions, "x-vortex-casing", string(firstSvc.DefaultCasing))
		}

		if firstSvc.Engine != "" {
			doc.Info.Extensions = setExtension(doc.Info.Extensions, "x-vortex-engine", string(firstSvc.Engine))
		}

		if firstSvc.Source != "" {
			doc.Info.Extensions = setExtension(doc.Info.Extensions, "x-vortex-source", firstSvc.Source)
		}
	}

	// Schemas / Models
	validStructs := make(map[string]bool)
	for _, s := range root.Structs {
		if !unicode.IsUpper(rune(s.Name[0])) {
			continue
		}

		isInternal := false
		for _, f := range s.Fields {
			if strings.Contains(f.Type.Name, "sync.") || strings.Contains(f.Type.Name, "chan ") ||
				f.Type.Name == "event.Bus" || f.Type.Name == "bus.Bus" || f.Type.Name == "log.Logger" || f.Type.Name == "aoni.WebSocketDialer" {
				isInternal = true
				break
			}
		}

		if !isInternal {
			validStructs[s.Name] = true
		}
	}

	// Add standard Error and RateLimit schemas
	validStructs["ErrorResponse"] = true
	validStructs["RateLimitError"] = true

	for sName := range validStructs {
		s := findStructByName(root.Structs, sName)
		if s == nil {
			continue
		}

		schema := openapi3.NewObjectSchema()
		if s.Description != "" {
			schema.Description = s.Description
		} else if desc := cleanDocSummary(s.Doc); desc != "" {
			schema.Description = desc
		}

		if s.Deprecation != nil {
			schema.Deprecated = true
		}

		for _, f := range s.Fields {
			wireKey := f.WireName
			if wireKey == "" {
				wireKey = f.GoName
			}

			wireKey = strings.TrimSuffix(wireKey, ",omitempty")

			fSchema := mapGoTypeToSchema(f.Type.Name, validStructs)
			schema.WithPropertyRef(wireKey, fSchema)
		}

		doc.Components.Schemas[s.Name] = &openapi3.SchemaRef{Value: schema}
	}

	if _, ok := doc.Components.Schemas["ErrorResponse"]; !ok {
		errSchema := openapi3.NewObjectSchema()
		errSchema.WithProperty("error", openapi3.NewStringSchema())
		errSchema.WithProperty("message", openapi3.NewStringSchema())
		doc.Components.Schemas["ErrorResponse"] = &openapi3.SchemaRef{Value: errSchema}
	}

	if _, ok := doc.Components.Schemas["RateLimitError"]; !ok {
		rlSchema := openapi3.NewObjectSchema()
		rlSchema.WithProperty("error", openapi3.NewStringSchema())
		rlSchema.WithProperty("retryAfter", openapi3.NewInt32Schema())
		doc.Components.Schemas["RateLimitError"] = &openapi3.SchemaRef{Value: rlSchema}
	}

	// Services and Operations
	for _, svc := range services {
		for _, m := range svc.Methods {
			if m.Operation != ir.OpHTTP && m.HTTPMethod == "" {
				continue
			}

			rawPath := "/"
			if m.Path != nil && m.Path.RawTemplate != "" {
				rawPath = m.Path.RawTemplate
				if !strings.HasPrefix(rawPath, "/") {
					rawPath = "/" + rawPath
				}
			}

			pathItem := doc.Paths.Find(rawPath)
			if pathItem == nil {
				pathItem = &openapi3.PathItem{}
				doc.Paths.Set(rawPath, pathItem)
			}

			op := openapi3.NewOperation()

			if m.OperationID != "" {
				op.OperationID = m.OperationID
			} else {
				op.OperationID = m.Name
			}

			if m.Summary != "" {
				op.Summary = m.Summary
			} else if summary := cleanDocSummary(m.Doc); summary != "" {
				op.Summary = summary
			}

			if m.Description != "" {
				op.Description = m.Description
			}

			switch {
			case len(m.Tags) > 0:
				op.Tags = m.Tags
			case len(svc.Tags) > 0:
				op.Tags = svc.Tags
			default:
				op.Tags = []string{svc.Name}
			}

			if m.Deprecation != nil {
				op.Deprecated = true
				if m.Deprecation.Reason != "" && op.Description == "" {
					op.Description = "Deprecated: " + m.Deprecation.Reason
				}
			}

			// Vendor extensions are only emitted when --vortex is requested
			if cfg.Vortex {
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

			// Path Parameters
			pathVars := make(map[string]bool)
			if m.Path != nil {
				for _, seg := range m.Path.Segments {
					if seg.IsVariable {
						pathVars[seg.VarName] = true
						pathVars[strings.ToLower(seg.VarName)] = true

						p := openapi3.NewPathParameter(seg.VarName)
						p.Schema = openapi3.NewStringSchema().NewRef()
						p.Required = true
						op.AddParameter(p)
					}
				}
			}

			// Query / Header / Body parameters
			for _, param := range m.Params {
				if param.Location == ir.LocContext || param.Location == ir.LocModifiers || param.GoName == "ctx" ||
					param.GoName == "mods" {
					continue
				}

				if pathVars[param.GoName] || pathVars[strings.ToLower(param.GoName)] || param.Location == ir.LocPath {
					continue
				}

				typeName := param.GoType.Name
				if param.GoType.IsPointer {
					typeName = strings.TrimPrefix(typeName, "*")
				}

				if m.PayloadKind == ir.PayloadJSON && (param.Location == ir.LocBody || isComplexType(typeName)) {
					op.RequestBody = &openapi3.RequestBodyRef{
						Value: openapi3.NewRequestBody().WithJSONSchemaRef(mapGoTypeToSchema(typeName, validStructs)),
					}

					continue
				}

				if m.PayloadKind == ir.PayloadForm &&
					(param.Location == ir.LocBody || param.Location == ir.LocFormFields || isComplexType(typeName)) {
					op.RequestBody = &openapi3.RequestBodyRef{
						Value: openapi3.NewRequestBody().
							WithFormDataSchemaRef(mapGoTypeToSchema(typeName, validStructs)),
					}

					continue
				}

				// Query Parameter
				qName := param.WireKey
				if qName == "" {
					qName = param.GoName
				}

				q := openapi3.NewQueryParameter(qName)
				q.Schema = mapGoTypeToSchema(typeName, validStructs)

				// Smart parameter defaults & limits
				switch qName {
				case "limit":
					if q.Schema != nil && q.Schema.Value != nil {
						q.Schema.Value.Default = 10
						minVal := float64(1)
						q.Schema.Value.Min = &minVal
					}

				case "offset":
					if q.Schema != nil && q.Schema.Value != nil {
						q.Schema.Value.Default = 0
						minVal := float64(0)
						q.Schema.Value.Min = &minVal
					}

				case "header":
					if q.Schema != nil && q.Schema.Value != nil {
						q.Schema.Value.Default = true
					}
				case "height":
					if q.Schema != nil && q.Schema.Value != nil {
						q.Schema.Value.Default = 500
					}
				case "width":
					if q.Schema != nil && q.Schema.Value != nil {
						q.Schema.Value.Default = "100%"
					}
				}

				op.AddParameter(q)
			}

			// Responses
			resp := openapi3.NewResponse().WithDescription("Successful operation")
			if m.Return != nil && !m.Return.IsVoid && m.Return.SuccessType.Name != "" &&
				m.Return.SuccessType.Name != "error" {
				returnTypeName := strings.TrimPrefix(m.Return.SuccessType.Name, "*")

				switch {
				case m.Return.IsDirectBytes || returnTypeName == "[]byte" || strings.Contains(strings.ToLower(m.Name), "image"):
					binSchema := openapi3.NewStringSchema()
					binSchema.Format = "binary"
					resp.WithContent(
						openapi3.NewContentWithSchema(binSchema, []string{"image/png", "application/octet-stream"}),
					)

				case strings.Contains(strings.ToLower(m.Name), "graph") || returnTypeName == "html":
					resp.WithContent(openapi3.NewContentWithSchema(openapi3.NewStringSchema(), []string{"text/html"}))

				default:
					resp.WithJSONSchemaRef(mapGoTypeToSchema(returnTypeName, validStructs))
				}
			}

			op.AddResponse(200, resp)

			// Standard 400 Bad Request
			errResp400 := openapi3.NewResponse().WithDescription("Invalid input or malformed request")
			errResp400.WithJSONSchemaRef(&openapi3.SchemaRef{Ref: "#/components/schemas/ErrorResponse"})
			op.AddResponse(400, errResp400)

			// If path has parameters (e.g. {sku}, {id}, {name}), add 404 Not Found
			if len(pathVars) > 0 {
				errResp404 := openapi3.NewResponse().WithDescription("Resource or item not found")
				errResp404.WithJSONSchemaRef(&openapi3.SchemaRef{Ref: "#/components/schemas/ErrorResponse"})
				op.AddResponse(404, errResp404)
			}

			// Standard 429 Rate Limit Exceeded
			errResp429 := openapi3.NewResponse().WithDescription("Rate limit exceeded")
			errResp429.WithJSONSchemaRef(&openapi3.SchemaRef{Ref: "#/components/schemas/RateLimitError"})
			op.AddResponse(429, errResp429)

			// Assign to PathItem
			method := strings.ToUpper(m.HTTPMethod)
			if method == "" {
				method = "GET"
			}

			switch method {
			case "GET":
				pathItem.Get = op
			case "POST":
				pathItem.Post = op
			case "PUT":
				pathItem.Put = op
			case "DELETE":
				pathItem.Delete = op
			case "PATCH":
				pathItem.Patch = op
			case "HEAD":
				pathItem.Head = op
			case "OPTIONS":
				pathItem.Options = op
			default:
				pathItem.Get = op
			}
		}
	}

	if cfg.AsYAML {
		return yaml.Marshal(doc)
	}

	return json.MarshalIndent(doc, "", "  ")
}

func mapGoTypeToSchema(goType string, validStructs map[string]bool) *openapi3.SchemaRef {
	clean := strings.TrimPrefix(goType, "*")

	if strings.HasPrefix(clean, "[]") {
		elemType := strings.TrimPrefix(clean, "[]")
		arrSchema := openapi3.NewArraySchema()
		arrSchema.Items = mapGoTypeToSchema(elemType, validStructs)

		return &openapi3.SchemaRef{
			Value: arrSchema,
		}
	}

	if strings.HasPrefix(clean, "map[") {
		return &openapi3.SchemaRef{
			Value: openapi3.NewObjectSchema(),
		}
	}

	switch clean {
	case "string":
		return openapi3.NewStringSchema().NewRef()
	case "int", "int32", "int16", "int8":
		return openapi3.NewInt32Schema().NewRef()
	case "int64", "uint64", "uint", "uint32":
		return openapi3.NewInt64Schema().NewRef()
	case "float32", "float64":
		return openapi3.NewFloat64Schema().NewRef()
	case "bool":
		return openapi3.NewBoolSchema().NewRef()
	case "any", "interface{}":
		return &openapi3.SchemaRef{Value: openapi3.NewObjectSchema()}
	default:
		if validStructs != nil && validStructs[clean] {
			return &openapi3.SchemaRef{
				Ref: "#/components/schemas/" + clean,
			}
		}

		return &openapi3.SchemaRef{Value: openapi3.NewObjectSchema()}
	}
}

func findStructByName(structs []*ir.StructIR, name string) *ir.StructIR {
	for _, s := range structs {
		if s.Name == name {
			return s
		}
	}

	return nil
}

func isComplexType(t string) bool {
	clean := strings.TrimPrefix(t, "*")
	if strings.HasPrefix(clean, "[]") || strings.HasPrefix(clean, "map[") {
		return true
	}

	switch clean {
	case "string", "int", "int64", "uint64", "uint32", "int32", "float64", "float32", "bool", "any":
		return false
	default:
		return true
	}
}

func cleanDocSummary(docLines []string) string {
	var clean []string
	for _, l := range docLines {
		t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "//"))
		if strings.HasPrefix(t, "@") {
			continue
		}

		if t != "" {
			clean = append(clean, t)
		}
	}

	return strings.Join(clean, " ")
}

func setExtension(extMap map[string]any, key string, val any) map[string]any {
	if extMap == nil {
		extMap = make(map[string]any)
	}

	extMap[key] = val

	return extMap
}

func cleanAPITitle(name, pkgName string) string {
	name = strings.TrimSpace(name)
	pkgName = strings.TrimSpace(pkgName)

	if name == "" || strings.EqualFold(name, "api") {
		if pkgName != "" && !strings.EqualFold(pkgName, "api") && !strings.EqualFold(pkgName, "main") {
			return strings.ToUpper(pkgName[:1]) + pkgName[1:] + " API"
		}

		return "API Specification"
	}

	if strings.HasSuffix(strings.ToUpper(name), "API") {
		if strings.HasSuffix(name, "API") && len(name) > 3 && !strings.HasSuffix(name, " API") {
			return name[:len(name)-3] + " API"
		}

		return name
	}

	return name + " API"
}
