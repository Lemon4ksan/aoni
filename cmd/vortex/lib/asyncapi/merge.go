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
	ensureRootContainers(root)

	for _, s := range specs[1:] {
		if s == nil {
			continue
		}

		mergeServers(root, s.Servers)
		mergeChannels(root, s.Channels)
		mergeOperations(root, s.Operations)
		mergeComponents(&root.Components, s.Components)
		mergeTags(root, s.Tags)
	}

	return root
}

func ensureRootContainers(root *Document) {
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
}

func mergeServers(root *Document, incomingServers map[string]Server) {
	for srvKey, srv := range incomingServers {
		if _, exists := root.Servers[srvKey]; !exists {
			root.Servers[srvKey] = srv
		}
	}
}

func mergeChannels(root *Document, incomingChannels map[string]Channel) {
	for chKey, ch := range incomingChannels {
		existing, exists := root.Channels[chKey]
		if exists {
			mergeChannel(&existing, ch)
			root.Channels[chKey] = existing
			continue
		}
		root.Channels[chKey] = ch
	}
}

func mergeOperations(root *Document, incomingOps map[string]Operation) {
	for opKey, op := range incomingOps {
		existing, exists := root.Operations[opKey]
		if exists {
			mergeOperation(&existing, op)
			root.Operations[opKey] = existing
			continue
		}
		root.Operations[opKey] = op
	}
}

func mergeTags(root *Document, incomingTags []Tag) {
	for _, tag := range incomingTags {
		if !hasTag(root.Tags, tag.Name) {
			root.Tags = append(root.Tags, tag)
		}
	}
}

func mergeIntersection(specs ...*Document) *Document {
	root := cloneDocument(specs[0])

	for _, s := range specs[1:] {
		// Filter channels
		for chKey := range root.Channels {
			if s.Channels == nil || !hasChannelKey(s.Channels, chKey) {
				delete(root.Channels, chKey)
			}
		}

		// Filter operations
		for opKey := range root.Operations {
			if s.Operations == nil || !hasOperationKey(s.Operations, opKey) {
				delete(root.Operations, opKey)
			}
		}
	}

	return root
}

func hasChannelKey(channels map[string]Channel, key string) bool {
	_, ok := channels[key]
	return ok
}

func hasOperationKey(ops map[string]Operation, key string) bool {
	_, ok := ops[key]
	return ok
}

func mergeDifference(specs ...*Document) *Document {
	root := cloneDocument(specs[0])

	for _, s := range specs[1:] {
		for chKey := range s.Channels {
			delete(root.Channels, chKey)
		}
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
	mergeGenericMap(&dst.Schemas, src.Schemas)
	mergeGenericMap(&dst.Messages, src.Messages)
	mergeGenericMap(&dst.Parameters, src.Parameters)
	mergeGenericMap(&dst.SecuritySchemes, src.SecuritySchemes)
	mergeGenericMap(&dst.ServerVariables, src.ServerVariables)
	mergeGenericMap(&dst.CorrelationIDs, src.CorrelationIDs)
	mergeGenericMap(&dst.Replies, src.Replies)
	mergeGenericMap(&dst.OperationTraits, src.OperationTraits)
	mergeGenericMap(&dst.MessageTraits, src.MessageTraits)
	mergeGenericMap(&dst.Tags, src.Tags)
}

func mergeGenericMap[K comparable, V any](dst *map[K]V, src map[K]V) {
	if src == nil {
		return
	}
	if *dst == nil {
		*dst = make(map[K]V)
	}
	for k, v := range src {
		if _, exists := (*dst)[k]; !exists {
			(*dst)[k] = v
		}
	}
}

func hasTag(tags []Tag, name string) bool {
	return slices.ContainsFunc(tags, func(t Tag) bool {
		return t.Name == name
	})
}

func hasRef(refs []RefObject, ref string) bool {
	return slices.ContainsFunc(refs, func(r RefObject) bool {
		return r.Ref == ref
	})
}

func cloneDocument(src *Document) *Document {
	if src == nil {
		return nil
	}

	d := *src
	d.Servers = maps.Clone(src.Servers)
	d.Channels = maps.Clone(src.Channels)
	d.Operations = maps.Clone(src.Operations)
	d.Tags = slices.Clone(src.Tags)

	d.Components = Components{
		Messages:        maps.Clone(src.Components.Messages),
		Schemas:         maps.Clone(src.Components.Schemas),
		Parameters:      maps.Clone(src.Components.Parameters),
		SecuritySchemes: maps.Clone(src.Components.SecuritySchemes),
		ServerVariables: maps.Clone(src.Components.ServerVariables),
		CorrelationIDs:  maps.Clone(src.Components.CorrelationIDs),
		Replies:         maps.Clone(src.Components.Replies),
		OperationTraits: maps.Clone(src.Components.OperationTraits),
		MessageTraits:   maps.Clone(src.Components.MessageTraits),
		Tags:            maps.Clone(src.Components.Tags),
	}

	return &d
}

func initNilComponents(c *Components) {
	if c.Messages == nil {
		c.Messages = make(map[string]Message)
	}
	if c.Schemas == nil {
		c.Schemas = make(map[string]Schema)
	}
	if c.Parameters == nil {
		c.Parameters = make(map[string]Parameter)
	}
	if c.SecuritySchemes == nil {
		c.SecuritySchemes = make(map[string]SecurityScheme)
	}
	if c.ServerVariables == nil {
		c.ServerVariables = make(map[string]ServerVar)
	}
	if c.CorrelationIDs == nil {
		c.CorrelationIDs = make(map[string]CorrelationID)
	}
	if c.Replies == nil {
		c.Replies = make(map[string]OperationReply)
	}
	if c.OperationTraits == nil {
		c.OperationTraits = make(map[string]Operation)
	}
	if c.MessageTraits == nil {
		c.MessageTraits = make(map[string]Message)
	}
	if c.Tags == nil {
		c.Tags = make(map[string]Tag)
	}
}
