// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package jsbundle

// Endpoint describes an API route or RPC procedure discovered in JavaScript source code.
type Endpoint struct {
	Path         string            `json:"path"`
	HTTPMethod   string            `json:"http_method"`
	Protocol     string            `json:"protocol"` // "rest", "grpc-web", "twirp", "trpc", "graphql"
	Headers      map[string]string `json:"headers,omitempty"`
	RequestType  string            `json:"request_type,omitempty"`
	ResponseType string            `json:"response_type,omitempty"`
}

// FieldDescriptor describes a discovered positional or named struct field.
type FieldDescriptor struct {
	Index      int    `json:"index"`
	Name       string `json:"name"`
	GoType     string `json:"go_type"`
	IsNested   bool   `json:"is_nested"`
	SubMsgType string `json:"sub_msg_type,omitempty"`
	IsArray    bool   `json:"is_array"`
}

// MessageDescriptor describes a discovered data structure (Protobuf, JSPB, DTO class).
type MessageDescriptor struct {
	ID        string                  `json:"id"`
	Name      string                  `json:"name"`
	Fields    map[int]FieldDescriptor `json:"fields"`
	SourceRef string                  `json:"source_ref,omitempty"`
}

// EnumDescriptor describes a discovered enumeration mapping.
type EnumDescriptor struct {
	Name   string         `json:"name"`
	Values map[int]string `json:"values"`
}

// ScanResult aggregates all discovered endpoints, messages, and enums from scanned JS files.
type ScanResult struct {
	Endpoints []Endpoint                    `json:"endpoints"`
	Messages  map[string]*MessageDescriptor `json:"messages"`
	Enums     map[string]*EnumDescriptor    `json:"enums"`
}

// NewScanResult initializes an empty ScanResult container.
func NewScanResult() *ScanResult {
	return &ScanResult{
		Endpoints: make([]Endpoint, 0),
		Messages:  make(map[string]*MessageDescriptor),
		Enums:     make(map[string]*EnumDescriptor),
	}
}

// Merge combines another ScanResult into the current result.
func (r *ScanResult) Merge(other *ScanResult) {
	if other == nil {
		return
	}

	r.Endpoints = append(r.Endpoints, other.Endpoints...)

	for id, msg := range other.Messages {
		if existing, ok := r.Messages[id]; ok {
			for idx, f := range msg.Fields {
				if _, exists := existing.Fields[idx]; !exists {
					existing.Fields[idx] = f
				}
			}
		} else {
			r.Messages[id] = msg
		}
	}

	for name, enum := range other.Enums {
		if _, ok := r.Enums[name]; !ok {
			r.Enums[name] = enum
		}
	}
}
