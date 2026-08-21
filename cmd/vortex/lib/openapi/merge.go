// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package openapi

import (
	"strings"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ingest"
)

// MergeMode defines the set operation used when merging multiple OpenAPI / HAR specifications.
type MergeMode string

const (
	// MergeModeUnion includes all endpoints and schemas from all specifications (A ∪ B). (Default)
	MergeModeUnion MergeMode = "union"

	// MergeModeIntersection includes only endpoints present in ALL specifications (A ∩ B).
	MergeModeIntersection MergeMode = "intersect"

	// MergeModeDifference includes only endpoints present in the first specification and missing in others (A \ B).
	MergeModeDifference MergeMode = "diff"
)

// MergeOpenAPISpecs combines multiple OpenAPI 3.x specifications into a unified specification using Union mode.
func MergeOpenAPISpecs(specs ...*openapi3.T) *openapi3.T {
	return MergeOpenAPISpecsWithMode(MergeModeUnion, specs...)
}

// MergeOpenAPISpecsWithMode combines multiple specifications using the chosen set operation (union, intersect, diff).
func MergeOpenAPISpecsWithMode(mode MergeMode, specs ...*openapi3.T) *openapi3.T {
	if len(specs) == 0 {
		return nil
	}

	if len(specs) == 1 {
		return specs[0]
	}

	switch mode {
	case MergeModeIntersection:
		return mergeIntersection(specs...)
	case MergeModeDifference:
		return mergeDifference(specs...)
	default:
		return mergeUnion(specs...)
	}
}

func mergeUnion(specs ...*openapi3.T) *openapi3.T {
	root := specs[0]
	if root.Paths == nil {
		root.Paths = openapi3.NewPaths()
	}

	if root.Components == nil {
		root.Components = &openapi3.Components{
			Schemas: make(openapi3.Schemas),
		}
	} else if root.Components.Schemas == nil {
		root.Components.Schemas = make(openapi3.Schemas)
	}

	for _, s := range specs[1:] {
		if s == nil {
			continue
		}

		// 1. Merge Servers
		for _, srv := range s.Servers {
			hasServer := false

			for _, existing := range root.Servers {
				if existing != nil && srv != nil && existing.URL == srv.URL {
					hasServer = true
					break
				}
			}

			if !hasServer && srv != nil {
				root.Servers = append(root.Servers, srv)
			}
		}

		// 2. Merge Schemas
		if s.Components != nil {
			for name, schemaRef := range s.Components.Schemas {
				if existingSchema, exists := root.Components.Schemas[name]; exists && existingSchema.Value != nil &&
					schemaRef.Value != nil {
					existingSchema.Value = ingest.MergeSchemas(existingSchema.Value, schemaRef.Value)
				} else {
					root.Components.Schemas[name] = schemaRef
				}
			}
		}

		// 3. Merge Headers in Info.Extensions["x-vortex-headers"]
		if s.Info != nil && s.Info.Extensions != nil {
			if hRaw, ok := s.Info.Extensions["x-vortex-headers"]; ok {
				if root.Info == nil {
					root.Info = &openapi3.Info{
						Title:   "Combined API Specification",
						Version: "1.0.0",
					}
				}

				if root.Info.Extensions == nil {
					root.Info.Extensions = make(map[string]any)
				}

				existingHeaders, _ := root.Info.Extensions["x-vortex-headers"].([]map[string]string)
				headerMap := make(map[string]string)

				for _, h := range existingHeaders {
					headerMap[h["name"]] = h["value"]
				}

				if incomingList, ok := hRaw.([]map[string]string); ok {
					for _, h := range incomingList {
						if _, exists := headerMap[h["name"]]; !exists {
							headerMap[h["name"]] = h["value"]
							existingHeaders = append(existingHeaders, h)
						}
					}
				} else if incomingSlice, ok := hRaw.([]any); ok {
					for _, item := range incomingSlice {
						if hMap, isMap := item.(map[string]any); isMap {
							name, _ := hMap["name"].(string)
							val, _ := hMap["value"].(string)

							if name != "" && val != "" {
								if _, exists := headerMap[name]; !exists {
									headerMap[name] = val
									existingHeaders = append(existingHeaders, map[string]string{
										"name":  name,
										"value": val,
									})
								}
							}
						}
					}
				}

				root.Info.Extensions["x-vortex-headers"] = existingHeaders
			}
		}

		// 4. Merge Paths and Operations
		if s.Paths != nil {
			for pathStr, pathItem := range s.Paths.Map() {
				if pathItem == nil {
					continue
				}

				existingItem := root.Paths.Value(pathStr)
				if existingItem == nil {
					root.Paths.Set(pathStr, pathItem)
				} else {
					mergePathItem(existingItem, pathItem)
				}
			}
		}
	}

	return root
}

