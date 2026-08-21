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
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseSpec parses, normalizes, and resolves traits for an AsyncAPI specification conforming to AsyncAPI 2.x and 3.x.
//
// References:
//   - AsyncAPI 3.1.0 §Document Object (https://www.asyncapi.com/docs/concepts/asyncapi-document)
//   - AsyncAPI 2.6.0 §AsyncAPI Object (https://v2.asyncapi.com/docs/reference/specification/v2.6.0#asyncapiObject)
func ParseSpec(data []byte) (*Document, error) {
	if len(data) == 0 {
		return nil, errors.New("empty asyncapi specification data")
	}

	var doc Document

	// Try unmarshaling YAML first (YAML is a superset of JSON)
	if err := yaml.Unmarshal(data, &doc); err != nil {
		if errJSON := json.Unmarshal(data, &doc); errJSON != nil {
			return nil, fmt.Errorf("failed to parse asyncapi spec: %w", err)
		}
	}

	if doc.AsyncAPI == "" {
		// Detect version prefix
		var raw map[string]any
		_ = yaml.Unmarshal(data, &raw)
		if v, ok := raw["asyncapi"].(string); ok {
			doc.AsyncAPI = v
		}
	}

	// 1. Normalize AsyncAPI 2.x publish/subscribe channels into 3.x operations model
	normalizeAsyncAPI2(&doc)

	// 2. Resolve traits merging mechanism (AsyncAPI 3.1.0 & 2.6.0 §Traits Merge Mechanism)
	applyTraits(&doc)

	// 3. Extract channel address parameters from dynamic templates ({param})
	extractAddressParameters(&doc)

	return &doc, nil
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
func LoadSpecWithMode(filename string, data []byte, mode MergeMode) (*Document, error) {
	if len(data) > 0 {
		return ParseSpec(data)
	}

	if strings.Contains(filename, ",") {
		parts := strings.Split(filename, ",")
		var allSpecs []*Document

		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}

			if strings.ContainsAny(part, "*?[]") {
				matches, err := filepath.Glob(part)
				if err != nil {
					return nil, fmt.Errorf("invalid glob pattern %q: %w", part, err)
				}

				for _, match := range matches {
					doc, lErr := LoadFile(match)
					if lErr != nil {
						return nil, fmt.Errorf("failed reading spec file %s: %w", match, lErr)
					}
					allSpecs = append(allSpecs, doc)
				}
			} else {
				doc, lErr := LoadFile(part)
				if lErr != nil {
					return nil, fmt.Errorf("failed reading spec file %s: %w", part, lErr)
				}
				allSpecs = append(allSpecs, doc)
			}
		}

		if len(allSpecs) == 0 {
			return nil, fmt.Errorf("no valid asyncapi specification files found in %q", filename)
		}

		return MergeSpecsWithMode(mode, allSpecs...), nil
	}

	if strings.ContainsAny(filename, "*?[]") {
		matches, err := filepath.Glob(filename)
		if err == nil && len(matches) > 0 {
			var allSpecs []*Document
			for _, match := range matches {
				doc, lErr := LoadFile(match)
				if lErr != nil {
					return nil, fmt.Errorf("failed reading spec file %s: %w", match, lErr)
				}
				allSpecs = append(allSpecs, doc)
			}
			return MergeSpecsWithMode(mode, allSpecs...), nil
		}
	}

	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("reading asyncapi spec %s: %w", filename, err)
	}

	return ParseSpec(data)
}

