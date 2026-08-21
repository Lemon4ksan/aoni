// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package openapi

import (
	"maps"
	"slices"
	"strings"

	"github.com/lemon4ksan/foundation/generic"
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

// MergeOpenAPISpecs combines multiple OpenAPI specifications into a unified specification using Union mode.
func MergeOpenAPISpecs(specs ...*Document) *Document {
	return MergeOpenAPISpecsWithMode(MergeModeUnion, specs...)
}

// MergeOpenAPISpecsWithMode combines multiple specifications using the chosen set operation (union, intersect, diff).
func MergeOpenAPISpecsWithMode(mode MergeMode, specs ...*Document) *Document {
	validSpecs := generic.Filter(specs, func(d *Document) bool {
		return d != nil
	})

	if len(validSpecs) == 0 {
		return nil
	}

	if len(validSpecs) == 1 {
		return cloneDocument(validSpecs[0])
	}

	switch mode {
	case MergeModeIntersection:
		return mergeIntersection(validSpecs...)
	case MergeModeDifference:
		return mergeDifference(validSpecs...)
	default:
		return mergeUnion(validSpecs...)
	}
}

func mergeUnion(specs ...*Document) *Document {
	root := cloneDocument(specs[0])
	if root.Paths == nil {
		root.Paths = make(map[string]*PathItem)
	}

	if root.Components == nil {
		root.Components = &Components{
			Schemas: make(map[string]*Schema),
		}
	} else if root.Components.Schemas == nil {
		root.Components.Schemas = make(map[string]*Schema)
	}

	for _, s := range specs[1:] {
		if s == nil {
			continue
		}

		// 1. Merge Servers
		for _, srv := range s.Servers {
			hasServer := false
			for _, existing := range root.Servers {
				if existing.URL == srv.URL {
					hasServer = true
					break
				}
			}

			if !hasServer && srv.URL != "" {
				root.Servers = append(root.Servers, srv)
			}
		}

		// 2. Merge Schemas
		if s.Components != nil && s.Components.Schemas != nil {
			for name, schema := range s.Components.Schemas {
				if existingSchema, exists := root.Components.Schemas[name]; exists && existingSchema != nil && schema != nil {
					mergeSchema(existingSchema, schema)
				} else {
					root.Components.Schemas[name] = schema
				}
			}
		}

		// 3. Merge Headers in Info.Extensions["x-vortex-headers"]
		if s.Info != nil && s.Info.Extensions != nil {
			if hRaw, ok := s.Info.Extensions["x-vortex-headers"]; ok {
				if root.Info == nil {
					root.Info = &Info{
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
			for pathStr, pathItem := range s.Paths {
				if pathItem == nil {
					continue
				}

				existingItem := root.Paths[pathStr]
				if existingItem == nil {
					root.Paths[pathStr] = pathItem
				} else {
					mergePathItem(existingItem, pathItem)
				}
			}
		}
	}

	return root
}

func mergeIntersection(specs ...*Document) *Document {
	root := cloneDocument(specs[0])
	if root == nil || root.Paths == nil {
		return root
	}

	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}

	for pathStr, pathItem := range root.Paths {
		if pathItem == nil {
			continue
		}

		for _, m := range methods {
			op := getPathItemOp(pathItem, m)
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

				otherItem := otherSpec.Paths[pathStr]
				if otherItem == nil || getPathItemOp(otherItem, m) == nil {
					inAll = false
					break
				}
			}

			if inAll {
				// Merge operation metadata from other specs
				for _, otherSpec := range specs[1:] {
					otherItem := otherSpec.Paths[pathStr]
					if otherItem != nil {
						if otherOp := getPathItemOp(otherItem, m); otherOp != nil {
							mergeOperation(op, otherOp)
						}
					}

					// Merge schemas
					if otherSpec.Components != nil && otherSpec.Components.Schemas != nil {
						for name, s := range otherSpec.Components.Schemas {
							if existing, ok := root.Components.Schemas[name]; ok && existing != nil && s != nil {
								mergeSchema(existing, s)
							} else {
								root.Components.Schemas[name] = s
							}
						}
					}
				}
			} else {
				setPathItemOp(pathItem, m, nil)
			}
		}

		if countPathItemOps(pathItem) == 0 {
			delete(root.Paths, pathStr)
		}
	}

	return root
}

func mergeDifference(specs ...*Document) *Document {
	root := cloneDocument(specs[0])
	if root == nil || root.Paths == nil {
		return root
	}

	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}

	for _, otherSpec := range specs[1:] {
		if otherSpec == nil || otherSpec.Paths == nil {
			continue
		}

		for pathStr, otherItem := range otherSpec.Paths {
			if otherItem == nil {
				continue
			}

			rootItem := root.Paths[pathStr]
			if rootItem == nil {
				continue
			}

			for _, m := range methods {
				if getPathItemOp(otherItem, m) != nil {
					setPathItemOp(rootItem, m, nil)
				}
			}

			if countPathItemOps(rootItem) == 0 {
				delete(root.Paths, pathStr)
			}
		}
	}

	return root
}

func setPathItemOp(p *PathItem, method string, op *Operation) {
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
	case "TRACE":
		p.Trace = op
	}
}