func mergeIntersection(specs ...*openapi3.T) *openapi3.T {
	root := specs[0]
	if root == nil || root.Paths == nil {
		return root
	}

	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}

	for pathStr, pathItem := range root.Paths.Map() {
		if pathItem == nil {
			continue
		}

		for _, m := range methods {
			op := getPathItemOperation(pathItem, m)
			if op == nil {
				continue
			}

			// Check if this operation exists in ALL other specs
			inAll := true
			for _, otherSpec := range specs[1:] {
				if otherSpec == nil || otherSpec.Paths == nil {
					inAll = false
					break
				}

				otherItem := otherSpec.Paths.Value(pathStr)
				if otherItem == nil || getPathItemOperation(otherItem, m) == nil {
					inAll = false
					break
				}
			}

			if inAll {
				// Merge operation metadata from other specs
				for _, otherSpec := range specs[1:] {
					otherItem := otherSpec.Paths.Value(pathStr)
					if otherItem != nil {
						if otherOp := getPathItemOperation(otherItem, m); otherOp != nil {
							mergeOperation(op, otherOp)
						}
					}

					// Merge schemas
					if otherSpec.Components != nil {
						for name, sRef := range otherSpec.Components.Schemas {
							if existing, ok := root.Components.Schemas[name]; ok && existing.Value != nil &&
								sRef.Value != nil {
								existing.Value = ingest.MergeSchemas(existing.Value, sRef.Value)
							} else {
								root.Components.Schemas[name] = sRef
							}
						}
					}
				}
			} else {
				setPathItemOperation(pathItem, m, nil)
			}
		}

		if countPathItemOperations(pathItem) == 0 {
			root.Paths.Delete(pathStr)
		}
	}

	return root
}

func mergeDifference(specs ...*openapi3.T) *openapi3.T {
	root := specs[0]
	if root == nil || root.Paths == nil {
		return root
	}

	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}

	for _, otherSpec := range specs[1:] {
		if otherSpec == nil || otherSpec.Paths == nil {
			continue
		}

		for pathStr, otherItem := range otherSpec.Paths.Map() {
			if otherItem == nil {
				continue
			}

			rootItem := root.Paths.Value(pathStr)
			if rootItem == nil {
				continue
			}

			for _, m := range methods {
				if getPathItemOperation(otherItem, m) != nil {
					setPathItemOperation(rootItem, m, nil)
				}
			}

			if countPathItemOperations(rootItem) == 0 {
				root.Paths.Delete(pathStr)
			}
		}
	}

	return root
}

func getPathItemOperation(p *openapi3.PathItem, method string) *openapi3.Operation {
	if p == nil {
		return nil
	}

	switch strings.ToUpper(method) {
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
	default:
		return nil
	}
}

func setPathItemOperation(p *openapi3.PathItem, method string, op *openapi3.Operation) {
	if p == nil {
		return
	}

	switch strings.ToUpper(method) {
	case "GET":
		p.Get = op
	case "POST":
		p.Post = op
	case "PUT":
		p.Put = op
	case "DELETE":
		p.Delete = op
	case "PATCH":
		p.Patch = op
	case "HEAD":
		p.Head = op
	case "OPTIONS":
		p.Options = op
	}
}

func countPathItemOperations(p *openapi3.PathItem) int {
	if p == nil {
		return 0
	}

	count := 0
	for _, m := range []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"} {
		if getPathItemOperation(p, m) != nil {
			count++
		}
	}

	return count
}

