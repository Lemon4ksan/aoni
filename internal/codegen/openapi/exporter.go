// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package openapi

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"gopkg.in/yaml.v3"

	"github.com/lemon4ksan/aoni/internal/codegen/ir"
)

// ExportConfig controls OpenAPI 3.1 specification generation from aoni IR.
type ExportConfig struct {
	Title       string
	Version     string
	Description string
	BaseURL     string
	AsYAML      bool
}

// ExportOpenAPI generates a standard OpenAPI 3.1 JSON or YAML document from aoni RootIR.
func ExportOpenAPI(root *ir.RootIR, cfg ExportConfig) ([]byte, error) {
	if root == nil {
		return nil, errors.New("root IR cannot be nil")
	}

	title := cfg.Title
	if title == "" {
		if len(root.Services) > 0 && root.Services[0].Name != "" {
			title = root.Services[0].Name + " API"
		} else {
			title = root.PackageName + " API"
		}
	}

	version := cfg.Version
	if version == "" {
		version = "1.0.0"
	}

	doc := &openapi3.T{
		OpenAPI: "3.1.0",
		Info: &openapi3.Info{
			Title:       title,
			Version:     version,
			Description: cfg.Description,
		},
		Paths: openapi3.NewPaths(),
		Components: &openapi3.Components{
			Schemas: make(openapi3.Schemas),
		},
	}

	// Servers
	baseURL := cfg.BaseURL
	if baseURL == "" && len(root.Services) > 0 {
		baseURL = root.Services[0].BaseURL
	}

	if baseURL != "" {
		doc.Servers = append(doc.Servers, &openapi3.Server{
			URL:         baseURL,
			Description: "Default API server",
		})
	}

	// Schemas / Models
	for _, s := range root.Structs {
		schema := openapi3.NewObjectSchema()
		if len(s.Doc) > 0 {
			schema.Description = strings.Join(s.Doc, " ")
		}

		for _, f := range s.Fields {
			wireKey := f.WireName
			if wireKey == "" {
				wireKey = f.GoName
			}

			wireKey = strings.TrimSuffix(wireKey, ",omitempty")

			fSchema := mapGoTypeToSchema(f.Type.Name)
			schema.WithPropertyRef(wireKey, fSchema)
		}

		doc.Components.Schemas[s.Name] = &openapi3.SchemaRef{Value: schema}
	}

	// Services and Operations
	for _, svc := range root.Services {
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

			op.OperationID = m.Name
			if len(m.Doc) > 0 {
				op.Summary = strings.Join(m.Doc, " ")
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
						Value: openapi3.NewRequestBody().WithJSONSchemaRef(mapGoTypeToSchema(typeName)),
					}

					continue
				}

				if m.PayloadKind == ir.PayloadForm &&
					(param.Location == ir.LocBody || param.Location == ir.LocFormFields || isComplexType(typeName)) {
					op.RequestBody = &openapi3.RequestBodyRef{
						Value: openapi3.NewRequestBody().WithFormDataSchemaRef(mapGoTypeToSchema(typeName)),
					}

					continue
				}

				// Query Parameter
				qName := param.WireKey
				if qName == "" {
					qName = param.GoName
				}

				q := openapi3.NewQueryParameter(qName)
				q.Schema = mapGoTypeToSchema(typeName)
				op.AddParameter(q)
			}

			// Responses
			resp := openapi3.NewResponse().WithDescription("Successful operation")
			if m.Return != nil && !m.Return.IsVoid && m.Return.SuccessType.Name != "" &&
				m.Return.SuccessType.Name != "error" {
				returnTypeName := strings.TrimPrefix(m.Return.SuccessType.Name, "*")
				resp.WithJSONSchemaRef(mapGoTypeToSchema(returnTypeName))
			}

			op.AddResponse(200, resp)

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

func mapGoTypeToSchema(goType string) *openapi3.SchemaRef {
	clean := strings.TrimPrefix(goType, "*")

	if strings.HasPrefix(clean, "[]") {
		elemType := strings.TrimPrefix(clean, "[]")
		arrSchema := openapi3.NewArraySchema()
		arrSchema.Items = mapGoTypeToSchema(elemType)

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
		// Reference to struct component
		return &openapi3.SchemaRef{
			Ref: "#/components/schemas/" + clean,
		}
	}
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
