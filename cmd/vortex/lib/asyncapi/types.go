// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package asyncapi provides parsing, normalization, and declarative contract generation
// for AsyncAPI 2.x and 3.x specifications conforming to the AsyncAPI 3.1.0 standard.
//
// Reference:
//   - AsyncAPI 3.1.0 Specification: https://www.asyncapi.com/docs/reference/specification/v3.1.0
package asyncapi

// Document represents a normalized AsyncAPI document conforming to the AsyncAPI 3.1.0 specification.
//
// Reference: AsyncAPI 3.1.0 §Document Object (https://www.asyncapi.com/docs/concepts/asyncapi-document)
type Document struct {
	AsyncAPI     string               `json:"asyncapi"               yaml:"asyncapi"`
	ID           string               `json:"id,omitempty"          yaml:"id,omitempty"`
	Info         Info                 `json:"info"                   yaml:"info"`
	Servers      map[string]Server    `json:"servers,omitempty"      yaml:"servers,omitempty"`
	Channels     map[string]Channel   `json:"channels,omitempty"     yaml:"channels,omitempty"`
	Operations   map[string]Operation `json:"operations,omitempty"   yaml:"operations,omitempty"`
	Components   Components           `json:"components,omitempty"   yaml:"components,omitempty"`
	Tags         []Tag                `json:"tags,omitempty"         yaml:"tags,omitempty"`
	ExternalDocs *ExternalDocs        `json:"externalDocs,omitempty" yaml:"externalDocs,omitempty"`
}

// Info describes API title, version, terms of service, and contact/license metadata.
//
// Reference: AsyncAPI 3.1.0 §Info Object (https://www.asyncapi.com/docs/concepts/asyncapi-document/structure#info-field)
type Info struct {
	Title          string        `json:"title"                    yaml:"title"`
	Version        string        `json:"version"                  yaml:"version"`
	Description    string        `json:"description,omitempty"    yaml:"description,omitempty"`
	TermsOfService string        `json:"termsOfService,omitempty" yaml:"termsOfService,omitempty"`
	Contact        *Contact      `json:"contact,omitempty"       yaml:"contact,omitempty"`
	License        *License      `json:"license,omitempty"       yaml:"license,omitempty"`
	Tags           []Tag         `json:"tags,omitempty"          yaml:"tags,omitempty"`
	ExternalDocs   *ExternalDocs `json:"externalDocs,omitempty"   yaml:"externalDocs,omitempty"`
}

// Contact contains contact information for the exposed API.
//
// Reference: AsyncAPI 3.1.0 §Contact Object
type Contact struct {
	Name  string `json:"name,omitempty"  yaml:"name,omitempty"`
	URL   string `json:"url,omitempty"   yaml:"url,omitempty"`
	Email string `json:"email,omitempty" yaml:"email,omitempty"`
}

// License describes license metadata for the exposed API.
//
// Reference: AsyncAPI 3.1.0 §License Object
type License struct {
	Name string `json:"name"          yaml:"name"`
	URL  string `json:"url,omitempty" yaml:"url,omitempty"`
}

// Server describes message broker or websocket host parameters and security requirements.
//
// Reference: AsyncAPI 3.1.0 §Server Object (https://www.asyncapi.com/docs/concepts/asyncapi-document/add-server)
type Server struct {
	Host            string               `json:"host,omitempty"            yaml:"host,omitempty"`
	Protocol        string               `json:"protocol"                  yaml:"protocol"`
	ProtocolVersion string               `json:"protocolVersion,omitempty" yaml:"protocolVersion,omitempty"`
	Pathname        string               `json:"pathname,omitempty"        yaml:"pathname,omitempty"`
	Description     string               `json:"description,omitempty"     yaml:"description,omitempty"`
	Title           string               `json:"title,omitempty"           yaml:"title,omitempty"`
	Summary         string               `json:"summary,omitempty"         yaml:"summary,omitempty"`
	Variables       map[string]ServerVar `json:"variables,omitempty"       yaml:"variables,omitempty"`
	Security        []RefObject          `json:"security,omitempty"        yaml:"security,omitempty"`
	Tags            []Tag                `json:"tags,omitempty"            yaml:"tags,omitempty"`
	ExternalDocs    *ExternalDocs        `json:"externalDocs,omitempty"    yaml:"externalDocs,omitempty"`
	Bindings        map[string]any       `json:"bindings,omitempty"        yaml:"bindings,omitempty"`

	// Legacy 2.x url alias
	URL string `json:"url,omitempty" yaml:"url,omitempty"`
}

