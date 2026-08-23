// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package asyncapi

// Document represents a normalized AsyncAPI document supporting both 2.x and 3.x schemas.
//
// # References
//   - AsyncAPI 3.1.0 §AsyncAPI Object: https://www.asyncapi.com/docs/reference/specification/v3.1.0#asyncapi-object
//   - AsyncAPI 2.6.0 §AsyncAPI Object: https://v2.asyncapi.com/docs/reference/specification/v2.6.0#asyncapiObject
type Document struct {
	AsyncAPI           string               `json:"asyncapi"                     yaml:"asyncapi"`
	ID                 string               `json:"id,omitempty"                 yaml:"id,omitempty"`
	Info               Info                 `json:"info"                         yaml:"info"`
	Servers            map[string]Server    `json:"servers,omitempty"            yaml:"servers,omitempty"`
	DefaultContentType string               `json:"defaultContentType,omitempty" yaml:"defaultContentType,omitempty"`
	Channels           map[string]Channel   `json:"channels,omitempty"           yaml:"channels,omitempty"`
	Operations         map[string]Operation `json:"operations,omitempty"         yaml:"operations,omitempty"`
	Components         Components           `json:"components,omitempty"         yaml:"components,omitempty"`
	Tags               []Tag                `json:"tags,omitempty"               yaml:"tags,omitempty"`
	ExternalDocs       *ExternalDocs        `json:"externalDocs,omitempty"       yaml:"externalDocs,omitempty"`
}

// Info describes API title, version, terms of service, and contact/license metadata.
//
// # References
//   - AsyncAPI 3.1.0 §Info Object: https://www.asyncapi.com/docs/reference/specification/v3.1.0#info-object
//   - AsyncAPI 2.6.0 §Info Object: https://v2.asyncapi.com/docs/reference/specification/v2.6.0#infoObject
type Info struct {
	Title          string        `json:"title"                    yaml:"title"`
	Version        string        `json:"version"                  yaml:"version"`
	Description    string        `json:"description,omitempty"    yaml:"description,omitempty"`
	TermsOfService string        `json:"termsOfService,omitempty" yaml:"termsOfService,omitempty"`
	Contact        *Contact      `json:"contact,omitempty"        yaml:"contact,omitempty"`
	License        *License      `json:"license,omitempty"        yaml:"license,omitempty"`
	Tags           []Tag         `json:"tags,omitempty"           yaml:"tags,omitempty"`
	ExternalDocs   *ExternalDocs `json:"externalDocs,omitempty"   yaml:"externalDocs,omitempty"`
}

// Contact contains contact information for the exposed API.
//
// # References
//   - AsyncAPI 3.1.0 §Contact Object: https://www.asyncapi.com/docs/reference/specification/v3.1.0#contact-object
//   - AsyncAPI 2.6.0 §Contact Object: https://v2.asyncapi.com/docs/reference/specification/v2.6.0#contactObject
type Contact struct {
	Name  string `json:"name,omitempty"  yaml:"name,omitempty"`
	URL   string `json:"url,omitempty"   yaml:"url,omitempty"`
	Email string `json:"email,omitempty" yaml:"email,omitempty"`
}

// License describes license metadata for the exposed API.
//
// # References
//   - AsyncAPI 3.1.0 §License Object: https://www.asyncapi.com/docs/reference/specification/v3.1.0#license-object
//   - AsyncAPI 2.6.0 §License Object: https://v2.asyncapi.com/docs/reference/specification/v2.6.0#licenseObject
type License struct {
	Name string `json:"name"          yaml:"name"`
	URL  string `json:"url,omitempty" yaml:"url,omitempty"`
}

// Server describes message broker or websocket host parameters and security requirements.
//
// # References
//   - AsyncAPI 3.1.0 §Server Object: https://www.asyncapi.com/docs/reference/specification/v3.1.0#server-object
//   - AsyncAPI 2.6.0 §Server Object: https://v2.asyncapi.com/docs/reference/specification/v2.6.0#serverObject
//   - RFC 6455 §The WebSocket Protocol: https://datatracker.ietf.org/doc/html/rfc6455
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

	// AsyncAPI 2.6.0 url field
	URL string `json:"url,omitempty" yaml:"url,omitempty"`
}