func countPathItemOps(p *PathItem) int {
	if p == nil {
		return 0
	}

	count := 0
	for _, m := range []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS", "TRACE"} {
		if getPathItemOp(p, m) != nil {
			count++
		}
	}

	return count
}

func mergePathItem(existing, incoming *PathItem) {
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

func mergeOperation(existing, incoming *Operation) {
	for _, p := range incoming.Parameters {
		if p != nil {
			hasParam := false
			for _, ep := range existing.Parameters {
				if ep != nil && ep.In == p.In && ep.Name == p.Name {
					hasParam = true
					break
				}
			}
			if !hasParam {
				existing.Parameters = append(existing.Parameters, p)
			}
		}
	}

	if incoming.RequestBody != nil {
		if existing.RequestBody == nil {
			existing.RequestBody = incoming.RequestBody
		} else {
			if existing.RequestBody.Content == nil {
				existing.RequestBody.Content = make(map[string]*MediaType)
			}
			for mt, content := range incoming.RequestBody.Content {
				existingContent := existing.RequestBody.Content[mt]
				if existingContent == nil {
					existing.RequestBody.Content[mt] = content
				} else if existingContent.Schema != nil && content.Schema != nil {
					mergeSchema(existingContent.Schema, content.Schema)
				}
			}
		}
	}

	if incoming.Responses != nil {
		if existing.Responses == nil {
			existing.Responses = make(map[string]*Response)
		}

		for statusStr, resp := range incoming.Responses {
			existingResp := existing.Responses[statusStr]
			if existingResp == nil {
				existing.Responses[statusStr] = resp
			} else if resp != nil {
				if existingResp.Content == nil {
					existingResp.Content = make(map[string]*MediaType)
				}
				for mt, content := range resp.Content {
					existingContent := existingResp.Content[mt]
					if existingContent == nil {
						existingResp.Content[mt] = content
					} else if existingContent.Schema != nil && content.Schema != nil {
						mergeSchema(existingContent.Schema, content.Schema)
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

func mergeSchema(dst, src *Schema) {
	if dst == nil || src == nil {
		return
	}

	if len(dst.Type) == 0 {
		dst.Type = src.Type
	}

	if dst.Properties == nil && src.Properties != nil {
		dst.Properties = make(map[string]*Schema)
	}

	for k, v := range src.Properties {
		if existingProp, ok := dst.Properties[k]; ok {
			mergeSchema(existingProp, v)
		} else {
			dst.Properties[k] = v
		}
	}
}

func cloneDocument(src *Document) *Document {
	if src == nil {
		return nil
	}

	d := *src
	d.Servers = slices.Clone(src.Servers)
	d.Paths = maps.Clone(src.Paths)

	if src.Components != nil {
		comp := *src.Components
		if src.Components.Schemas != nil {
			comp.Schemas = maps.Clone(src.Components.Schemas)
		}
		if src.Components.Responses != nil {
			comp.Responses = maps.Clone(src.Components.Responses)
		}
		if src.Components.Parameters != nil {
			comp.Parameters = maps.Clone(src.Components.Parameters)
		}
		if src.Components.RequestBodies != nil {
			comp.RequestBodies = maps.Clone(src.Components.RequestBodies)
		}
		d.Components = &comp
	}

	return &d
}
