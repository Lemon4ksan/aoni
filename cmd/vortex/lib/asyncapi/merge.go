// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package asyncapi

import (
	"maps"
	"slices"

	"github.com/lemon4ksan/foundation/generic"
)

// MergeMode defines the set operation used when merging multiple AsyncAPI specifications.
type MergeMode string

const (
	// MergeModeUnion includes all channels, operations, and components from all specs (A ∪ B). (Default)
	MergeModeUnion MergeMode = "union"

	// MergeModeIntersection includes only channels and operations present in all input specifications (A ∩ B).
	MergeModeIntersection MergeMode = "intersect"

	// MergeModeDifference includes only channels and operations present in the first specification and missing in others (A \ B).
	MergeModeDifference MergeMode = "diff"
)

// MergeSpecs combines multiple AsyncAPI specifications using default Union mode.
//
// Reference: AsyncAPI 3.1.0 Multi-Document Merging
func MergeSpecs(specs ...*Document) *Document {
	return MergeSpecsWithMode(MergeModeUnion, specs...)
}

// MergeSpecsWithMode combines multiple specifications using the chosen set operation (union, intersect, diff).
func MergeSpecsWithMode(mode MergeMode, specs ...*Document) *Document {
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

	if root.Servers == nil {
		root.Servers = make(map[string]Server)
	}
	if root.Channels == nil {
		root.Channels = make(map[string]Channel)
	}
	if root.Operations == nil {
		root.Operations = make(map[string]Operation)
	}

	initNilComponents(&root.Components)

	for _, s := range specs[1:] {
		if s == nil {
			continue
		}

		// 1. Merge Servers
		for srvKey, srv := range s.Servers {
			if _, exists := root.Servers[srvKey]; !exists {
				root.Servers[srvKey] = srv
			}
		}

		// 2. Merge Channels
		for chKey, ch := range s.Channels {
			if existing, exists := root.Channels[chKey]; exists {
				mergeChannel(&existing, ch)
				root.Channels[chKey] = existing
			} else {
				root.Channels[chKey] = ch
			}
		}

		// 3. Merge Operations
		for opKey, op := range s.Operations {
			if existing, exists := root.Operations[opKey]; exists {
				mergeOperation(&existing, op)
				root.Operations[opKey] = existing
			} else {
				root.Operations[opKey] = op
			}
		}

		// 4. Merge Components
		mergeComponents(&root.Components, s.Components)

		// 5. Merge Tags
		for _, tag := range s.Tags {
			if !hasTag(root.Tags, tag.Name) {
				root.Tags = append(root.Tags, tag)
			}
		}
	}

	return root
}

func mergeIntersection(specs ...*Document) *Document {
	root := cloneDocument(specs[0])

	for _, s := range specs[1:] {
		// Filter channels
		for chKey := range root.Channels {
			if s.Channels == nil {
				delete(root.Channels, chKey)
				continue
			}
			if _, exists := s.Channels[chKey]; !exists {
				delete(root.Channels, chKey)
			}
		}

		// Filter operations
		for opKey := range root.Operations {
			if s.Operations == nil {
				delete(root.Operations, opKey)
				continue
			}
			if _, exists := s.Operations[opKey]; !exists {
				delete(root.Operations, opKey)
			}
		}
	}

	return root
}

func mergeDifference(specs ...*Document) *Document {
	root := cloneDocument(specs[0])

	for _, s := range specs[1:] {
		// Remove channels present in subsequent specs
		for chKey := range s.Channels {
			delete(root.Channels, chKey)
		}

		// Remove operations present in subsequent specs
		for opKey := range s.Operations {
			delete(root.Operations, opKey)
		}
	}

	return root
}

func mergeChannel(existing *Channel, incoming Channel) {
	if existing.Messages == nil && incoming.Messages != nil {
		existing.Messages = make(map[string]Message)
	}
	for k, v := range incoming.Messages {
		if _, ok := existing.Messages[k]; !ok {
			existing.Messages[k] = v
		}
	}

	if existing.Parameters == nil && incoming.Parameters != nil {
		existing.Parameters = make(map[string]Parameter)
	}
	for k, v := range incoming.Parameters {
		if _, ok := existing.Parameters[k]; !ok {
			existing.Parameters[k] = v
		}
	}

	for _, tag := range incoming.Tags {
		if !hasTag(existing.Tags, tag.Name) {
			existing.Tags = append(existing.Tags, tag)
		}
	}
}