// ServerVar represents a templated server variable (e.g. {subdomain}.example.com:{port}).
//
// # References
//   - AsyncAPI 3.1.0 §Server Variable Object: https://www.asyncapi.com/docs/reference/specification/v3.1.0#server-variable-object
//   - AsyncAPI 2.6.0 §Server Variable Object: https://v2.asyncapi.com/docs/reference/specification/v2.6.0#serverVariableObject
//   - RFC 6570 §URI Template: https://datatracker.ietf.org/doc/html/rfc6570
type ServerVar struct {
	Default     string   `json:"default,omitempty"     yaml:"default,omitempty"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"        yaml:"enum,omitempty"`
	Examples    []string `json:"examples,omitempty"    yaml:"examples,omitempty"`
}

// Channel represents an event address, websocket route, or message broker topic.
//
// # References
//   - AsyncAPI 3.1.0 §Channel Object: https://www.asyncapi.com/docs/reference/specification/v3.1.0#channel-object
//   - AsyncAPI 2.6.0 §Channel Item Object: https://v2.asyncapi.com/docs/reference/specification/v2.6.0#channelItemObject
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

	// AsyncAPI 2.6.0 inline publish & subscribe operations
	Publish   *Operation2 `json:"publish,omitempty"   yaml:"publish,omitempty"`
	Subscribe *Operation2 `json:"subscribe,omitempty" yaml:"subscribe,omitempty"`
}

// Parameter models dynamic variables in channel addresses (e.g. users/{userId} or sensors/{streetlightId}).
//
// # References
//   - AsyncAPI 3.1.0 §Parameter Object: https://www.asyncapi.com/docs/reference/specification/v3.1.0#parameter-object
//   - AsyncAPI 2.6.0 §Parameter Object: https://v2.asyncapi.com/docs/reference/specification/v2.6.0#parameterObject
//   - RFC 6570 §URI Template: https://datatracker.ietf.org/doc/html/rfc6570
type Parameter struct {
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	Schema      Schema   `json:"schema,omitempty"      yaml:"schema,omitempty"`
	Enum        []string `json:"enum,omitempty"        yaml:"enum,omitempty"`
	Default     string   `json:"default,omitempty"     yaml:"default,omitempty"`
	Examples    []string `json:"examples,omitempty"    yaml:"examples,omitempty"`
	Location    string   `json:"location,omitempty"    yaml:"location,omitempty"`
}

// Operation represents an AsyncAPI 3.x action (send or receive), optional reply, and traits.
//
// # References
//   - AsyncAPI 3.1.0 §Operation Object: https://www.asyncapi.com/docs/reference/specification/v3.1.0#operation-object
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
// # References
//   - AsyncAPI 3.1.0 §Operation Reply Object: https://www.asyncapi.com/docs/reference/specification/v3.1.0#operation-reply-object
type OperationReply struct {
	Address  *OperationReplyAddress `json:"address,omitempty"  yaml:"address,omitempty"`
	Channel  *RefObject             `json:"channel,omitempty"  yaml:"channel,omitempty"`
	Messages []RefObject            `json:"messages,omitempty" yaml:"messages,omitempty"`
}

// OperationReplyAddress represents the dynamic return address destination (e.g. $message.header#/replyTo).
//
// # References
//   - AsyncAPI 3.1.0 §Operation Reply Address Object: https://www.asyncapi.com/docs/reference/specification/v3.1.0#operation-reply-address-object
type OperationReplyAddress struct {
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Location    string `json:"location"              yaml:"location"`
}

// Operation2 represents an AsyncAPI 2.x publish/subscribe operation block on a Channel Item.
//
// # References
//   - AsyncAPI 2.6.0 §Operation Object: https://v2.asyncapi.com/docs/reference/specification/v2.6.0#operationObject
type Operation2 struct {
	OperationID  string         `json:"operationId,omitempty"  yaml:"operationId,omitempty"`
	Summary      string         `json:"summary,omitempty"      yaml:"summary,omitempty"`
	Description  string         `json:"description,omitempty"  yaml:"description,omitempty"`
	Tags         []Tag          `json:"tags,omitempty"         yaml:"tags,omitempty"`
	ExternalDocs *ExternalDocs  `json:"externalDocs,omitempty" yaml:"externalDocs,omitempty"`
	Bindings     map[string]any `json:"bindings,omitempty"     yaml:"bindings,omitempty"`
	Traits       []RefObject    `json:"traits,omitempty"       yaml:"traits,omitempty"`
	Message      any            `json:"message,omitempty"      yaml:"message,omitempty"`
}