// ServerVar represents a templated server variable (e.g. {subdomain}.example.com:{port}).
//
// Reference: AsyncAPI 3.1.0 §Server Variable Object (https://www.asyncapi.com/docs/concepts/asyncapi-document/variable-url)
type ServerVar struct {
	Default     string   `json:"default,omitempty"     yaml:"default,omitempty"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"        yaml:"enum,omitempty"`
	Examples    []string `json:"examples,omitempty"    yaml:"examples,omitempty"`
}

// Channel represents an event address, websocket route, or message broker topic.
//
// Reference: AsyncAPI 3.1.0 §Channel Object (https://www.asyncapi.com/docs/concepts/asyncapi-document/adding-channels)
type Channel struct {
	Address      string               `json:"address,omitempty"      yaml:"address,omitempty"`
	Messages     map[string]Message   `json:"messages,omitempty"     yaml:"messages,omitempty"`
	Parameters   map[string]Parameter `json:"parameters,omitempty"   yaml:"parameters,omitempty"`
	Servers      []RefObject          `json:"servers,omitempty"      yaml:"servers,omitempty"`
	Title        string               `json:"title,omitempty"        yaml:"title,omitempty"`
	Summary      string               `json:"summary,omitempty"      yaml:"summary,omitempty"`
	Description  string               `json:"description,omitempty"  yaml:"description,omitempty"`
	Tags         []Tag                `json:"tags,omitempty"         yaml:"tags,omitempty"`
	ExternalDocs *ExternalDocs        `json:"externalDocs,omitempty" yaml:"externalDocs,omitempty"`
	Bindings     map[string]any       `json:"bindings,omitempty"     yaml:"bindings,omitempty"`

	// AsyncAPI 2.x inline operations
	Publish   *Operation2 `json:"publish,omitempty"   yaml:"publish,omitempty"`
	Subscribe *Operation2 `json:"subscribe,omitempty" yaml:"subscribe,omitempty"`
}

// Parameter models dynamic variables in channel addresses (e.g. users/{userId} or sensors/{streetlightId}).
//
// Reference: AsyncAPI 3.1.0 §Parameter Object (https://www.asyncapi.com/docs/concepts/asyncapi-document/dynamic-channel-address)
type Parameter struct {
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	Schema      Schema   `json:"schema,omitempty"      yaml:"schema,omitempty"`
	Enum        []string `json:"enum,omitempty"        yaml:"enum,omitempty"`
	Default     string   `json:"default,omitempty"     yaml:"default,omitempty"`
	Location    string   `json:"location,omitempty"    yaml:"location,omitempty"`
}

// Operation represents an AsyncAPI 3.x action (send or receive), optional reply, and traits.
//
// Reference: AsyncAPI 3.1.0 §Operation Object (https://www.asyncapi.com/docs/concepts/asyncapi-document/adding-operations)
type Operation struct {
	Action       string          `json:"action"                 yaml:"action"` // "send" (app -> client = @event) or "receive" (client -> app = @ws:emit)
	ChannelRef   string          `json:"-"                      yaml:"-"`
	Channel      RefObject       `json:"channel,omitempty"      yaml:"channel,omitempty"`
	Messages     []RefObject     `json:"messages,omitempty"     yaml:"messages,omitempty"`
	Reply        *OperationReply `json:"reply,omitempty"        yaml:"reply,omitempty"`
	Title        string          `json:"title,omitempty"        yaml:"title,omitempty"`
	Summary      string          `json:"summary,omitempty"      yaml:"summary,omitempty"`
	Description  string          `json:"description,omitempty"  yaml:"description,omitempty"`
	Security     []RefObject     `json:"security,omitempty"     yaml:"security,omitempty"`
	Tags         []Tag           `json:"tags,omitempty"         yaml:"tags,omitempty"`
	ExternalDocs *ExternalDocs   `json:"externalDocs,omitempty" yaml:"externalDocs,omitempty"`
	Bindings     map[string]any  `json:"bindings,omitempty"     yaml:"bindings,omitempty"`
	Traits       []RefObject     `json:"traits,omitempty"       yaml:"traits,omitempty"`
}

