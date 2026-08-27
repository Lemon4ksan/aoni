// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package asyncapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/lemon4ksan/foundation/generic"
	"gopkg.in/yaml.v3"
)

// ParseSpec parses, normalizes, and resolves traits for an AsyncAPI specification conforming to AsyncAPI 2.x and 3.x.
//
// # References
//   - AsyncAPI 3.1.0 §AsyncAPI Document Object: https://www.asyncapi.com/docs/reference/specification/v3.1.0#asyncapi-document-object
//   - AsyncAPI 2.6.0 §AsyncAPI Object: https://v2.asyncapi.com/docs/reference/specification/v2.6.0#asyncapiObject
//   - JSON Schema draft 2020-12 (AsyncAPI 3.1): https://json-schema.org/draft/2020-12/json-schema-core.html
func ParseSpec(data []byte) (*Document, error) {
	if len(data) == 0 {
		return nil, errors.New("vortex/asyncapi: empty specification data")
	}

	var doc Document
	if err := yaml.Unmarshal(data, &doc); err != nil {
		if errJSON := json.Unmarshal(data, &doc); errJSON != nil {
			return nil, fmt.Errorf("vortex/asyncapi: parse specification: %w", err)
		}
	}

	if doc.AsyncAPI == "" {
		doc.AsyncAPI = detectAsyncAPIVersion(data)
	}

	// 1. Normalize AsyncAPI 2.x publish/subscribe channels into 3.x operations model
	normalizeAsyncAPI2(&doc)

	// 2. Resolve traits merging mechanism (AsyncAPI 3.1.0 & 2.6.0 §Traits Merge Mechanism)
	applyTraits(&doc)

	// 3. Extract channel address parameters from dynamic templates ({param})
	extractAddressParameters(&doc)

	return &doc, nil
}

func detectAsyncAPIVersion(data []byte) string {
	var raw map[string]any

	_ = yaml.Unmarshal(data, &raw)
	if v, ok := raw["asyncapi"].(string); ok {
		return v
	}

	return "3.0.0"
}

// LoadFile reads and parses an AsyncAPI spec file from disk.
func LoadFile(filename string) (*Document, error) {
	return LoadSpec(filename, nil)
}

// LoadSpec loads an AsyncAPI specification with default Union merge mode.
func LoadSpec(filename string, data []byte) (*Document, error) {
	return LoadSpecWithMode(filename, data, MergeModeUnion)
}

// LoadSpecWithMode loads and combines multiple specifications using the specified MergeMode (union, intersect, diff).
//
// # References
//   - AsyncAPI 3.1.0 §Multi-Document Composition: https://www.asyncapi.com/docs/concepts/asyncapi-document
func LoadSpecWithMode(filename string, data []byte, mode MergeMode) (*Document, error) {
	if len(data) > 0 {
		return ParseSpec(data)
	}

	files, err := resolveSpecFiles(filename)
	if err != nil {
		return nil, err
	}

	if len(files) == 1 {
		return loadSingleFile(files[0])
	}

	var allSpecs []*Document
	for _, f := range files {
		doc, lErr := loadSingleFile(f)
		if lErr != nil {
			return nil, fmt.Errorf("vortex/asyncapi: read spec file %s: %w", f, lErr)
		}

		allSpecs = append(allSpecs, doc)
	}

	return MergeSpecsWithMode(mode, allSpecs...), nil
}

func resolveSpecFiles(target string) ([]string, error) {
	parts := strings.Split(target, ",")

	var result []string

	for _, part := range parts {
		clean := strings.TrimSpace(part)
		if clean == "" {
			continue
		}

		if strings.ContainsAny(clean, "*?[]") {
			matches, err := filepath.Glob(clean)
			if err != nil {
				return nil, fmt.Errorf("vortex/asyncapi: invalid glob pattern %q: %w", clean, err)
			}

			result = append(result, matches...)

			continue
		}

		result = append(result, clean)
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("vortex/asyncapi: no valid specification files found in %q", target)
	}

	return result, nil
}

func loadSingleFile(filename string) (*Document, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("vortex/asyncapi: read spec file %s: %w", filename, err)
	}

	return ParseSpec(data)
}

// normalizeAsyncAPI2 translates an AsyncAPI 2.x document into the unified AsyncAPI 3.x document model.
//
// References:
//   - AsyncAPI 2.6.0 §Channel Item Object: https://v2.asyncapi.com/docs/reference/specification/v2.6.0#channelItemObject
//   - AsyncAPI 2.6.0 §Operation Object (Publish vs Subscribe semantics): https://v2.asyncapi.com/docs/reference/specification/v2.6.0#operationObject
//   - AsyncAPI 3.1.0 §Operations Model (Action: send vs receive): https://www.asyncapi.com/docs/reference/specification/v3.1.0#operation-object
func normalizeAsyncAPI2(doc *Document) {
	if doc == nil {
		return
	}

	normalizeServers2(doc)

	if doc.Channels == nil {
		return
	}

	if doc.Operations == nil {
		doc.Operations = make(map[string]Operation)
	}

	normalizeChannelsAndOperations2(doc)
}