// Message represents a payload message schema, headers, traits, and correlation ID.
//
// # References
//   - AsyncAPI 3.1.0 §Message Object: https://www.asyncapi.com/docs/reference/specification/v3.1.0#message-object
//   - AsyncAPI 2.6.0 §Message Object: https://v2.asyncapi.com/docs/reference/specification/v2.6.0#messageObject
type Message struct {
	MessageID     string           `json:"messageId,omitempty"     yaml:"messageId,omitempty"` // AsyncAPI 2.6.0 messageId
	Name          string           `json:"name,omitempty"          yaml:"name,omitempty"`
	Title         string           `json:"title,omitempty"         yaml:"title,omitempty"`
	Summary       string           `json:"summary,omitempty"       yaml:"summary,omitempty"`
	Description   string           `json:"description,omitempty"   yaml:"description,omitempty"`
	ContentType   string           `json:"contentType,omitempty"   yaml:"contentType,omitempty"`
	SchemaFormat  string           `json:"schemaFormat,omitempty"  yaml:"schemaFormat,omitempty"`
	Headers       *Schema          `json:"headers,omitempty"       yaml:"headers,omitempty"`
	Payload       any              `json:"payload,omitempty"       yaml:"payload,omitempty"`
	CorrelationID *CorrelationID   `json:"correlationId,omitempty" yaml:"correlationId,omitempty"`
	Traits        []RefObject      `json:"traits,omitempty"        yaml:"traits,omitempty"`
	Bindings      map[string]any   `json:"bindings,omitempty"      yaml:"bindings,omitempty"`
	Tags          []Tag            `json:"tags,omitempty"          yaml:"tags,omitempty"`
	ExternalDocs  *ExternalDocs    `json:"externalDocs,omitempty"  yaml:"externalDocs,omitempty"`
	Examples      []MessageExample `json:"examples,omitempty"      yaml:"examples,omitempty"`
	Ref           string           `json:"$ref,omitempty"          yaml:"$ref,omitempty"`
}

// MessageExample represents an example payload and/or headers of a Message Object.
//
// # References
//   - AsyncAPI 3.1.0 §Message Example Object: https://www.asyncapi.com/docs/reference/specification/v3.1.0#message-example-object
//   - AsyncAPI 2.6.0 §Message Example Object: https://v2.asyncapi.com/docs/reference/specification/v2.6.0#messageExampleObject
type MessageExample struct {
	Name    string         `json:"name,omitempty"    yaml:"name,omitempty"`
	Summary string         `json:"summary,omitempty" yaml:"summary,omitempty"`
	Headers map[string]any `json:"headers,omitempty" yaml:"headers,omitempty"`
	Payload any            `json:"payload,omitempty" yaml:"payload,omitempty"`
}

// CorrelationID specifies an identifier used for message tracing and request-response correlation.
//
// # References
//   - AsyncAPI 3.1.0 §Correlation ID Object: https://www.asyncapi.com/docs/reference/specification/v3.1.0#correlation-id-object
//   - AsyncAPI 2.6.0 §Correlation ID Object: https://v2.asyncapi.com/docs/reference/specification/v2.6.0#correlationIdObject
type CorrelationID struct {
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Location    string `json:"location"              yaml:"location"`
}

