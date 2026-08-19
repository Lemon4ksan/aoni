// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package asyncapi provides parsing, normalization, and declarative contract generation for AsyncAPI 2.x and 3.x specifications.
package asyncapi

// Document represents a normalized AsyncAPI document supporting both 2.x and 3.x schemas.
type Document struct {
	AsyncAPI   string               `json:"asyncapi"             yaml:"asyncapi"`
	Info       Info                 `json:"info"                 yaml:"info"`
	Servers    map[string]Server    `json:"servers,omitempty"    yaml:"servers,omitempty"`
	Channels   map[string]Channel   `json:"channels,omitempty"   yaml:"channels,omitempty"`
	Operations map[string]Operation `json:"operations,omitempty" yaml:"operations,omitempty"`
	Components Components           `json:"components,omitempty" yaml:"components,omitempty"`
}

// Info describes API title, version, and documentation metadata.
type Info struct {
	Title       string `json:"title"                 yaml:"title"`
	Version     string `json:"version"               yaml:"version"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// Server describes message broker or websocket host parameters.
type Server struct {
	Host        string               `json:"host,omitempty"        yaml:"host,omitempty"`
	Protocol    string               `json:"protocol"              yaml:"protocol"`
	Pathname    string               `json:"pathname,omitempty"    yaml:"pathname,omitempty"`
	Description string               `json:"description,omitempty" yaml:"description,omitempty"`
	Variables   map[string]ServerVar `json:"variables,omitempty"   yaml:"variables,omitempty"`
}

// ServerVar represents a templated server variable.
type ServerVar struct {
	Default string   `json:"default,omitempty" yaml:"default,omitempty"`
	Enum    []string `json:"enum,omitempty"    yaml:"enum,omitempty"`
}

// Channel represents an event address, websocket route, or topic.
type Channel struct {
	Address     string               `json:"address,omitempty"     yaml:"address,omitempty"`
	Messages    map[string]Message   `json:"messages,omitempty"    yaml:"messages,omitempty"`
	Parameters  map[string]Parameter `json:"parameters,omitempty"  yaml:"parameters,omitempty"`
	Description string               `json:"description,omitempty" yaml:"description,omitempty"`

	// AsyncAPI 2.x inline operations
	Publish   *Operation2 `json:"publish,omitempty"   yaml:"publish,omitempty"`
	Subscribe *Operation2 `json:"subscribe,omitempty" yaml:"subscribe,omitempty"`
}

// Operation represents an AsyncAPI 3.x action (send or receive).
type Operation struct {
	Action      string      `json:"action"                yaml:"action"` // "send" (app -> client = @event) or "receive" (client -> app = @ws:emit)
	ChannelRef  string      `json:"-"                     yaml:"-"`
	Channel     RefObject   `json:"channel,omitempty"     yaml:"channel,omitempty"`
	Messages    []RefObject `json:"messages,omitempty"    yaml:"messages,omitempty"`
	Summary     string      `json:"summary,omitempty"     yaml:"summary,omitempty"`
	Description string      `json:"description,omitempty" yaml:"description,omitempty"`
}

// Operation2 represents an AsyncAPI 2.x publish/subscribe block.
type Operation2 struct {
	OperationID string `json:"operationId,omitempty" yaml:"operationId,omitempty"`
	Summary     string `json:"summary,omitempty"     yaml:"summary,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Message     any    `json:"message,omitempty"     yaml:"message,omitempty"`
}

// Parameter models templated channel parameters (e.g. {symbol}).
type Parameter struct {
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	Schema      Schema   `json:"schema,omitempty"      yaml:"schema,omitempty"`
	Enum        []string `json:"enum,omitempty"        yaml:"enum,omitempty"`
}

// Message represents a payload message schema definition.
type Message struct {
	Name        string `json:"name,omitempty"        yaml:"name,omitempty"`
	Title       string `json:"title,omitempty"       yaml:"title,omitempty"`
	Summary     string `json:"summary,omitempty"     yaml:"summary,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Payload     any    `json:"payload,omitempty"     yaml:"payload,omitempty"`
	Ref         string `json:"$ref,omitempty"        yaml:"$ref,omitempty"`
}

// Components stores reusable schemas, messages, and parameters.
type Components struct {
	Messages   map[string]Message   `json:"messages,omitempty"   yaml:"messages,omitempty"`
	Schemas    map[string]Schema    `json:"schemas,omitempty"    yaml:"schemas,omitempty"`
	Parameters map[string]Parameter `json:"parameters,omitempty" yaml:"parameters,omitempty"`
}

// Schema represents a JSON Schema definition for DTO models.
type Schema struct {
	Type        string            `json:"type,omitempty"        yaml:"type,omitempty"`
	Format      string            `json:"format,omitempty"      yaml:"format,omitempty"`
	Description string            `json:"description,omitempty" yaml:"description,omitempty"`
	Properties  map[string]Schema `json:"properties,omitempty"  yaml:"properties,omitempty"`
	Required    []string          `json:"required,omitempty"    yaml:"required,omitempty"`
	Items       *Schema           `json:"items,omitempty"       yaml:"items,omitempty"`
	Ref         string            `json:"$ref,omitempty"        yaml:"$ref,omitempty"`
	Enum        []any             `json:"enum,omitempty"        yaml:"enum,omitempty"`
}

// RefObject captures generic $ref wrappers.
type RefObject struct {
	Ref string `json:"$ref,omitempty" yaml:"$ref,omitempty"`
}