// OperationReply represents a request/reply asynchronous interaction pattern.
//
// Reference: AsyncAPI 3.1.0 §Operation Reply Object (https://www.asyncapi.com/docs/concepts/asyncapi-document/reply-info)
type OperationReply struct {
	Address  *OperationReplyAddress `json:"address,omitempty"  yaml:"address,omitempty"`
	Channel  *RefObject             `json:"channel,omitempty"  yaml:"channel,omitempty"`
	Messages []RefObject            `json:"messages,omitempty" yaml:"messages,omitempty"`
}

// OperationReplyAddress represents the dynamic return address destination (e.g. $message.header#/replyTo).
//
// Reference: AsyncAPI 3.1.0 §Operation Reply Address Object
type OperationReplyAddress struct {
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Location    string `json:"location,omitempty"    yaml:"location,omitempty"`
}

// Operation2 represents an AsyncAPI 2.x publish/subscribe block for backward compatibility.
type Operation2 struct {
	OperationID string `json:"operationId,omitempty" yaml:"operationId,omitempty"`
	Summary     string `json:"summary,omitempty"     yaml:"summary,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Message     any    `json:"message,omitempty"     yaml:"message,omitempty"`
}

// Message represents a payload message schema, headers, traits, and correlation ID.
//
// Reference: AsyncAPI 3.1.0 §Message Object (https://www.asyncapi.com/docs/concepts/asyncapi-document/adding-messages)
type Message struct {
	Name          string         `json:"name,omitempty"          yaml:"name,omitempty"`
	Title         string         `json:"title,omitempty"         yaml:"title,omitempty"`
	Summary       string         `json:"summary,omitempty"       yaml:"summary,omitempty"`
	Description   string         `json:"description,omitempty"   yaml:"description,omitempty"`
	ContentType   string         `json:"contentType,omitempty"   yaml:"contentType,omitempty"`
	Headers       *Schema        `json:"headers,omitempty"       yaml:"headers,omitempty"`
	Payload       any            `json:"payload,omitempty"       yaml:"payload,omitempty"`
	CorrelationID *CorrelationID `json:"correlationId,omitempty" yaml:"correlationId,omitempty"`
	Traits        []RefObject    `json:"traits,omitempty"        yaml:"traits,omitempty"`
	Bindings      map[string]any `json:"bindings,omitempty"      yaml:"bindings,omitempty"`
	Tags          []Tag          `json:"tags,omitempty"          yaml:"tags,omitempty"`
	ExternalDocs  *ExternalDocs  `json:"externalDocs,omitempty"  yaml:"externalDocs,omitempty"`
	Examples      []any          `json:"examples,omitempty"      yaml:"examples,omitempty"`
	Ref           string         `json:"$ref,omitempty"          yaml:"$ref,omitempty"`
}

// CorrelationID specifies an identifier used for message tracing and request-response correlation.
//
// Reference: AsyncAPI 3.1.0 §Correlation ID Object
type CorrelationID struct {
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Location    string `json:"location"              yaml:"location"`
}

// SecurityScheme defines a security scheme for servers or operations (e.g. bearer, apiKey, oauth2, scramSha256).
//
// Reference: AsyncAPI 3.1.0 §Security Scheme Object (https://www.asyncapi.com/docs/concepts/asyncapi-document/server-security)
type SecurityScheme struct {
	Type             string `json:"type"                       yaml:"type"`
	Description      string `json:"description,omitempty"      yaml:"description,omitempty"`
	Name             string `json:"name,omitempty"             yaml:"name,omitempty"`
	In               string `json:"in,omitempty"               yaml:"in,omitempty"`
	Scheme           string `json:"scheme,omitempty"           yaml:"scheme,omitempty"`
	BearerFormat     string `json:"bearerFormat,omitempty"     yaml:"bearerFormat,omitempty"`
	OpenIDConnectURL string `json:"openIdConnectUrl,omitempty" yaml:"openIdConnectUrl,omitempty"`
}

// Tag represents a categorization label for API entities.
//
// Reference: AsyncAPI 3.1.0 §Tag Object (https://www.asyncapi.com/docs/concepts/asyncapi-document/tags)
type Tag struct {
	Name         string        `json:"name"                   yaml:"name"`
	Description  string        `json:"description,omitempty" yaml:"description,omitempty"`
	ExternalDocs *ExternalDocs `json:"externalDocs,omitempty" yaml:"externalDocs,omitempty"`
}

// ExternalDocs provides a link to extended documentation.
//
// Reference: AsyncAPI 3.1.0 §External Documentation Object
type ExternalDocs struct {
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	URL         string `json:"url"                   yaml:"url"`
}

// Components stores reusable schemas, messages, parameters, security schemes, and traits.
//
// Reference: AsyncAPI 3.1.0 §Components Object (https://www.asyncapi.com/docs/concepts/asyncapi-document/structure#components-field)
type Components struct {
	Messages        map[string]Message        `json:"messages,omitempty"        yaml:"messages,omitempty"`
	Schemas         map[string]Schema         `json:"schemas,omitempty"         yaml:"schemas,omitempty"`
	Parameters      map[string]Parameter      `json:"parameters,omitempty"      yaml:"parameters,omitempty"`
	SecuritySchemes map[string]SecurityScheme `json:"securitySchemes,omitempty" yaml:"securitySchemes,omitempty"`
	ServerVariables map[string]ServerVar      `json:"serverVariables,omitempty" yaml:"serverVariables,omitempty"`
	CorrelationIDs  map[string]CorrelationID  `json:"correlationIds,omitempty"  yaml:"correlationIds,omitempty"`
	Replies         map[string]OperationReply `json:"replies,omitempty"         yaml:"replies,omitempty"`
	OperationTraits map[string]Operation      `json:"operationTraits,omitempty" yaml:"operationTraits,omitempty"`
	MessageTraits   map[string]Message        `json:"messageTraits,omitempty"   yaml:"messageTraits,omitempty"`
	Tags            map[string]Tag            `json:"tags,omitempty"            yaml:"tags,omitempty"`
}

// Schema represents a JSON Schema definition for message DTO models.
//
// Reference: AsyncAPI 3.1.0 §Schema Object (https://www.asyncapi.com/docs/concepts/asyncapi-document/define-payload)
type Schema struct {
	Type                 string            `json:"type,omitempty"                 yaml:"type,omitempty"`
	Format               string            `json:"format,omitempty"               yaml:"format,omitempty"`
	Title                string            `json:"title,omitempty"                yaml:"title,omitempty"`
	Description          string            `json:"description,omitempty"          yaml:"description,omitempty"`
	Properties           map[string]Schema `json:"properties,omitempty"           yaml:"properties,omitempty"`
	Required             []string          `json:"required,omitempty"             yaml:"required,omitempty"`
	Items                *Schema           `json:"items,omitempty"                yaml:"items,omitempty"`
	Ref                  string            `json:"$ref,omitempty"                 yaml:"$ref,omitempty"`
	Enum                 []any             `json:"enum,omitempty"                 yaml:"enum,omitempty"`
	Default              any               `json:"default,omitempty"              yaml:"default,omitempty"`
	AdditionalProperties any               `json:"additionalProperties,omitempty" yaml:"additionalProperties,omitempty"`
}

// RefObject captures generic $ref wrappers.
//
// Reference: AsyncAPI 3.1.0 §Reference Object (https://www.asyncapi.com/docs/concepts/asyncapi-document/reusable-parts)
type RefObject struct {
	Ref string `json:"$ref,omitempty" yaml:"$ref,omitempty"`
}