// normalizeServers2 upgrades AsyncAPI 2.6.0 Server URL string format into AsyncAPI 3.1.0 Host and Pathname structure.
//
// References:
//   - AsyncAPI 2.6.0 §Server Object: https://v2.asyncapi.com/docs/reference/specification/v2.6.0#serverObject
//   - AsyncAPI 3.1.0 §Server Object: https://www.asyncapi.com/docs/reference/specification/v3.1.0#server-object
func normalizeServers2(doc *Document) {
	for sKey, srv := range doc.Servers {
		if srv.Host != "" || srv.URL == "" {
			continue
		}

		u := srv.URL
		if idx := strings.Index(u, "://"); idx != -1 {
			u = u[idx+3:]
		}

		parts := strings.SplitN(u, "/", 2)

		srv.Host = parts[0]
		if len(parts) > 1 && parts[1] != "" {
			srv.Pathname = "/" + parts[1]
		}

		doc.Servers[sKey] = srv
	}
}

// normalizeChannelsAndOperations2 converts AsyncAPI 2.x publish/subscribe channels into AsyncAPI 3.x action operations.
//
// References:
//   - AsyncAPI 2.6.0 §Channel Item Object: https://v2.asyncapi.com/docs/reference/specification/v2.6.0#channelItemObject
//   - AsyncAPI 3.1.0 §Operation Object: https://www.asyncapi.com/docs/reference/specification/v3.1.0#operation-object
func normalizeChannelsAndOperations2(doc *Document) {
	for channelPath, ch := range doc.Channels {
		if ch.Address == "" {
			ch.Address = channelPath
			doc.Channels[channelPath] = ch
		}

		if ch.Publish != nil {
			opID := generic.Coalesce(ch.Publish.OperationID, "on"+sanitizeIdentifier(channelPath))
			doc.Operations[opID] = buildOperationFromPublish2(ch.Publish, channelPath)
		}

		if ch.Subscribe != nil {
			opID := generic.Coalesce(ch.Subscribe.OperationID, "send"+sanitizeIdentifier(channelPath))
			doc.Operations[opID] = buildOperationFromSubscribe2(ch.Subscribe, channelPath)
		}
	}
}

func buildOperationFromPublish2(pub *Operation2, channelPath string) Operation {
	return Operation{
		Action:       "send",
		ChannelRef:   channelPath,
		Summary:      pub.Summary,
		Description:  pub.Description,
		Tags:         pub.Tags,
		ExternalDocs: pub.ExternalDocs,
		Bindings:     pub.Bindings,
		Traits:       pub.Traits,
		Messages:     extractMessagesFrom2(pub.Message),
	}
}

func buildOperationFromSubscribe2(sub *Operation2, channelPath string) Operation {
	return Operation{
		Action:       "receive",
		ChannelRef:   channelPath,
		Summary:      sub.Summary,
		Description:  sub.Description,
		Tags:         sub.Tags,
		ExternalDocs: sub.ExternalDocs,
		Bindings:     sub.Bindings,
		Traits:       sub.Traits,
		Messages:     extractMessagesFrom2(sub.Message),
	}
}

func extractMessagesFrom2(msgAny any) []RefObject {
	if msgAny == nil {
		return nil
	}

	mMap, ok := msgAny.(map[string]any)
	if !ok {
		return nil
	}

	if refStr, ok := mMap["$ref"].(string); ok && refStr != "" {
		return []RefObject{{Ref: refStr}}
	}

	oneOfList, ok := mMap["oneOf"].([]any)
	if !ok {
		return nil
	}

	var res []RefObject
	for _, item := range oneOfList {
		if itemMap, ok := item.(map[string]any); ok {
			if refStr, ok := itemMap["$ref"].(string); ok && refStr != "" {
				res = append(res, RefObject{Ref: refStr})
			}
		}
	}

	return res
}

// applyTraits applies operationTraits and messageTraits according to the AsyncAPI merging mechanism:
// Traits are merged into the target object without overriding already defined properties.
//
// References:
//   - AsyncAPI 3.1.0 §Trait Merging Mechanism: https://www.asyncapi.com/docs/concepts/asyncapi-document/reusability-with-traits#trait-merging-mechanism
//   - AsyncAPI 2.6.0 §Operation Trait Object: https://v2.asyncapi.com/docs/reference/specification/v2.6.0#operationTraitObject
//   - AsyncAPI 2.6.0 §Message Trait Object: https://v2.asyncapi.com/docs/reference/specification/v2.6.0#messageTraitObject
func applyTraits(doc *Document) {
	if doc == nil {
		return
	}

	applyOperationTraits(doc)
	applyMessageTraits(doc)
}