func mergeOperation(existing *Operation, incoming Operation) {
	for _, mRef := range incoming.Messages {
		if !hasRef(existing.Messages, mRef.Ref) {
			existing.Messages = append(existing.Messages, mRef)
		}
	}

	for _, trait := range incoming.Traits {
		if !hasRef(existing.Traits, trait.Ref) {
			existing.Traits = append(existing.Traits, trait)
		}
	}

	for _, tag := range incoming.Tags {
		if !hasTag(existing.Tags, tag.Name) {
			existing.Tags = append(existing.Tags, tag)
		}
	}
}

func mergeComponents(dst *Components, src Components) {
	if src.Schemas != nil {
		if dst.Schemas == nil {
			dst.Schemas = make(map[string]Schema)
		}
		for k, v := range src.Schemas {
			if _, exists := dst.Schemas[k]; !exists {
				dst.Schemas[k] = v
			}
		}
	}

	if src.Messages != nil {
		if dst.Messages == nil {
			dst.Messages = make(map[string]Message)
		}
		for k, v := range src.Messages {
			if _, exists := dst.Messages[k]; !exists {
				dst.Messages[k] = v
			}
		}
	}

	if src.Parameters != nil {
		if dst.Parameters == nil {
			dst.Parameters = make(map[string]Parameter)
		}
		for k, v := range src.Parameters {
			if _, exists := dst.Parameters[k]; !exists {
				dst.Parameters[k] = v
			}
		}
	}

	if src.SecuritySchemes != nil {
		if dst.SecuritySchemes == nil {
			dst.SecuritySchemes = make(map[string]SecurityScheme)
		}
		for k, v := range src.SecuritySchemes {
			if _, exists := dst.SecuritySchemes[k]; !exists {
				dst.SecuritySchemes[k] = v
			}
		}
	}

	if src.OperationTraits != nil {
		if dst.OperationTraits == nil {
			dst.OperationTraits = make(map[string]Operation)
		}
		for k, v := range src.OperationTraits {
			if _, exists := dst.OperationTraits[k]; !exists {
				dst.OperationTraits[k] = v
			}
		}
	}

	if src.MessageTraits != nil {
		if dst.MessageTraits == nil {
			dst.MessageTraits = make(map[string]Message)
		}
		for k, v := range src.MessageTraits {
			if _, exists := dst.MessageTraits[k]; !exists {
				dst.MessageTraits[k] = v
			}
		}
	}
}

func cloneDocument(src *Document) *Document {
	if src == nil {
		return nil
	}

	d := *src

	if src.Servers != nil {
		d.Servers = maps.Clone(src.Servers)
	}
	if src.Channels != nil {
		d.Channels = maps.Clone(src.Channels)
	}
	if src.Operations != nil {
		d.Operations = maps.Clone(src.Operations)
	}
	if src.Tags != nil {
		d.Tags = slices.Clone(src.Tags)
	}

	d.Components = cloneComponents(src.Components)

	return &d
}

func cloneComponents(src Components) Components {
	c := src
	if src.Schemas != nil {
		c.Schemas = maps.Clone(src.Schemas)
	}
	if src.Messages != nil {
		c.Messages = maps.Clone(src.Messages)
	}
	if src.Parameters != nil {
		c.Parameters = maps.Clone(src.Parameters)
	}
	if src.SecuritySchemes != nil {
		c.SecuritySchemes = maps.Clone(src.SecuritySchemes)
	}
	if src.ServerVariables != nil {
		c.ServerVariables = maps.Clone(src.ServerVariables)
	}
	if src.CorrelationIDs != nil {
		c.CorrelationIDs = maps.Clone(src.CorrelationIDs)
	}
	if src.Replies != nil {
		c.Replies = maps.Clone(src.Replies)
	}
	if src.OperationTraits != nil {
		c.OperationTraits = maps.Clone(src.OperationTraits)
	}
	if src.MessageTraits != nil {
		c.MessageTraits = maps.Clone(src.MessageTraits)
	}
	if src.Tags != nil {
		c.Tags = maps.Clone(src.Tags)
	}

	return c
}

func initNilComponents(c *Components) {
	if c.Schemas == nil {
		c.Schemas = make(map[string]Schema)
	}
	if c.Messages == nil {
		c.Messages = make(map[string]Message)
	}
	if c.Parameters == nil {
		c.Parameters = make(map[string]Parameter)
	}
	if c.SecuritySchemes == nil {
		c.SecuritySchemes = make(map[string]SecurityScheme)
	}
	if c.OperationTraits == nil {
		c.OperationTraits = make(map[string]Operation)
	}
	if c.MessageTraits == nil {
		c.MessageTraits = make(map[string]Message)
	}
}

func hasTag(tags []Tag, name string) bool {
	return generic.Any(tags, func(t Tag) bool {
		return t.Name == name
	})
}

func hasRef(refs []RefObject, ref string) bool {
	return generic.Any(refs, func(r RefObject) bool {
		return r.Ref == ref
	})
}
