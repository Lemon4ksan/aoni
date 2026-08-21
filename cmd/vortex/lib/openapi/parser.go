// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package openapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseSpec parses an OpenAPI 3.1, 3.0, or Swagger 2.0 document from YAML or JSON bytes into a normalized Document AST.
//
// References:
//   - OpenAPI 3.1.0 Specification: https://spec.openapis.org/oas/v3.1.0
//   - Swagger 2.0 Specification: https://swagger.io/specification/v2/
func ParseSpec(data []byte) (*Document, error) {
	if len(data) == 0 {
		return nil, errors.New("empty openapi specification data")
	}

	var rawNode yaml.Node
	if err := yaml.Unmarshal(data, &rawNode); err != nil {
		if errJSON := json.Unmarshal(data, &rawNode); errJSON != nil {
			return nil, fmt.Errorf("failed to parse openapi specification: %w", err)
		}
	}

	var doc Document
	if err := rawNode.Decode(&doc); err != nil {
		return nil, fmt.Errorf("failed to decode openapi document: %w", err)
	}

	// Extract extensions from raw node
	extractExtensions(&rawNode, &doc)

	// Normalize Swagger 2.0 into OpenAPI 3.x in-memory representation
	if doc.Swagger != "" || strings.HasPrefix(doc.Swagger, "2.") {
		normalizeSwagger2(&doc)
	}

	// Normalize Components
	if doc.Components == nil {
		doc.Components = &Components{
			Schemas: make(map[string]*Schema),
		}
	} else if doc.Components.Schemas == nil {
		doc.Components.Schemas = make(map[string]*Schema)
	}

	return &doc, nil
}

func normalizeSwagger2(doc *Document) {
	// 1. Convert Host + BasePath + Schemes -> Servers
	if len(doc.Servers) == 0 && (doc.Host != "" || doc.BasePath != "") {
		scheme := "https"
		if len(doc.Schemes) > 0 && doc.Schemes[0] != "" {
			scheme = doc.Schemes[0]
		}

		serverURL := ""
		if doc.Host != "" {
			serverURL = scheme + "://" + doc.Host
		}
		if doc.BasePath != "" {
			serverURL = strings.TrimSuffix(serverURL, "/") + "/" + strings.TrimPrefix(doc.BasePath, "/")
		}

		if serverURL != "" {
			doc.Servers = []Server{{URL: serverURL}}
		}
	}

	// 2. Convert Definitions -> Components.Schemas
	if doc.Components == nil {
		doc.Components = &Components{
			Schemas: make(map[string]*Schema),
		}
	}

	if len(doc.Definitions) > 0 {
		for name, schema := range doc.Definitions {
			if schema != nil {
				normalizeSchemaRefs(schema)
				doc.Components.Schemas[name] = schema
			}
		}
	}

	// 3. Convert Operations parameters (in: body -> RequestBody)
	for _, pathItem := range doc.Paths {
		if pathItem == nil {
			continue
		}

		for _, op := range pathItem.OperationsMap() {
			if op == nil {
				continue
			}

			var nonBodyParams []*Parameter
			for _, p := range op.Parameters {
				if p == nil {
					continue
				}

				if p.In == "body" {
					if op.RequestBody == nil {
						op.RequestBody = &RequestBody{
							Description: p.Description,
							Required:    p.Required,
							Content: map[string]*MediaType{
								"application/json": {
									Schema: p.Schema,
								},
							},
						}
					}
				} else {
					normalizeSchemaRefs(p.Schema)
					nonBodyParams = append(nonBodyParams, p)
				}
			}
			op.Parameters = nonBodyParams

			// Normalize responses
			for _, resp := range op.Responses {
				if resp != nil {
					for _, media := range resp.Content {
						if media != nil {
							normalizeSchemaRefs(media.Schema)
						}
					}
				}
			}
		}
	}
}

func normalizeSchemaRefs(s *Schema) {
	if s == nil {
		return
	}

	if strings.HasPrefix(s.Ref, "#/definitions/") {
		s.Ref = "#/components/schemas/" + strings.TrimPrefix(s.Ref, "#/definitions/")
	}

	for _, prop := range s.Properties {
		normalizeSchemaRefs(prop)
	}

	normalizeSchemaRefs(s.Items)

	for _, sub := range s.AllOf {
		normalizeSchemaRefs(sub)
	}
	for _, sub := range s.OneOf {
		normalizeSchemaRefs(sub)
	}
	for _, sub := range s.AnyOf {
		normalizeSchemaRefs(sub)
	}
}

func extractExtensions(root *yaml.Node, doc *Document) {
	if root == nil || root.Kind != yaml.DocumentNode && root.Kind != yaml.MappingNode {
		return
	}

	mapNode := root
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		mapNode = root.Content[0]
	}

	if mapNode.Kind != yaml.MappingNode {
		return
	}

	for i := 0; i < len(mapNode.Content)-1; i += 2 {
		key := mapNode.Content[i].Value
		valNode := mapNode.Content[i+1]

		if key == "info" && doc.Info != nil && valNode.Kind == yaml.MappingNode {
			doc.Info.Extensions = extractMapExtensions(valNode)
		}

		if key == "paths" && doc.Paths != nil && valNode.Kind == yaml.MappingNode {
			extractPathsExtensions(valNode, doc.Paths)
		}
	}
}

func extractMapExtensions(mapNode *yaml.Node) map[string]any {
	exts := make(map[string]any)
	for i := 0; i < len(mapNode.Content)-1; i += 2 {
		k := mapNode.Content[i].Value
		if strings.HasPrefix(k, "x-") {
			var val any
			if err := mapNode.Content[i+1].Decode(&val); err == nil {
				exts[k] = val
			}
		}
	}
	return exts
}

func extractPathsExtensions(pathsNode *yaml.Node, paths map[string]*PathItem) {
	for i := 0; i < len(pathsNode.Content)-1; i += 2 {
		pathStr := pathsNode.Content[i].Value
		pathItemNode := pathsNode.Content[i+1]

		pathItem, exists := paths[pathStr]
		if !exists || pathItem == nil || pathItemNode.Kind != yaml.MappingNode {
			continue
		}

		for j := 0; j < len(pathItemNode.Content)-1; j += 2 {
			method := strings.ToUpper(pathItemNode.Content[j].Value)
			opNode := pathItemNode.Content[j+1]

			op := getPathItemOp(pathItem, method)
			if op != nil && opNode.Kind == yaml.MappingNode {
				op.Extensions = extractMapExtensions(opNode)
			}
		}
	}
}

func getPathItemOp(p *PathItem, method string) *Operation {
	if p == nil {
		return nil
	}
	switch method {
	case "GET":
		return p.Get
	case "POST":
		return p.Post
	case "PUT":
		return p.Put
	case "DELETE":
		return p.Delete
	case "PATCH":
		return p.Patch
	case "HEAD":
		return p.Head
	case "OPTIONS":
		return p.Options
	case "TRACE":
		return p.Trace
	default:
		return nil
	}
}
