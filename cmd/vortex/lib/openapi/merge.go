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

var httpMethods = []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS", "TRACE"}

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
	ensureRootContainers(root)

	for _, s := range specs[1:] {
		if s == nil {
			continue
		}
		mergeServers(root, s.Servers)
		mergeSchemas(root, s.Components)
		mergeInfoHeaders(root, s.Info)
		mergePaths(root, s.Paths)
	}

	return root
}

func ensureRootContainers(root *Document) {
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
}

func mergeServers(root *Document, servers []Server) {
	for _, srv := range servers {
		if srv.URL == "" || hasServerURL(root.Servers, srv.URL) {
			continue
		}
		root.Servers = append(root.Servers, srv)
	}
}

func hasServerURL(servers []Server, targetURL string) bool {
	return slices.ContainsFunc(servers, func(s Server) bool {
		return s.URL == targetURL
	})
}

func mergeSchemas(root *Document, incomingComp *Components) {
	if incomingComp == nil || incomingComp.Schemas == nil {
		return
	}

	for name, schema := range incomingComp.Schemas {
		existingSchema, exists := root.Components.Schemas[name]
		if exists && existingSchema != nil && schema != nil {
			mergeSchema(existingSchema, schema)
			continue
		}
		root.Components.Schemas[name] = schema
	}
}

func mergeInfoHeaders(root *Document, incomingInfo *Info) {
	if incomingInfo == nil || incomingInfo.Extensions == nil {
		return
	}

	hRaw, ok := incomingInfo.Extensions["x-vortex-headers"]
	if !ok {
		return
	}

	ensureInfoExtensions(root)

	existingHeaders, _ := root.Info.Extensions["x-vortex-headers"].([]map[string]string)
	root.Info.Extensions["x-vortex-headers"] = mergeHeadersList(existingHeaders, hRaw)
}

func ensureInfoExtensions(root *Document) {
	if root.Info == nil {
		root.Info = &Info{
			Title:   "Combined API Specification",
			Version: "1.0.0",
		}
	}
	if root.Info.Extensions == nil {
		root.Info.Extensions = make(map[string]any)
	}
}

func mergeHeadersList(existing []map[string]string, incoming any) []map[string]string {
	headerMap := make(map[string]string)
	for _, h := range existing {
		headerMap[h["name"]] = h["value"]
	}

	switch inList := incoming.(type) {
	case []map[string]string:
		for _, h := range inList {
			if _, exists := headerMap[h["name"]]; !exists {
				headerMap[h["name"]] = h["value"]
				existing = append(existing, h)
			}
		}
	case []any:
		for _, item := range inList {
			hMap, isMap := item.(map[string]any)
			if !isMap {
				continue
			}
			name, _ := hMap["name"].(string)
			val, _ := hMap["value"].(string)
			if name != "" && val != "" {
				if _, exists := headerMap[name]; !exists {
					headerMap[name] = val
					existing = append(existing, map[string]string{
						"name":  name,
						"value": val,
					})
				}
			}
		}
	}

	return existing
}

func mergePaths(root *Document, incomingPaths map[string]*PathItem) {
	if incomingPaths == nil {
		return
	}

	for pathStr, pathItem := range incomingPaths {
		if pathItem == nil {
			continue
		}

		existingItem := root.Paths[pathStr]
		if existingItem == nil {
			root.Paths[pathStr] = pathItem
			continue
		}

		mergePathItem(existingItem, pathItem)
	}
}

func mergeIntersection(specs ...*Document) *Document {
	root := cloneDocument(specs[0])
	if root == nil || root.Paths == nil {
		return root
	}

	for pathStr, pathItem := range root.Paths {
		if pathItem == nil {
			continue
		}

		for _, m := range httpMethods {
			op := getPathItemOp(pathItem, m)
			if op == nil {
				continue
			}

			if !isOpInAllSpecs(specs[1:], pathStr, m) {
				setPathItemOp(pathItem, m, nil)
				continue
			}

			mergeOpFromAllSpecs(specs[1:], root, op, pathStr, m)
		}

		if countPathItemOps(pathItem) == 0 {
			delete(root.Paths, pathStr)
		}
	}

	return root
}

func isOpInAllSpecs(specs []*Document, pathStr, method string) bool {
	return generic.All(specs, func(other *Document) bool {
		if other == nil || other.Paths == nil {
			return false
		}
		item := other.Paths[pathStr]
		return item != nil && getPathItemOp(item, method) != nil
	})
}

