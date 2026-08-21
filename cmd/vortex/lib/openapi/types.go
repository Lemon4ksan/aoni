// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package openapi provides parsing, loading, 3-way specification merging,
// and declarative Go contract generation for OpenAPI 2.0, 3.0, 3.1 and HAR specifications.
//
// References:
//   - OpenAPI 3.1.0 Specification: https://spec.openapis.org/oas/v3.1.0
//   - OpenAPI 3.0.3 Specification: https://spec.openapis.org/oas/v3.0.3
//   - Swagger 2.0 Specification: https://swagger.io/specification/v2/
package openapi

import (
	"encoding/json"
	"strings"

	"gopkg.in/yaml.v3"
)

// Document represents a complete OpenAPI root document (OpenAPI 3.1/3.0 & normalized Swagger 2.0).
//
// References:
//   - OpenAPI 3.1.0 §4.8.1 OpenAPI Object
//   - OpenAPI 3.0.3 §4.8.1 OpenAPI Object
//   - Swagger 2.0 §5.1 Swagger Object
type Document struct {
	OpenAPI      string               `json:"openapi,omitempty"      yaml:"openapi,omitempty"`
	Swagger      string               `json:"swagger,omitempty"      yaml:"swagger,omitempty"`
	Info         *Info                `json:"info,omitempty"         yaml:"info,omitempty"`
	Servers      []Server             `json:"servers,omitempty"      yaml:"servers,omitempty"`
	Paths        map[string]*PathItem `json:"paths,omitempty"        yaml:"paths,omitempty"`
	Components   *Components          `json:"components,omitempty"   yaml:"components,omitempty"`
	Security     []map[string][]string `json:"security,omitempty"     yaml:"security,omitempty"`
	Tags         []Tag                `json:"tags,omitempty"         yaml:"tags,omitempty"`
	ExternalDocs *ExternalDocs        `json:"externalDocs,omitempty" yaml:"externalDocs,omitempty"`

	// Swagger 2.0 legacy fields (normalized into OpenAPI 3.x in memory)
	Host        string               `json:"host,omitempty"        yaml:"host,omitempty"`
	BasePath    string               `json:"basePath,omitempty"    yaml:"basePath,omitempty"`
	Schemes     []string             `json:"schemes,omitempty"     yaml:"schemes,omitempty"`
	Definitions map[string]*Schema   `json:"definitions,omitempty" yaml:"definitions,omitempty"`
}

// Info provides metadata about the API.
//
// Reference: OpenAPI 3.1.0 §4.8.2 Info Object
type Info struct {
	Title          string         `json:"title"                    yaml:"title"`
	Summary        string         `json:"summary,omitempty"        yaml:"summary,omitempty"`
	Description    string         `json:"description,omitempty"    yaml:"description,omitempty"`
	TermsOfService string         `json:"termsOfService,omitempty" yaml:"termsOfService,omitempty"`
	Contact        *Contact       `json:"contact,omitempty"       yaml:"contact,omitempty"`
	License        *License       `json:"license,omitempty"       yaml:"license,omitempty"`
	Version        string         `json:"version"                  yaml:"version"`
	Extensions     map[string]any `json:"-"                        yaml:"-"`
}

func (i *Info) MarshalJSON() ([]byte, error) {
	type Alias Info
	m := make(map[string]any)
	b, err := json.Marshal((*Alias)(i))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	for k, v := range i.Extensions {
		if strings.HasPrefix(k, "x-") {
			m[k] = v
		}
	}
	return json.Marshal(m)
}

func (i *Info) MarshalYAML() (any, error) {
	type Alias Info
	m := make(map[string]any)
	b, err := yaml.Marshal((*Alias)(i))
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	for k, v := range i.Extensions {
		if strings.HasPrefix(k, "x-") {
			m[k] = v
		}
	}
	return m, nil
}