func applyOperationTraits(doc *Document) {
	for opKey, op := range doc.Operations {
		for _, traitRef := range op.Traits {
			traitName := extractRefKey(traitRef.Ref)

			trait, ok := doc.Components.OperationTraits[traitName]
			if !ok {
				continue
			}

			op.Summary = generic.Coalesce(op.Summary, trait.Summary)
			op.Description = generic.Coalesce(op.Description, trait.Description)

			op.Title = generic.Coalesce(op.Title, trait.Title)
			if len(op.Tags) == 0 {
				op.Tags = trait.Tags
			}

			if len(op.Security) == 0 {
				op.Security = trait.Security
			}
		}

		doc.Operations[opKey] = op
	}
}

func applyMessageTraits(doc *Document) {
	for msgKey, msg := range doc.Components.Messages {
		for _, traitRef := range msg.Traits {
			traitName := extractRefKey(traitRef.Ref)

			trait, ok := doc.Components.MessageTraits[traitName]
			if !ok {
				continue
			}

			msg.Description = generic.Coalesce(msg.Description, trait.Description)
			msg.Summary = generic.Coalesce(msg.Summary, trait.Summary)
			msg.Title = generic.Coalesce(msg.Title, trait.Title)

			msg.ContentType = generic.Coalesce(msg.ContentType, trait.ContentType)
			if msg.CorrelationID == nil {
				msg.CorrelationID = trait.CorrelationID
			}

			if len(msg.Tags) == 0 {
				msg.Tags = trait.Tags
			}

			mergeTraitHeaders(&msg, trait.Headers)
		}

		doc.Components.Messages[msgKey] = msg
	}
}

func mergeTraitHeaders(msg *Message, traitHeaders *Schema) {
	if traitHeaders == nil {
		return
	}

	if msg.Headers == nil {
		msg.Headers = traitHeaders
		return
	}

	if traitHeaders.Properties == nil {
		return
	}

	if msg.Headers.Properties == nil {
		msg.Headers.Properties = make(map[string]Schema)
	}

	for propKey, propVal := range traitHeaders.Properties {
		if _, exists := msg.Headers.Properties[propKey]; !exists {
			msg.Headers.Properties[propKey] = propVal
		}
	}
}

// extractAddressParameters parses {param} placeholders in channel addresses and populates channel.Parameters.
//
// References:
//   - AsyncAPI 3.1.0 §Parameters in Channel Address: https://www.asyncapi.com/docs/concepts/asyncapi-document/dynamic-channel-address
//   - AsyncAPI 2.6.0 §Parameters Object: https://v2.asyncapi.com/docs/reference/specification/v2.6.0#parametersObject
//   - RFC 6570 §URI Template: https://datatracker.ietf.org/doc/html/rfc6570
func extractAddressParameters(doc *Document) {
	if doc == nil || doc.Channels == nil {
		return
	}

	for chKey, ch := range doc.Channels {
		addr := generic.Coalesce(ch.Address, chKey)

		if ch.Parameters == nil {
			ch.Parameters = make(map[string]Parameter)
		}

		for _, paramName := range extractTemplatePlaceholders(addr) {
			if _, exists := ch.Parameters[paramName]; exists {
				continue
			}

			if compParam, ok := doc.Components.Parameters[paramName]; ok {
				ch.Parameters[paramName] = compParam
				continue
			}

			ch.Parameters[paramName] = Parameter{
				Description: "Channel address parameter " + paramName,
				Schema:      Schema{Type: "string"},
			}
		}

		doc.Channels[chKey] = ch
	}
}

func extractTemplatePlaceholders(s string) []string {
	var placeholders []string

	rem := s
	for {
		start := strings.Index(rem, "{")
		if start == -1 {
			break
		}

		end := strings.Index(rem[start:], "}")
		if end == -1 {
			break
		}

		name := rem[start+1 : start+end]
		if !slices.Contains(placeholders, name) {
			placeholders = append(placeholders, name)
		}

		rem = rem[start+end+1:]
	}

	return placeholders
}

func extractRefKey(ref string) string {
	parts := strings.Split(ref, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}

	return ref
}

func sanitizeIdentifier(s string) string {
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "{", "")
	s = strings.ReplaceAll(s, "}", "")
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, ".", "_")
	s = strings.Trim(s, "_")

	parts := generic.Filter(strings.Split(s, "_"), func(p string) bool { return p != "" })

	var sb strings.Builder
	for _, p := range parts {
		sb.WriteString(strings.ToUpper(p[:1]))
		sb.WriteString(p[1:])
	}

	return sb.String()
}