// SecurityScheme defines a security scheme for servers or operations (e.g. bearer, apiKey, oauth2, scramSha256).
//
// # References
//   - AsyncAPI 3.1.0 §Security Scheme Object: https://www.asyncapi.com/docs/reference/specification/v3.1.0#security-scheme-object
//   - AsyncAPI 2.6.0 §Security Scheme Object: https://v2.asyncapi.com/docs/reference/specification/v2.6.0#securitySchemeObject
//   - RFC 6749 §The OAuth 2.0 Authorization Framework: https://datatracker.ietf.org/doc/html/rfc6749
//   - RFC 7235 §HTTP Authentication: https://datatracker.ietf.org/doc/html/rfc7235
type SecurityScheme struct {
	Type             string      `json:"type"                       yaml:"type"`
	Description      string      `json:"description,omitempty"      yaml:"description,omitempty"`
	Name             string      `json:"name,omitempty"             yaml:"name,omitempty"`
	In               string      `json:"in,omitempty"               yaml:"in,omitempty"`
	Scheme           string      `json:"scheme,omitempty"           yaml:"scheme,omitempty"`
	BearerFormat     string      `json:"bearerFormat,omitempty"     yaml:"bearerFormat,omitempty"`
	Flows            *OAuthFlows `json:"flows,omitempty"            yaml:"flows,omitempty"`
	OpenIDConnectURL string      `json:"openIdConnectUrl,omitempty" yaml:"openIdConnectUrl,omitempty"`
	Scopes           []string    `json:"scopes,omitempty"           yaml:"scopes,omitempty"`
}

// OAuthFlows allows configuration of supported OAuth Flows.
//
// # References
//   - AsyncAPI 3.1.0 §OAuth Flows Object: https://www.asyncapi.com/docs/reference/specification/v3.1.0#oauth-flows-object
//   - AsyncAPI 2.6.0 §OAuth Flows Object: https://v2.asyncapi.com/docs/reference/specification/v2.6.0#oauthFlowsObject
type OAuthFlows struct {
	Implicit          *OAuthFlow `json:"implicit,omitempty"          yaml:"implicit,omitempty"`
	Password          *OAuthFlow `json:"password,omitempty"          yaml:"password,omitempty"`
	ClientCredentials *OAuthFlow `json:"clientCredentials,omitempty" yaml:"clientCredentials,omitempty"`
	AuthorizationCode *OAuthFlow `json:"authorizationCode,omitempty" yaml:"authorizationCode,omitempty"`
}

// OAuthFlow configuration details for a supported OAuth Flow.
//
// # References
//   - AsyncAPI 3.1.0 §OAuth Flow Object: https://www.asyncapi.com/docs/reference/specification/v3.1.0#oauth-flow-object
//   - AsyncAPI 2.6.0 §OAuth Flow Object: https://v2.asyncapi.com/docs/reference/specification/v2.6.0#oauthFlowObject
type OAuthFlow struct {
	AuthorizationURL string            `json:"authorizationUrl,omitempty" yaml:"authorizationUrl,omitempty"`
	TokenURL         string            `json:"tokenUrl,omitempty"         yaml:"tokenUrl,omitempty"`
	RefreshURL       string            `json:"refreshUrl,omitempty"       yaml:"refreshUrl,omitempty"`
	AvailableScopes  map[string]string `json:"availableScopes,omitempty"  yaml:"availableScopes,omitempty"`
	Scopes           map[string]string `json:"scopes,omitempty"           yaml:"scopes,omitempty"`
}

// Tag represents a categorization label for API entities.
//
// # References
//   - AsyncAPI 3.1.0 §Tag Object: https://www.asyncapi.com/docs/reference/specification/v3.1.0#tag-object
//   - AsyncAPI 2.6.0 §Tag Object: https://v2.asyncapi.com/docs/reference/specification/v2.6.0#tagObject
type Tag struct {
	Name         string        `json:"name"                   yaml:"name"`
	Description  string        `json:"description,omitempty"  yaml:"description,omitempty"`
	ExternalDocs *ExternalDocs `json:"externalDocs,omitempty" yaml:"externalDocs,omitempty"`
}

// ExternalDocs provides a link to extended documentation.
//
// # References
//   - AsyncAPI 3.1.0 §External Documentation Object: https://www.asyncapi.com/docs/reference/specification/v3.1.0#external-documentation-object
//   - AsyncAPI 2.6.0 §External Documentation Object: https://v2.asyncapi.com/docs/reference/specification/v2.6.0#externalDocumentationObject
type ExternalDocs struct {
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	URL         string `json:"url"                   yaml:"url"`
}