// Contact contains contact information for the exposed API.
//
// Reference: OpenAPI 3.1.0 §4.8.3 Contact Object
type Contact struct {
	Name  string `json:"name,omitempty"  yaml:"name,omitempty"`
	URL   string `json:"url,omitempty"   yaml:"url,omitempty"`
	Email string `json:"email,omitempty" yaml:"email,omitempty"`
}

// License contains license information for the exposed API.
//
// Reference: OpenAPI 3.1.0 §4.8.4 License Object
type License struct {
	Name       string `json:"name"                 yaml:"name"`
	Identifier string `json:"identifier,omitempty" yaml:"identifier,omitempty"`
	URL        string `json:"url,omitempty"        yaml:"url,omitempty"`
}

// Server represents an API host and connection base path.
//
// Reference: OpenAPI 3.1.0 §4.8.5 Server Object
type Server struct {
	URL         string                    `json:"url"                   yaml:"url"`
	Description string                    `json:"description,omitempty" yaml:"description,omitempty"`
	Variables   map[string]ServerVariable `json:"variables,omitempty"   yaml:"variables,omitempty"`
}

// ServerVariable describes a variable for server URL template substitution.
//
// Reference: OpenAPI 3.1.0 §4.8.6 Server Variable Object
type ServerVariable struct {
	Enum        []string `json:"enum,omitempty"        yaml:"enum,omitempty"`
	Default     string   `json:"default"               yaml:"default"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
}

// PathItem describes the operations available on a single path route.
//
// Reference: OpenAPI 3.1.0 §4.8.9 Path Item Object
type PathItem struct {
	Ref         string       `json:"$ref,omitempty"        yaml:"$ref,omitempty"`
	Summary     string       `json:"summary,omitempty"     yaml:"summary,omitempty"`
	Description string       `json:"description,omitempty" yaml:"description,omitempty"`
	Get         *Operation   `json:"get,omitempty"         yaml:"get,omitempty"`
	Put         *Operation   `json:"put,omitempty"         yaml:"put,omitempty"`
	Post        *Operation   `json:"post,omitempty"        yaml:"post,omitempty"`
	Delete      *Operation   `json:"delete,omitempty"      yaml:"delete,omitempty"`
	Options     *Operation   `json:"options,omitempty"     yaml:"options,omitempty"`
	Head        *Operation   `json:"head,omitempty"        yaml:"head,omitempty"`
	Patch       *Operation   `json:"patch,omitempty"       yaml:"patch,omitempty"`
	Trace       *Operation   `json:"trace,omitempty"       yaml:"trace,omitempty"`
	Servers     []Server     `json:"servers,omitempty"     yaml:"servers,omitempty"`
	Parameters  []*Parameter `json:"parameters,omitempty"  yaml:"parameters,omitempty"`
}

// OperationsMap returns a map of all defined HTTP methods and their Operations.
func (p *PathItem) OperationsMap() map[string]*Operation {
	m := make(map[string]*Operation)
	if p.Get != nil {
		m["GET"] = p.Get
	}
	if p.Post != nil {
		m["POST"] = p.Post
	}
	if p.Put != nil {
		m["PUT"] = p.Put
	}
	if p.Delete != nil {
		m["DELETE"] = p.Delete
	}
	if p.Patch != nil {
		m["PATCH"] = p.Patch
	}
	if p.Head != nil {
		m["HEAD"] = p.Head
	}
	if p.Options != nil {
		m["OPTIONS"] = p.Options
	}
	if p.Trace != nil {
		m["TRACE"] = p.Trace
	}
	return m
}

// Operation describes a single API operation on a path.
//
// Reference: OpenAPI 3.1.0 §4.8.10 Operation Object
type Operation struct {
	Tags         []string              `json:"tags,omitempty"         yaml:"tags,omitempty"`
	Summary      string                `json:"summary,omitempty"      yaml:"summary,omitempty"`
	Description  string                `json:"description,omitempty"  yaml:"description,omitempty"`
	ExternalDocs *ExternalDocs         `json:"externalDocs,omitempty" yaml:"externalDocs,omitempty"`
	OperationID  string                `json:"operationId,omitempty"  yaml:"operationId,omitempty"`
	Parameters   []*Parameter          `json:"parameters,omitempty"   yaml:"parameters,omitempty"`
	RequestBody  *RequestBody          `json:"requestBody,omitempty"  yaml:"requestBody,omitempty"`
	Responses    map[string]*Response  `json:"responses,omitempty"    yaml:"responses,omitempty"`
	Deprecated   bool                  `json:"deprecated,omitempty"   yaml:"deprecated,omitempty"`
	Security     []map[string][]string `json:"security,omitempty"     yaml:"security,omitempty"`
	Servers      []Server              `json:"servers,omitempty"      yaml:"servers,omitempty"`
	Extensions   map[string]any        `json:"-"                      yaml:"-"`
}

func (op *Operation) MarshalJSON() ([]byte, error) {
	type Alias Operation
	m := make(map[string]any)
	b, err := json.Marshal((*Alias)(op))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	for k, v := range op.Extensions {
		if strings.HasPrefix(k, "x-") {
			m[k] = v
		}
	}
	return json.Marshal(m)
}

func (op *Operation) MarshalYAML() (any, error) {
	type Alias Operation
	m := make(map[string]any)
	b, err := yaml.Marshal((*Alias)(op))
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	for k, v := range op.Extensions {
		if strings.HasPrefix(k, "x-") {
			m[k] = v
		}
	}
	return m, nil
}

// Parameter describes a single operation parameter (path, query, header, cookie).
//
// Reference: OpenAPI 3.1.0 §4.8.12 Parameter Object
type Parameter struct {
	Name            string         `json:"name"                      yaml:"name"`
	In              string         `json:"in"                        yaml:"in"` // "path", "query", "header", "cookie"
	Description     string         `json:"description,omitempty"     yaml:"description,omitempty"`
	Required        bool           `json:"required,omitempty"        yaml:"required,omitempty"`
	Deprecated      bool           `json:"deprecated,omitempty"      yaml:"deprecated,omitempty"`
	AllowEmptyValue bool           `json:"allowEmptyValue,omitempty" yaml:"allowEmptyValue,omitempty"`
	Style           string         `json:"style,omitempty"           yaml:"style,omitempty"`
	Explode         *bool          `json:"explode,omitempty"         yaml:"explode,omitempty"`
	Schema          *Schema        `json:"schema,omitempty"          yaml:"schema,omitempty"`
	Example         any            `json:"example,omitempty"         yaml:"example,omitempty"`
	Extensions      map[string]any `json:"-"                         yaml:"-"`
}

// RequestBody describes a single request body.
//
// Reference: OpenAPI 3.1.0 §4.8.13 Request Body Object
type RequestBody struct {
	Description string                `json:"description,omitempty" yaml:"description,omitempty"`
	Content     map[string]*MediaType `json:"content"                yaml:"content"`
	Required    bool                  `json:"required,omitempty"    yaml:"required,omitempty"`
	Ref         string                `json:"$ref,omitempty"        yaml:"$ref,omitempty"`
}

// MediaType provides schema and examples for a specific media type (e.g. application/json).
//
// Reference: OpenAPI 3.1.0 §4.8.14 Media Type Object
type MediaType struct {
	Schema   *Schema `json:"schema,omitempty"   yaml:"schema,omitempty"`
	Example  any     `json:"example,omitempty"  yaml:"example,omitempty"`
	Examples map[string]any `json:"examples,omitempty" yaml:"examples,omitempty"`
}

// Response describes a single response from an API Operation.
//
// Reference: OpenAPI 3.1.0 §4.8.17 Response Object
type Response struct {
	Description string                `json:"description"           yaml:"description"`
	Headers     map[string]*Header    `json:"headers,omitempty"     yaml:"headers,omitempty"`
	Content     map[string]*MediaType `json:"content,omitempty"     yaml:"content,omitempty"`
	Ref         string                `json:"$ref,omitempty"        yaml:"$ref,omitempty"`
}

// Header describes a response or parameter header.
//
// Reference: OpenAPI 3.1.0 §4.8.18 Header Object
type Header struct {
	Description string  `json:"description,omitempty" yaml:"description,omitempty"`
	Required    bool    `json:"required,omitempty"    yaml:"required,omitempty"`
	Deprecated  bool    `json:"deprecated,omitempty"  yaml:"deprecated,omitempty"`
	Schema      *Schema `json:"schema,omitempty"      yaml:"schema,omitempty"`
}

// Components holds a set of reusable objects for different aspects of the specification.
//
// Reference: OpenAPI 3.1.0 §4.8.7 Components Object
type Components struct {
	Schemas         map[string]*Schema         `json:"schemas,omitempty"         yaml:"schemas,omitempty"`
	Responses       map[string]*Response       `json:"responses,omitempty"       yaml:"responses,omitempty"`
	Parameters      map[string]*Parameter      `json:"parameters,omitempty"      yaml:"parameters,omitempty"`
	RequestBodies   map[string]*RequestBody    `json:"requestBodies,omitempty"   yaml:"requestBodies,omitempty"`
	Headers         map[string]*Header         `json:"headers,omitempty"         yaml:"headers,omitempty"`
	SecuritySchemes map[string]*SecurityScheme `json:"securitySchemes,omitempty" yaml:"securitySchemes,omitempty"`
}

// Schema represents a JSON Schema definition (supporting JSON Schema Draft 2020-12 / Draft 04/07).
//
// Reference: OpenAPI 3.1.0 §4.8.24 Schema Object
type Schema struct {
	Type                 TypeArray          `json:"type,omitempty"                 yaml:"type,omitempty"`
	Format               string             `json:"format,omitempty"               yaml:"format,omitempty"`
	Title                string             `json:"title,omitempty"                yaml:"title,omitempty"`
	Description          string             `json:"description,omitempty"          yaml:"description,omitempty"`
	Default              any                `json:"default,omitempty"              yaml:"default,omitempty"`
	Enum                 []any              `json:"enum,omitempty"                 yaml:"enum,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"           yaml:"properties,omitempty"`
	Required             []string           `json:"required,omitempty"             yaml:"required,omitempty"`
	Items                *Schema            `json:"items,omitempty"                yaml:"items,omitempty"`
	AllOf                []*Schema          `json:"allOf,omitempty"                yaml:"allOf,omitempty"`
	OneOf                []*Schema          `json:"oneOf,omitempty"                yaml:"oneOf,omitempty"`
	AnyOf                []*Schema          `json:"anyOf,omitempty"                yaml:"anyOf,omitempty"`
	Not                  *Schema            `json:"not,omitempty"                  yaml:"not,omitempty"`
	AdditionalProperties any                `json:"additionalProperties,omitempty" yaml:"additionalProperties,omitempty"`
	Nullable             bool               `json:"nullable,omitempty"             yaml:"nullable,omitempty"`
	ReadOnly             bool               `json:"readOnly,omitempty"             yaml:"readOnly,omitempty"`
	WriteOnly            bool               `json:"writeOnly,omitempty"            yaml:"writeOnly,omitempty"`
	Deprecated           bool               `json:"deprecated,omitempty"           yaml:"deprecated,omitempty"`
	Ref                  string             `json:"$ref,omitempty"                 yaml:"$ref,omitempty"`
}

// IsType reports whether the schema matches the given type name (e.g. "object", "string", "array", "integer").
func (s *Schema) IsType(typeName string) bool {
	if s == nil {
		return false
	}
	return s.Type.Contains(typeName)
}

// TypeArray represents a schema type that can be parsed from either a single string ("string")
// or an array of strings (["string", "null"] in OpenAPI 3.1.0 / JSON Schema Draft 2020-12).
type TypeArray []string

// Contains reports whether the given type name is present in the TypeArray.
func (ta TypeArray) Contains(name string) bool {
	for _, t := range ta {
		if strings.EqualFold(t, name) {
			return true
		}
	}
	return false
}

// Primary returns the primary non-null type name, or empty string.
func (ta TypeArray) Primary() string {
	for _, t := range ta {
		if !strings.EqualFold(t, "null") {
			return t
		}
	}
	if len(ta) > 0 {
		return ta[0]
	}
	return ""
}

// UnmarshalYAML implements custom unmarshaling for TypeArray from scalar or sequence.
func (ta *TypeArray) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		*ta = TypeArray{value.Value}
		return nil
	}
	if value.Kind == yaml.SequenceNode {
		var list []string
		if err := value.Decode(&list); err != nil {
			return err
		}
		*ta = list
		return nil
	}
	return nil
}

// UnmarshalJSON implements custom unmarshaling for TypeArray from string or array.
func (ta *TypeArray) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*ta = TypeArray{single}
		return nil
	}
	var list []string
	if err := json.Unmarshal(data, &list); err == nil {
		*ta = list
		return nil
	}
	return nil
}

// SecurityScheme defines a security scheme that can be used by operations.
//
// Reference: OpenAPI 3.1.0 §4.8.27 Security Scheme Object
type SecurityScheme struct {
	Type             string      `json:"type"                       yaml:"type"` // "apiKey", "http", "oauth2", "openIdConnect"
	Description      string      `json:"description,omitempty"      yaml:"description,omitempty"`
	Name             string      `json:"name,omitempty"             yaml:"name,omitempty"`
	In               string      `json:"in,omitempty"               yaml:"in,omitempty"` // "query", "header", "cookie"
	Scheme           string      `json:"scheme,omitempty"           yaml:"scheme,omitempty"`
	BearerFormat     string      `json:"bearerFormat,omitempty"     yaml:"bearerFormat,omitempty"`
	Flows            *OAuthFlows `json:"flows,omitempty"            yaml:"flows,omitempty"`
	OpenIDConnectURL string      `json:"openIdConnectUrl,omitempty" yaml:"openIdConnectUrl,omitempty"`
}

// OAuthFlows allows configuration of supported OAuth Flows.
//
// Reference: OpenAPI 3.1.0 §4.8.28 OAuth Flows Object
type OAuthFlows struct {
	Implicit          *OAuthFlow `json:"implicit,omitempty"          yaml:"implicit,omitempty"`
	Password          *OAuthFlow `json:"password,omitempty"          yaml:"password,omitempty"`
	ClientCredentials *OAuthFlow `json:"clientCredentials,omitempty" yaml:"clientCredentials,omitempty"`
	AuthorizationCode *OAuthFlow `json:"authorizationCode,omitempty" yaml:"authorizationCode,omitempty"`
}

// OAuthFlow configuration details for a supported OAuth Flow.
//
// Reference: OpenAPI 3.1.0 §4.8.29 OAuth Flow Object
type OAuthFlow struct {
	AuthorizationURL string            `json:"authorizationUrl,omitempty" yaml:"authorizationUrl,omitempty"`
	TokenURL         string            `json:"tokenUrl,omitempty"         yaml:"tokenUrl,omitempty"`
	RefreshURL       string            `json:"refreshUrl,omitempty"       yaml:"refreshUrl,omitempty"`
	Scopes           map[string]string `json:"scopes,omitempty"           yaml:"scopes,omitempty"`
}

// Tag represents a categorization label for API operations.
//
// Reference: OpenAPI 3.1.0 §4.8.31 Tag Object
type Tag struct {
	Name         string        `json:"name"                   yaml:"name"`
	Description  string        `json:"description,omitempty" yaml:"description,omitempty"`
	ExternalDocs *ExternalDocs `json:"externalDocs,omitempty" yaml:"externalDocs,omitempty"`
}

// ExternalDocs provides a reference to external documentation.
//
// Reference: OpenAPI 3.1.0 §4.8.32 External Documentation Object
type ExternalDocs struct {
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	URL         string `json:"url"                   yaml:"url"`
}