// normalizeAsyncAPI2 translates an AsyncAPI 2.x document into the unified AsyncAPI 3.x document model.
//
// References:
//   - AsyncAPI 2.6.0 §Channel Item Object (https://v2.asyncapi.com/docs/reference/specification/v2.6.0#channelItemObject)
//   - AsyncAPI 2.6.0 §Operation Object (https://v2.asyncapi.com/docs/reference/specification/v2.6.0#operationObject)
func normalizeAsyncAPI2(doc *Document) {
	if doc == nil {
		return
	}

	// 1. Normalize Servers: 2.x 'url' -> 3.x 'host' and 'pathname'
	for sKey, srv := range doc.Servers {
		if srv.Host == "" && srv.URL != "" {
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

	if doc.Channels == nil {
		return
	}

	if doc.Operations == nil {
		doc.Operations = make(map[string]Operation)
	}

	for channelPath, ch := range doc.Channels {
		// Set channel address if empty (in 2.x channelPath is the address)
		if ch.Address == "" {
			ch.Address = channelPath
			doc.Channels[channelPath] = ch
		}

		// In AsyncAPI 2.x:
		// `publish` means application publishes/consumes from channel (client receives messages -> action: send)
		if ch.Publish != nil {
			opID := ch.Publish.OperationID
			if opID == "" {
				opID = "on" + sanitizeIdentifier(channelPath)
			}

			messages := extractMessagesFrom2(ch.Publish.Message)

			doc.Operations[opID] = Operation{
				Action:       "send",
				ChannelRef:   channelPath,
				Summary:      ch.Publish.Summary,
				Description:  ch.Publish.Description,
				Tags:         ch.Publish.Tags,
				ExternalDocs: ch.Publish.ExternalDocs,
				Bindings:     ch.Publish.Bindings,
				Traits:       ch.Publish.Traits,
				Messages:     messages,
			}
		}

		// `subscribe` means application subscribes/produces to channel (client sends messages -> action: receive)
		if ch.Subscribe != nil {
			opID := ch.Subscribe.OperationID
			if opID == "" {
				opID = "send" + sanitizeIdentifier(channelPath)
			}

			messages := extractMessagesFrom2(ch.Subscribe.Message)

			doc.Operations[opID] = Operation{
				Action:       "receive",
				ChannelRef:   channelPath,
				Summary:      ch.Subscribe.Summary,
				Description:  ch.Subscribe.Description,
				Tags:         ch.Subscribe.Tags,
				ExternalDocs: ch.Subscribe.ExternalDocs,
				Bindings:     ch.Subscribe.Bindings,
				Traits:       ch.Subscribe.Traits,
				Messages:     messages,
			}
		}
	}
}

func extractMessagesFrom2(msgAny any) []RefObject {
	if msgAny == nil {
		return nil
	}

	var res []RefObject

	if mMap, ok := msgAny.(map[string]any); ok {
		if refStr, ok := mMap["$ref"].(string); ok && refStr != "" {
			res = append(res, RefObject{Ref: refStr})
			return res
		}

		// Check oneOf in 2.x
		if oneOfList, ok := mMap["oneOf"].([]any); ok {
			for _, item := range oneOfList {
				if itemMap, ok := item.(map[string]any); ok {
					if refStr, ok := itemMap["$ref"].(string); ok && refStr != "" {
						res = append(res, RefObject{Ref: refStr})
					}
				}
			}
			return res
		}
	}

	return res
}

// applyTraits applies operationTraits and messageTraits according to the AsyncAPI merging mechanism:
// Traits are merged into the target object without overriding already defined properties.
//
// References:
//   - AsyncAPI 3.1.0 §Trait Merging Mechanism (https://www.asyncapi.com/docs/concepts/asyncapi-document/reusability-with-traits#trait-merging-mechanism)
//   - AsyncAPI 2.6.0 §Operation Trait Object & §Message Trait Object
func applyTraits(doc *Document) {
	if doc == nil {
		return
	}

	// 1. Merge Operation Traits
	for opKey, op := range doc.Operations {
		for _, traitRef := range op.Traits {
			traitName := extractRefKey(traitRef.Ref)
			if trait, ok := doc.Components.OperationTraits[traitName]; ok {
				if op.Summary == "" {
					op.Summary = trait.Summary
				}
				if op.Description == "" {
					op.Description = trait.Description
				}
				if op.Title == "" {
					op.Title = trait.Title
				}
				if len(op.Tags) == 0 {
					op.Tags = trait.Tags
				}
				if len(op.Security) == 0 {
					op.Security = trait.Security
				}
			}
		}
		doc.Operations[opKey] = op
	}

	// 2. Merge Message Traits across Components
	for msgKey, msg := range doc.Components.Messages {
		for _, traitRef := range msg.Traits {
			traitName := extractRefKey(traitRef.Ref)
			if trait, ok := doc.Components.MessageTraits[traitName]; ok {
				if msg.Description == "" {
					msg.Description = trait.Description
				}
				if msg.Summary == "" {
					msg.Summary = trait.Summary
				}
				if msg.Title == "" {
					msg.Title = trait.Title
				}
				if msg.ContentType == "" {
					msg.ContentType = trait.ContentType
				}
				if msg.CorrelationID == nil {
					msg.CorrelationID = trait.CorrelationID
				}
				if len(msg.Tags) == 0 {
					msg.Tags = trait.Tags
				}
				if trait.Headers != nil {
					if msg.Headers == nil {
						msg.Headers = trait.Headers
					} else if trait.Headers.Properties != nil {
						if msg.Headers.Properties == nil {
							msg.Headers.Properties = make(map[string]Schema)
						}
						for propKey, propVal := range trait.Headers.Properties {
							if _, exists := msg.Headers.Properties[propKey]; !exists {
								msg.Headers.Properties[propKey] = propVal
							}
						}
					}
				}
			}
		}
		doc.Components.Messages[msgKey] = msg
	}
}

// extractAddressParameters parses {param} placeholders in channel addresses and populates channel.Parameters.
//
// References:
//   - AsyncAPI 3.1.0 §Parameters in Channel Address (https://www.asyncapi.com/docs/concepts/asyncapi-document/dynamic-channel-address)
//   - AsyncAPI 2.6.0 §Parameters Object (https://v2.asyncapi.com/docs/reference/specification/v2.6.0#parametersObject)
func extractAddressParameters(doc *Document) {
	if doc == nil || doc.Channels == nil {
		return
	}

	for chKey, ch := range doc.Channels {
		addr := ch.Address
		if addr == "" {
			addr = chKey
		}

		if ch.Parameters == nil {
			ch.Parameters = make(map[string]Parameter)
		}

		// Find placeholders enclosed in curly braces like {userId} or {streetlightId}
		for {
			start := strings.Index(addr, "{")
			if start == -1 {
				break
			}
			end := strings.Index(addr[start:], "}")
			if end == -1 {
				break
			}

			paramName := addr[start+1 : start+end]
			if _, exists := ch.Parameters[paramName]; !exists {
				// Lookup in components.parameters if available
				if compParam, ok := doc.Components.Parameters[paramName]; ok {
					ch.Parameters[paramName] = compParam
				} else {
					ch.Parameters[paramName] = Parameter{
						Description: fmt.Sprintf("Channel address parameter %s", paramName),
						Schema:      Schema{Type: "string"},
					}
				}
			}

			addr = addr[start+end+1:]
		}

		doc.Channels[chKey] = ch
	}
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

	parts := strings.Split(s, "_")

	var resultSb strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		resultSb.WriteString(strings.ToUpper(p[:1]) + p[1:])
	}

	return resultSb.String()
}
