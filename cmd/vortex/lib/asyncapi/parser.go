// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package asyncapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseSpec parses, normalizes, and resolves traits for an AsyncAPI specification conforming to AsyncAPI 3.1.0.
//
// Reference: AsyncAPI 3.1.0 §Document Object (https://www.asyncapi.com/docs/concepts/asyncapi-document)
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

	// 2. Resolve traits merging mechanism (AsyncAPI 3.1.0 §Reusability with traits)
	applyTraits(&doc)

	// 3. Extract channel address parameters from dynamic templates ({param})
	extractAddressParameters(&doc)

	return &doc, nil
}

// LoadFile reads and parses an AsyncAPI spec file from disk.
func LoadFile(filename string) (*Document, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("reading asyncapi spec %s: %w", filename, err)
	}

	return ParseSpec(data)
}

func normalizeAsyncAPI2(doc *Document) {
	if doc == nil || doc.Channels == nil {
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
		// `publish` means application publishes to channel (client receives messages -> action: send)
		if ch.Publish != nil {
			opID := ch.Publish.OperationID
			if opID == "" {
				opID = "on" + sanitizeIdentifier(channelPath)
			}

			doc.Operations[opID] = Operation{
				Action:      "send",
				ChannelRef:  channelPath,
				Summary:     ch.Publish.Summary,
				Description: ch.Publish.Description,
			}
		}

		// `subscribe` means application subscribes to channel (client sends messages -> action: receive)
		if ch.Subscribe != nil {
			opID := ch.Subscribe.OperationID
			if opID == "" {
				opID = "send" + sanitizeIdentifier(channelPath)
			}

			doc.Operations[opID] = Operation{
				Action:      "receive",
				ChannelRef:  channelPath,
				Summary:     ch.Subscribe.Summary,
				Description: ch.Subscribe.Description,
			}
		}
	}
}

// applyTraits applies operationTraits and messageTraits according to the AsyncAPI 3.1.0 merging mechanism:
// Traits are merged into the target object without overriding already defined properties.
//
// Reference: AsyncAPI 3.1.0 §Trait Merging Mechanism (https://www.asyncapi.com/docs/concepts/asyncapi-document/reusability-with-traits#trait-merging-mechanism)
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
// Reference: AsyncAPI 3.1.0 §Parameters in Channel Address (https://www.asyncapi.com/docs/concepts/asyncapi-document/dynamic-channel-address)
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

		// Find placeholders enclosed in curly braces like {userId}
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
