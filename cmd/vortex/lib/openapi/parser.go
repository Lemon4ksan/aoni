// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package openapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/lemon4ksan/foundation/generic"
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

	rawNode, err := parseYAMLOrJSONNode(data)
	if err != nil {
		return nil, err
	}

	var doc Document
	if err := rawNode.Decode(&doc); err != nil {
		return nil, fmt.Errorf("failed to decode openapi document: %w", err)
	}

	extractExtensions(rawNode, &doc)

	// In-memory zero-allocation migration from Swagger 2.0 to OpenAPI 3.x
	if isSwagger2(&doc) {
		normalizeSwagger2(&doc)
	}

	ensureComponents(&doc)

	return &doc, nil
}

func parseYAMLOrJSONNode(data []byte) (*yaml.Node, error) {
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err == nil {
		return &node, nil
	}

	if err := json.Unmarshal(data, &node); err != nil {
		return nil, fmt.Errorf("failed to parse openapi specification: %w", err)
	}

	return &node, nil
}

func isSwagger2(doc *Document) bool {
	return doc.Swagger != "" || strings.HasPrefix(doc.Swagger, "2.")
}

// normalizeSwagger2 transforms legacy Swagger 2.0 AST structures into the unified OpenAPI 3.x AST.
func normalizeSwagger2(doc *Document) {
	normalizeSwaggerServers(doc)
	ensureComponents(doc)
	normalizeSwaggerDefinitions(doc)
	normalizeSwaggerParameters(doc)
	normalizeSwaggerResponses(doc)
	normalizeSwaggerSecurity(doc)
	normalizeSwaggerOperations(doc)
}