// Components stores reusable schemas, messages, parameters, security schemes, and traits.
//
// # References
//   - AsyncAPI 3.1.0 §Components Object: https://www.asyncapi.com/docs/reference/specification/v3.1.0#components-object
//   - AsyncAPI 2.6.0 §Components Object: https://v2.asyncapi.com/docs/reference/specification/v2.6.0#componentsObject
type Components struct {
	Messages          map[string]Message               `json:"messages,omitempty"          yaml:"messages,omitempty"`
	Schemas           map[string]Schema                `json:"schemas,omitempty"           yaml:"schemas,omitempty"`
	Servers           map[string]Server                `json:"servers,omitempty"           yaml:"servers,omitempty"`
	Channels          map[string]Channel               `json:"channels,omitempty"          yaml:"channels,omitempty"`
	Operations        map[string]Operation             `json:"operations,omitempty"        yaml:"operations,omitempty"`
	Parameters        map[string]Parameter             `json:"parameters,omitempty"        yaml:"parameters,omitempty"`
	SecuritySchemes   map[string]SecurityScheme        `json:"securitySchemes,omitempty"   yaml:"securitySchemes,omitempty"`
	ServerVariables   map[string]ServerVar             `json:"serverVariables,omitempty"   yaml:"serverVariables,omitempty"`
	CorrelationIDs    map[string]CorrelationID         `json:"correlationIds,omitempty"    yaml:"correlationIds,omitempty"`
	Replies           map[string]OperationReply        `json:"replies,omitempty"           yaml:"replies,omitempty"`
	ReplyAddresses    map[string]OperationReplyAddress `json:"replyAddresses,omitempty"    yaml:"replyAddresses,omitempty"`
	OperationTraits   map[string]Operation             `json:"operationTraits,omitempty"   yaml:"operationTraits,omitempty"`
	MessageTraits     map[string]Message               `json:"messageTraits,omitempty"     yaml:"messageTraits,omitempty"`
	ServerBindings    map[string]map[string]any        `json:"serverBindings,omitempty"    yaml:"serverBindings,omitempty"`
	ChannelBindings   map[string]map[string]any        `json:"channelBindings,omitempty"   yaml:"channelBindings,omitempty"`
	OperationBindings map[string]map[string]any        `json:"operationBindings,omitempty" yaml:"operationBindings,omitempty"`
	MessageBindings   map[string]map[string]any        `json:"messageBindings,omitempty"   yaml:"messageBindings,omitempty"`
	Tags              map[string]Tag                   `json:"tags,omitempty"              yaml:"tags,omitempty"`
	ExternalDocs      map[string]ExternalDocs          `json:"externalDocs,omitempty"      yaml:"externalDocs,omitempty"`
}

// MultiFormatSchema represents a schema definition supporting multiple formats (JSON Schema, Avro, Protobuf, RAML, OpenAPI).
//
// # References
//   - AsyncAPI 3.1.0 §Multi Format Schema Object: https://www.asyncapi.com/docs/reference/specification/v3.1.0#multi-format-schema-object
type MultiFormatSchema struct {
	SchemaFormat string `json:"schemaFormat" yaml:"schemaFormat"`
	Schema       any    `json:"schema"       yaml:"schema"`
}

// Schema represents a JSON Schema definition for message DTO models.
//
// # References
//   - AsyncAPI 3.1.0 §Schema Object: https://www.asyncapi.com/docs/reference/specification/v3.1.0#schema-object
//   - AsyncAPI 2.6.0 §Schema Object: https://v2.asyncapi.com/docs/reference/specification/v2.6.0#schemaObject
//   - JSON Schema Draft 07: https://json-schema.org/draft-07/json-schema-release-notes.html
//   - JSON Schema Draft 2020-12: https://json-schema.org/draft/2020-12/json-schema-core.html
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
// # References
//   - AsyncAPI 3.1.0 §Reference Object: https://www.asyncapi.com/docs/reference/specification/v3.1.0#reference-object
//   - AsyncAPI 2.6.0 §Reference Object: https://v2.asyncapi.com/docs/reference/specification/v2.6.0#referenceObject
type RefObject struct {
	Ref string `json:"$ref,omitempty" yaml:"$ref,omitempty"`
}