func mergeOpFromAllSpecs(specs []*Document, root *Document, op *Operation, pathStr, method string) {
	for _, other := range specs {
		if other == nil || other.Paths == nil {
			continue
		}
		otherItem := other.Paths[pathStr]
		if otherItem != nil {
			if otherOp := getPathItemOp(otherItem, method); otherOp != nil {
				mergeOperation(op, otherOp)
			}
		}
		mergeSchemas(root, other.Components)
	}
}

func mergeDifference(specs ...*Document) *Document {
	root := cloneDocument(specs[0])
	if root == nil || root.Paths == nil {
		return root
	}

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

			for _, m := range httpMethods {
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
	for _, m := range httpMethods {
		if getPathItemOp(p, m) != nil {
			count++
		}
	}
	return count
}

func mergePathItem(existing, incoming *PathItem) {
	mergeSubOp(&existing.Get, incoming.Get)
	mergeSubOp(&existing.Post, incoming.Post)
	mergeSubOp(&existing.Put, incoming.Put)
	mergeSubOp(&existing.Delete, incoming.Delete)
	mergeSubOp(&existing.Patch, incoming.Patch)
	mergeSubOp(&existing.Head, incoming.Head)
	mergeSubOp(&existing.Options, incoming.Options)
	mergeSubOp(&existing.Trace, incoming.Trace)
}

func mergeSubOp(dst **Operation, incoming *Operation) {
	if incoming == nil {
		return
	}
	if *dst == nil {
		*dst = incoming
		return
	}
	mergeOperation(*dst, incoming)
}

func mergeOperation(existing, incoming *Operation) {
	mergeOperationParams(existing, incoming.Parameters)
	mergeOperationRequestBody(existing, incoming.RequestBody)
	mergeOperationResponses(existing, incoming.Responses)
	mergeOperationHeaders(existing, incoming.Extensions)
}

func mergeOperationParams(existing *Operation, incomingParams []*Parameter) {
	for _, p := range incomingParams {
		if p == nil || hasParam(existing.Parameters, p.In, p.Name) {
			continue
		}
		existing.Parameters = append(existing.Parameters, p)
	}
}

func hasParam(params []*Parameter, inLocation, name string) bool {
	return slices.ContainsFunc(params, func(ep *Parameter) bool {
		return ep != nil && ep.In == inLocation && ep.Name == name
	})
}

func mergeOperationRequestBody(existing *Operation, incomingReqBody *RequestBody) {
	if incomingReqBody == nil {
		return
	}
	if existing.RequestBody == nil {
		existing.RequestBody = incomingReqBody
		return
	}
	if existing.RequestBody.Content == nil {
		existing.RequestBody.Content = make(map[string]*MediaType)
	}

	for mt, content := range incomingReqBody.Content {
		existingContent := existing.RequestBody.Content[mt]
		if existingContent == nil {
			existing.RequestBody.Content[mt] = content
			continue
		}
		if existingContent.Schema != nil && content.Schema != nil {
			mergeSchema(existingContent.Schema, content.Schema)
		}
	}
}

func mergeOperationResponses(existing *Operation, incomingResponses map[string]*Response) {
	if incomingResponses == nil {
		return
	}
	if existing.Responses == nil {
		existing.Responses = make(map[string]*Response)
	}

	for statusStr, resp := range incomingResponses {
		existingResp := existing.Responses[statusStr]
		if existingResp == nil {
			existing.Responses[statusStr] = resp
			continue
		}
		if resp == nil {
			continue
		}
		if existingResp.Content == nil {
			existingResp.Content = make(map[string]*MediaType)
		}
		for mt, content := range resp.Content {
			existingContent := existingResp.Content[mt]
			if existingContent == nil {
				existingResp.Content[mt] = content
				continue
			}
			if existingContent.Schema != nil && content.Schema != nil {
				mergeSchema(existingContent.Schema, content.Schema)
			}
		}
	}
}

func mergeOperationHeaders(existing *Operation, incomingExts map[string]any) {
	if incomingExts == nil {
		return
	}

	inHeadersRaw, ok := incomingExts["x-vortex-headers"]
	if !ok {
		return
	}

	if existing.Extensions == nil {
		existing.Extensions = make(map[string]any)
	}

	existingHeaders, _ := existing.Extensions["x-vortex-headers"].([]map[string]string)
	existing.Extensions["x-vortex-headers"] = mergeHeadersList(existingHeaders, inHeadersRaw)
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
		existingProp, ok := dst.Properties[k]
		if ok {
			mergeSchema(existingProp, v)
			continue
		}
		dst.Properties[k] = v
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
