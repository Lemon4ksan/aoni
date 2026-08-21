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

// ParseSpec parses and normalizes an AsyncAPI specification from JSON or YAML bytes.
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

	// Normalize AsyncAPI 2.x publish/subscribe channels into 3.x operations model
	normalizeAsyncAPI2(&doc)

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

func sanitizeIdentifier(s string) string {
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "{", "")
	s = strings.ReplaceAll(s, "}", "")
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, ".", "_")
	s = strings.Trim(s, "_")

	parts := strings.Split(s, "_")

	var (
		result      string
		resultSb116 strings.Builder
	)

	for _, p := range parts {
		if p == "" {
			continue
		}

		resultSb116.WriteString(strings.ToUpper(p[:1]) + p[1:])
	}

	result += resultSb116.String()

	return result
}