// normalizeSwaggerServers handles the Swagger 2.0 endpoint fragmentation quirk.
//
// Quirk (Swagger 2.0 §5.1): Swagger 2.0 specifies base URLs across three separate root fields:
// 'host' ("api.example.com"), 'basePath' ("/v1"), and 'schemes' (["https"]).
// OpenAPI 3.x consolidates these into the unified 'servers' array (`https://api.example.com/v1`).
func normalizeSwaggerServers(doc *Document) {
	if len(doc.Servers) > 0 || (doc.Host == "" && doc.BasePath == "") {
		return
	}

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

func ensureComponents(doc *Document) {
	if doc.Components == nil {
		doc.Components = &Components{
			Schemas:         make(map[string]*Schema),
			Responses:       make(map[string]*Response),
			Parameters:      make(map[string]*Parameter),
			SecuritySchemes: make(map[string]*SecurityScheme),
		}
		return
	}

	if doc.Components.Schemas == nil {
		doc.Components.Schemas = make(map[string]*Schema)
	}
	if doc.Components.Responses == nil {
		doc.Components.Responses = make(map[string]*Response)
	}
	if doc.Components.Parameters == nil {
		doc.Components.Parameters = make(map[string]*Parameter)
	}
	if doc.Components.SecuritySchemes == nil {
		doc.Components.SecuritySchemes = make(map[string]*SecurityScheme)
	}
}

// normalizeSwaggerDefinitions relocates models from Swagger 2.0 'definitions' to OpenAPI 3.x 'components.schemas'.
//
// Quirk (Swagger 2.0 §5.17 vs OpenAPI 3.1 §4.8.7): Models in 2.0 are stored under root 'definitions'
// and referenced via '#/definitions/{Model}'. OpenAPI 3.x requires '#/components/schemas/{Model}'.
func normalizeSwaggerDefinitions(doc *Document) {
	for name, schema := range doc.Definitions {
		if schema == nil {
			continue
		}
		normalizeSchemaRefs(schema)
		doc.Components.Schemas[name] = schema
	}
}

// normalizeSwaggerParameters migrates top-level reusable parameters into 'components.parameters'.
func normalizeSwaggerParameters(doc *Document) {
	for name, param := range doc.Parameters {
		if param == nil {
			continue
		}
		normalizeSchemaRefs(param.Schema)
		doc.Components.Parameters[name] = param
	}
}

// normalizeSwaggerResponses upgrades legacy 2.0 response schema wrappers into 3.x media type containers.
//
// Quirk (Swagger 2.0 §5.12 vs OpenAPI 3.1 §4.8.17): In Swagger 2.0, responses define schemas directly
// via `response.schema`. In OpenAPI 3.x, responses encapsulate payloads in `response.content["application/json"].schema`.
func normalizeSwaggerResponses(doc *Document) {
	for name, resp := range doc.Responses {
		if resp == nil {
			continue
		}
		if resp.Schema != nil && len(resp.Content) == 0 {
			normalizeSchemaRefs(resp.Schema)
			resp.Content = map[string]*MediaType{
				"application/json": {Schema: resp.Schema},
			}
		}
		doc.Components.Responses[name] = resp
	}
}

// normalizeSwaggerSecurity maps legacy OAuth2 flow names to OpenAPI 3.x OAuthFlows.
//
// Quirk (Swagger 2.0 §5.23 vs OpenAPI 3.1 §4.8.27): OAuth2 flow identifiers were changed:
//   - 'accessCode'  -> 'authorizationCode'
//   - 'application' -> 'clientCredentials'
func normalizeSwaggerSecurity(doc *Document) {
	for name, sec := range doc.SecurityDefinitions {
		if sec == nil {
			continue
		}
		if sec.Type == "oauth2" && sec.Flow != "" && sec.Flows == nil {
			sec.Flows = mapSwaggerOAuthFlow(sec)
		}
		doc.Components.SecuritySchemes[name] = sec
	}
}

func mapSwaggerOAuthFlow(sec *SecurityScheme) *OAuthFlows {
	flow := &OAuthFlow{
		AuthorizationURL: sec.AuthorizationURL,
		TokenURL:         sec.TokenURL,
		Scopes:           sec.Scopes,
	}

	flows := &OAuthFlows{}
	switch sec.Flow {
	case "implicit":
		flows.Implicit = flow
	case "password":
		flows.Password = flow
	case "application":
		flows.ClientCredentials = flow
	case "accessCode":
		flows.AuthorizationCode = flow
	}

	return flows
}

func normalizeSwaggerOperations(doc *Document) {
	for _, pathItem := range doc.Paths {
		if pathItem == nil {
			continue
		}
		for _, op := range pathItem.OperationsMap() {
			if op == nil {
				continue
			}
			normalizeOperationParameters(op)
			normalizeOperationResponses(op)
		}
	}
}

// normalizeOperationParameters converts legacy `in: body` parameters to OpenAPI 3.x `RequestBody`.
//
// Quirk (Swagger 2.0 §5.9 vs OpenAPI 3.1 §4.8.13): Swagger 2.0 lacks a dedicated RequestBody entity,
// placing payload schemas inside the parameter list with `in: "body"`. We synthesize a canonical
// OpenAPI 3.x RequestBody with `application/json` content type and prune `in: body` from parameter list.
func normalizeOperationParameters(op *Operation) {
	for _, p := range op.Parameters {
		if p == nil {
			continue
		}

		if p.In == "body" {
			if op.RequestBody == nil {
				normalizeSchemaRefs(p.Schema)
				op.RequestBody = &RequestBody{
					Description: p.Description,
					Required:    p.Required,
					Content: map[string]*MediaType{
						"application/json": {Schema: p.Schema},
					},
				}
			}
			continue
		}

		// Quirk (Swagger 2.0 §5.9): In Swagger 2.0, primitive parameters use 'type' directly on Parameter
		// rather than nested Schema. In OpenAPI 3, parameters always contain a Schema object.
		if p.Schema == nil && p.Type != "" {
			p.Schema = &Schema{
				Type:   TypeArray{p.Type},
				Format: p.Format,
				Items:  p.Items,
			}
		}
		normalizeSchemaRefs(p.Schema)
	}

	op.Parameters = generic.Filter(op.Parameters, func(p *Parameter) bool {
		return p != nil && p.In != "body"
	})
}

func normalizeOperationResponses(op *Operation) {
	for _, resp := range op.Responses {
		if resp == nil {
			continue
		}

		if resp.Schema != nil && len(resp.Content) == 0 {
			normalizeSchemaRefs(resp.Schema)
			resp.Content = map[string]*MediaType{
				"application/json": {Schema: resp.Schema},
			}
		}

		for _, media := range resp.Content {
			if media != nil {
				normalizeSchemaRefs(media.Schema)
			}
		}
	}
}

// normalizeSchemaRefs recursively rewrites '#/definitions/' references to '#/components/schemas/'.
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
	mapNode := unwrapMappingNode(root)
	if mapNode == nil {
		return
	}

	for i := 0; i < len(mapNode.Content)-1; i += 2 {
		key := mapNode.Content[i].Value
		valNode := mapNode.Content[i+1]

		switch key {
		case "info":
			if doc.Info != nil && valNode.Kind == yaml.MappingNode {
				doc.Info.Extensions = extractMapExtensions(valNode)
			}
		case "paths":
			if doc.Paths != nil && valNode.Kind == yaml.MappingNode {
				extractPathsExtensions(valNode, doc.Paths)
			}
		}
	}
}

func unwrapMappingNode(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	return node
}

func extractMapExtensions(mapNode *yaml.Node) map[string]any {
	exts := make(map[string]any)
	for i := 0; i < len(mapNode.Content)-1; i += 2 {
		k := mapNode.Content[i].Value
		if !strings.HasPrefix(k, "x-") {
			continue
		}
		var val any
		if err := mapNode.Content[i+1].Decode(&val); err == nil {
			exts[k] = val
		}
	}
	return exts
}

func extractPathsExtensions(pathsNode *yaml.Node, paths map[string]*PathItem) {
	for i := 0; i < len(pathsNode.Content)-1; i += 2 {
		pathStr := pathsNode.Content[i].Value
		pathItemNode := pathsNode.Content[i+1]

		pathItem := paths[pathStr]
		if pathItem == nil || pathItemNode.Kind != yaml.MappingNode {
			continue
		}

		extractOperationExtensions(pathItemNode, pathItem)
	}
}

func extractOperationExtensions(pathItemNode *yaml.Node, pathItem *PathItem) {
	for j := 0; j < len(pathItemNode.Content)-1; j += 2 {
		method := strings.ToUpper(pathItemNode.Content[j].Value)
		opNode := pathItemNode.Content[j+1]

		op := getPathItemOp(pathItem, method)
		if op != nil && opNode.Kind == yaml.MappingNode {
			op.Extensions = extractMapExtensions(opNode)
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