func mergePathItem(existing, incoming *openapi3.PathItem) {
	if incoming.Get != nil {
		if existing.Get == nil {
			existing.Get = incoming.Get
		} else {
			mergeOperation(existing.Get, incoming.Get)
		}
	}

	if incoming.Post != nil {
		if existing.Post == nil {
			existing.Post = incoming.Post
		} else {
			mergeOperation(existing.Post, incoming.Post)
		}
	}

	if incoming.Put != nil {
		if existing.Put == nil {
			existing.Put = incoming.Put
		} else {
			mergeOperation(existing.Put, incoming.Put)
		}
	}

	if incoming.Delete != nil {
		if existing.Delete == nil {
			existing.Delete = incoming.Delete
		} else {
			mergeOperation(existing.Delete, incoming.Delete)
		}
	}

	if incoming.Patch != nil {
		if existing.Patch == nil {
			existing.Patch = incoming.Patch
		} else {
			mergeOperation(existing.Patch, incoming.Patch)
		}
	}

	if incoming.Head != nil {
		if existing.Head == nil {
			existing.Head = incoming.Head
		} else {
			mergeOperation(existing.Head, incoming.Head)
		}
	}

	if incoming.Options != nil {
		if existing.Options == nil {
			existing.Options = incoming.Options
		} else {
			mergeOperation(existing.Options, incoming.Options)
		}
	}
}

func mergeOperation(existing, incoming *openapi3.Operation) {
	for _, p := range incoming.Parameters {
		if p != nil && p.Value != nil {
			if existingParam := existing.Parameters.GetByInAndName(p.Value.In, p.Value.Name); existingParam == nil {
				existing.AddParameter(p.Value)
			}
		}
	}

	if incoming.RequestBody != nil && incoming.RequestBody.Value != nil {
		if existing.RequestBody == nil || existing.RequestBody.Value == nil {
			existing.RequestBody = incoming.RequestBody
		} else {
			for mt, content := range incoming.RequestBody.Value.Content {
				existingContent := existing.RequestBody.Value.Content.Get(mt)
				if existingContent == nil {
					existing.RequestBody.Value.Content[mt] = content
				} else if existingContent.Schema != nil && content.Schema != nil && existingContent.Schema.Value != nil && content.Schema.Value != nil {
					existingContent.Schema.Value = ingest.MergeSchemas(
						existingContent.Schema.Value,
						content.Schema.Value,
					)
				}
			}
		}
	}

	if incoming.Responses != nil {
		if existing.Responses == nil {
			existing.Responses = openapi3.NewResponses()
		}

		for statusStr, respRef := range incoming.Responses.Map() {
			existingResp := existing.Responses.Value(statusStr)
			if existingResp == nil {
				existing.Responses.Set(statusStr, respRef)
			} else if existingResp.Value != nil && respRef.Value != nil {
				for mt, content := range respRef.Value.Content {
					existingContent := existingResp.Value.Content.Get(mt)
					if existingContent == nil {
						existingResp.Value.Content[mt] = content
					} else if existingContent.Schema != nil && content.Schema != nil && existingContent.Schema.Value != nil && content.Schema.Value != nil {
						existingContent.Schema.Value = ingest.MergeSchemas(
							existingContent.Schema.Value,
							content.Schema.Value,
						)
					}
				}
			}
		}
	}

	if incoming.Extensions != nil {
		if inHeadersRaw, ok := incoming.Extensions["x-vortex-headers"]; ok {
			if existing.Extensions == nil {
				existing.Extensions = make(map[string]any)
			}

			existingHeaders, _ := existing.Extensions["x-vortex-headers"].([]map[string]string)
			headerMap := make(map[string]string)

			for _, h := range existingHeaders {
				headerMap[h["name"]] = h["value"]
			}

			if inList, ok := inHeadersRaw.([]map[string]string); ok {
				for _, h := range inList {
					if _, exists := headerMap[h["name"]]; !exists {
						headerMap[h["name"]] = h["value"]
						existingHeaders = append(existingHeaders, h)
					}
				}
			}

			existing.Extensions["x-vortex-headers"] = existingHeaders
		}
	}
}
