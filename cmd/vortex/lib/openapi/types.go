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
	OpenAPI           string                `json:"openapi,omitempty"           yaml:"openapi,omitempty"`
	Swagger           string                `json:"swagger,omitempty"           yaml:"swagger,omitempty"`
	Info              *Info                 `json:"info,omitempty"              yaml:"info,omitempty"`
	JSONSchemaDialect string                `json:"jsonSchemaDialect,omitempty"  yaml:"jsonSchemaDialect,omitempty"`
	Servers           []Server              `json:"servers,omitempty"           yaml:"servers,omitempty"`
	Paths             map[string]*PathItem  `json:"paths,omitempty"             yaml:"paths,omitempty"`
	Webhooks          map[string]*PathItem  `json:"webhooks,omitempty"          yaml:"webhooks,omitempty"`
	Components        *Components           `json:"components,omitempty"        yaml:"components,omitempty"`
	Security          []map[string][]string `json:"security,omitempty"          yaml:"security,omitempty"`
	Tags              []Tag                 `json:"tags,omitempty"              yaml:"tags,omitempty"`
	ExternalDocs      *ExternalDocs         `json:"externalDocs,omitempty"      yaml:"externalDocs,omitempty"`

	// Swagger 2.0 legacy fields (normalized into OpenAPI 3.x in memory)
	Host                string                     `json:"host,omitempty"                yaml:"host,omitempty"`
	BasePath            string                     `json:"basePath,omitempty"            yaml:"basePath,omitempty"`
	Schemes             []string                   `json:"schemes,omitempty"             yaml:"schemes,omitempty"`
	Consumes            []string                   `json:"consumes,omitempty"            yaml:"consumes,omitempty"`
	Produces            []string                   `json:"produces,omitempty"            yaml:"produces,omitempty"`
	Definitions         map[string]*Schema         `json:"definitions,omitempty"         yaml:"definitions,omitempty"`
	Parameters          map[string]*Parameter      `json:"parameters,omitempty"          yaml:"parameters,omitempty"`
	Responses           map[string]*Response       `json:"responses,omitempty"           yaml:"responses,omitempty"`
	SecurityDefinitions map[string]*SecurityScheme `json:"securityDefinitions,omitempty" yaml:"securityDefinitions,omitempty"`
}

// Info provides metadata about the API.
//
// References:
//   - OpenAPI 3.1.0 §4.8.2 Info Object
//   - Swagger 2.0 §5.2 Info Object
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
// References:
//   - OpenAPI 3.1.0 §4.8.3 Contact Object
//   - Swagger 2.0 §5.3 Contact Object
type Contact struct {
	Name  string `json:"name,omitempty"  yaml:"name,omitempty"`
	URL   string `json:"url,omitempty"   yaml:"url,omitempty"`
	Email string `json:"email,omitempty" yaml:"email,omitempty"`
}

// License contains license information for the exposed API.
//
// References:
//   - OpenAPI 3.1.0 §4.8.4 License Object
//   - Swagger 2.0 §5.4 License Object
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
// References:
//   - OpenAPI 3.1.0 §4.8.9 Path Item Object
//   - Swagger 2.0 §5.6 Path Item Object
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
// References:
//   - OpenAPI 3.1.0 §4.8.10 Operation Object
//   - Swagger 2.0 §5.7 Operation Object
type Operation struct {
	Tags         []string              `json:"tags,omitempty"         yaml:"tags,omitempty"`
	Summary      string                `json:"summary,omitempty"      yaml:"summary,omitempty"`
	Description  string                `json:"description,omitempty"  yaml:"description,omitempty"`
	ExternalDocs *ExternalDocs         `json:"externalDocs,omitempty" yaml:"externalDocs,omitempty"`
	OperationID  string                `json:"operationId,omitempty"  yaml:"operationId,omitempty"`
	Consumes     []string              `json:"consumes,omitempty"     yaml:"consumes,omitempty"`
	Produces     []string              `json:"produces,omitempty"     yaml:"produces,omitempty"`
	Parameters   []*Parameter          `json:"parameters,omitempty"   yaml:"parameters,omitempty"`
	RequestBody  *RequestBody          `json:"requestBody,omitempty"  yaml:"requestBody,omitempty"`
	Responses    map[string]*Response  `json:"responses,omitempty"    yaml:"responses,omitempty"`
	Callbacks    map[string]*PathItem  `json:"callbacks,omitempty"    yaml:"callbacks,omitempty"`
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

// Parameter describes a single operation parameter (path, query, header, cookie, or formData/body in Swagger 2.0).
//
// References:
//   - OpenAPI 3.1.0 §4.8.12 Parameter Object
//   - Swagger 2.0 §5.9 Parameter Object
type Parameter struct {
	Name            string         `json:"name"                      yaml:"name"`
	In              string         `json:"in"                        yaml:"in"` // "path", "query", "header", "cookie", "formData", "body"
	Description     string         `json:"description,omitempty"     yaml:"description,omitempty"`
	Required        bool           `json:"required,omitempty"        yaml:"required,omitempty"`
	Deprecated      bool           `json:"deprecated,omitempty"      yaml:"deprecated,omitempty"`
	AllowEmptyValue bool           `json:"allowEmptyValue,omitempty" yaml:"allowEmptyValue,omitempty"`
	Style           string         `json:"style,omitempty"           yaml:"style,omitempty"`
	Explode         *bool          `json:"explode,omitempty"         yaml:"explode,omitempty"`
	AllowReserved   bool           `json:"allowReserved,omitempty"   yaml:"allowReserved,omitempty"`
	Schema          *Schema        `json:"schema,omitempty"          yaml:"schema,omitempty"`
	Example         any            `json:"example,omitempty"         yaml:"example,omitempty"`
	Examples        map[string]any `json:"examples,omitempty"        yaml:"examples,omitempty"`

	// Swagger 2.0 legacy fields
	Type             string  `json:"type,omitempty"             yaml:"type,omitempty"`
	Format           string  `json:"format,omitempty"           yaml:"format,omitempty"`
	Items            *Schema `json:"items,omitempty"            yaml:"items,omitempty"`
	CollectionFormat string  `json:"collectionFormat,omitempty" yaml:"collectionFormat,omitempty"`

	Extensions map[string]any `json:"-" yaml:"-"`
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
	Schema   *Schema        `json:"schema,omitempty"   yaml:"schema,omitempty"`
	Example  any            `json:"example,omitempty"  yaml:"example,omitempty"`
	Examples map[string]any `json:"examples,omitempty" yaml:"examples,omitempty"`
	Encoding map[string]any `json:"encoding,omitempty" yaml:"encoding,omitempty"`
}

// Response describes a single response from an API Operation.
//
// References:
//   - OpenAPI 3.1.0 §4.8.17 Response Object
//   - Swagger 2.0 §5.12 Response Object
type Response struct {
	Description string                `json:"description"           yaml:"description"`
	Headers     map[string]*Header    `json:"headers,omitempty"     yaml:"headers,omitempty"`
	Content     map[string]*MediaType `json:"content,omitempty"     yaml:"content,omitempty"`
	Links       map[string]*Link      `json:"links,omitempty"       yaml:"links,omitempty"`
	Ref         string                `json:"$ref,omitempty"        yaml:"$ref,omitempty"`

	// Swagger 2.0 legacy schema field
	Schema   *Schema        `json:"schema,omitempty"   yaml:"schema,omitempty"`
	Examples map[string]any `json:"examples,omitempty" yaml:"examples,omitempty"`
}

// Header describes a response or parameter header.
//
// References:
//   - OpenAPI 3.1.0 §4.8.18 Header Object
//   - Swagger 2.0 §5.14 Header Object
type Header struct {
	Description string  `json:"description,omitempty" yaml:"description,omitempty"`
	Required    bool    `json:"required,omitempty"    yaml:"required,omitempty"`
	Deprecated  bool    `json:"deprecated,omitempty"  yaml:"deprecated,omitempty"`
	Style       string  `json:"style,omitempty"       yaml:"style,omitempty"`
	Explode     *bool   `json:"explode,omitempty"     yaml:"explode,omitempty"`
	Schema      *Schema `json:"schema,omitempty"      yaml:"schema,omitempty"`

	// Swagger 2.0 legacy fields
	Type             string  `json:"type,omitempty"             yaml:"type,omitempty"`
	Format           string  `json:"format,omitempty"           yaml:"format,omitempty"`
	Items            *Schema `json:"items,omitempty"            yaml:"items,omitempty"`
	CollectionFormat string  `json:"collectionFormat,omitempty" yaml:"collectionFormat,omitempty"`
}

// Link represents a design-time link between operations.
//
// Reference: OpenAPI 3.1.0 §4.8.21 Link Object
type Link struct {
	OperationRef string         `json:"operationRef,omitempty" yaml:"operationRef,omitempty"`
	OperationID  string         `json:"operationId,omitempty"  yaml:"operationId,omitempty"`
	Parameters   map[string]any `json:"parameters,omitempty"   yaml:"parameters,omitempty"`
	RequestBody  any            `json:"requestBody,omitempty"  yaml:"requestBody,omitempty"`
	Description  string         `json:"description,omitempty"  yaml:"description,omitempty"`
	Server       *Server        `json:"server,omitempty"       yaml:"server,omitempty"`
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
	Links           map[string]*Link           `json:"links,omitempty"           yaml:"links,omitempty"`
	Callbacks       map[string]*PathItem       `json:"callbacks,omitempty"       yaml:"callbacks,omitempty"`
	PathItems       map[string]*PathItem       `json:"pathItems,omitempty"       yaml:"pathItems,omitempty"`
}

// Schema represents a JSON Schema definition (supporting JSON Schema Draft 2020-12 / Draft 04/07).
//
// References:
//   - OpenAPI 3.1.0 §4.8.24 Schema Object
//   - Swagger 2.0 §5.17 Schema Object
type Schema struct {
	Type                 TypeArray          `json:"type,omitempty"                 yaml:"type,omitempty"`
	Format               string             `json:"format,omitempty"               yaml:"format,omitempty"`
	Title                string             `json:"title,omitempty"                yaml:"title,omitempty"`
	Description          string             `json:"description,omitempty"          yaml:"description,omitempty"`
	Default              any                `json:"default,omitempty"              yaml:"default,omitempty"`
	MultipleOf           *float64           `json:"multipleOf,omitempty"           yaml:"multipleOf,omitempty"`
	Maximum              *float64           `json:"maximum,omitempty"              yaml:"maximum,omitempty"`
	ExclusiveMaximum     any                `json:"exclusiveMaximum,omitempty"     yaml:"exclusiveMaximum,omitempty"`
	Minimum              *float64           `json:"minimum,omitempty"              yaml:"minimum,omitempty"`
	ExclusiveMinimum     any                `json:"exclusiveMinimum,omitempty"     yaml:"exclusiveMinimum,omitempty"`
	MaxLength            *int               `json:"maxLength,omitempty"            yaml:"maxLength,omitempty"`
	MinLength            *int               `json:"minLength,omitempty"            yaml:"minLength,omitempty"`
	Pattern              string             `json:"pattern,omitempty"              yaml:"pattern,omitempty"`
	MaxItems             *int               `json:"maxItems,omitempty"             yaml:"maxItems,omitempty"`
	MinItems             *int               `json:"minItems,omitempty"             yaml:"minItems,omitempty"`
	UniqueItems          bool               `json:"uniqueItems,omitempty"          yaml:"uniqueItems,omitempty"`
	MaxProperties        *int               `json:"maxProperties,omitempty"        yaml:"maxProperties,omitempty"`
	MinProperties        *int               `json:"minProperties,omitempty"        yaml:"minProperties,omitempty"`
	Required             []string           `json:"required,omitempty"             yaml:"required,omitempty"`
	Enum                 []any              `json:"enum,omitempty"                 yaml:"enum,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"           yaml:"properties,omitempty"`
	Items                *Schema            `json:"items,omitempty"                yaml:"items,omitempty"`
	AllOf                []*Schema          `json:"allOf,omitempty"                yaml:"allOf,omitempty"`
	OneOf                []*Schema          `json:"oneOf,omitempty"                yaml:"oneOf,omitempty"`
	AnyOf                []*Schema          `json:"anyOf,omitempty"                yaml:"anyOf,omitempty"`
	Not                  *Schema            `json:"not,omitempty"                  yaml:"not,omitempty"`
	AdditionalProperties any                `json:"additionalProperties,omitempty" yaml:"additionalProperties,omitempty"`
	Discriminator        *Discriminator     `json:"discriminator,omitempty"        yaml:"discriminator,omitempty"`
	ReadOnly             bool               `json:"readOnly,omitempty"             yaml:"readOnly,omitempty"`
	WriteOnly            bool               `json:"writeOnly,omitempty"            yaml:"writeOnly,omitempty"`
	XML                  *XML               `json:"xml,omitempty"                  yaml:"xml,omitempty"`
	ExternalDocs         *ExternalDocs      `json:"externalDocs,omitempty"         yaml:"externalDocs,omitempty"`
	Example              any                `json:"example,omitempty"              yaml:"example,omitempty"`
	Examples             []any              `json:"examples,omitempty"             yaml:"examples,omitempty"`
	Nullable             bool               `json:"nullable,omitempty"             yaml:"nullable,omitempty"`
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

// Discriminator adds support for polymorphism.
//
// References:
//   - OpenAPI 3.1.0 §4.8.25 Discriminator Object
//   - Swagger 2.0 §5.18 Schema Object (string discriminator)
type Discriminator struct {
	PropertyName string            `json:"propertyName"      yaml:"propertyName"`
	Mapping      map[string]string `json:"mapping,omitempty" yaml:"mapping,omitempty"`
}

// UnmarshalJSON supports parsing Discriminator from either a string (Swagger 2.0) or an object (OpenAPI 3.x).
func (d *Discriminator) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		d.PropertyName = str
		return nil
	}

	type Alias Discriminator
	var a Alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*d = Discriminator(a)
	return nil
}

// UnmarshalYAML supports parsing Discriminator from either a string or an object node in YAML.
func (d *Discriminator) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		d.PropertyName = value.Value
		return nil
	}

	type Alias Discriminator
	var a Alias
	if err := value.Decode(&a); err != nil {
		return err
	}
	*d = Discriminator(a)
	return nil
}

// XML provides metadata for XML representation format.
//
// References:
//   - OpenAPI 3.1.0 §4.8.26 XML Object
//   - Swagger 2.0 §5.19 XML Object
type XML struct {
	Name      string `json:"name,omitempty"      yaml:"name,omitempty"`
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Prefix    string `json:"prefix,omitempty"    yaml:"prefix,omitempty"`
	Attribute bool   `json:"attribute,omitempty" yaml:"attribute,omitempty"`
	Wrapped   bool   `json:"wrapped,omitempty"   yaml:"wrapped,omitempty"`
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
// References:
//   - OpenAPI 3.1.0 §4.8.27 Security Scheme Object
//   - Swagger 2.0 §5.23 Security Scheme Object
type SecurityScheme struct {
	Type             string      `json:"type"                       yaml:"type"` // "apiKey", "http", "oauth2", "openIdConnect", "mutualTLS"
	Description      string      `json:"description,omitempty"      yaml:"description,omitempty"`
	Name             string      `json:"name,omitempty"             yaml:"name,omitempty"`
	In               string      `json:"in,omitempty"               yaml:"in,omitempty"` // "query", "header", "cookie"
	Scheme           string      `json:"scheme,omitempty"           yaml:"scheme,omitempty"`
	BearerFormat     string      `json:"bearerFormat,omitempty"     yaml:"bearerFormat,omitempty"`
	Flows            *OAuthFlows `json:"flows,omitempty"            yaml:"flows,omitempty"`
	OpenIDConnectURL string      `json:"openIdConnectUrl,omitempty" yaml:"openIdConnectUrl,omitempty"`

	// Swagger 2.0 legacy fields
	Flow             string            `json:"flow,omitempty"             yaml:"flow,omitempty"`
	AuthorizationURL string            `json:"authorizationUrl,omitempty" yaml:"authorizationUrl,omitempty"`
	TokenURL         string            `json:"tokenUrl,omitempty"         yaml:"tokenUrl,omitempty"`
	Scopes           map[string]string `json:"scopes,omitempty"           yaml:"scopes,omitempty"`
}

// OAuthFlows allows configuration of supported OAuth Flows.
//
// References:
//   - OpenAPI 3.1.0 §4.8.28 OAuth Flows Object
//   - Swagger 2.0 §5.23 Security Scheme Object
type OAuthFlows struct {
	Implicit          *OAuthFlow `json:"implicit,omitempty"          yaml:"implicit,omitempty"`
	Password          *OAuthFlow `json:"password,omitempty"          yaml:"password,omitempty"`
	ClientCredentials *OAuthFlow `json:"clientCredentials,omitempty" yaml:"clientCredentials,omitempty"`
	AuthorizationCode *OAuthFlow `json:"authorizationCode,omitempty" yaml:"authorizationCode,omitempty"`
}

// OAuthFlow configuration details for a supported OAuth Flow.
//
// References:
//   - OpenAPI 3.1.0 §4.8.29 OAuth Flow Object
//   - Swagger 2.0 §5.23 Security Scheme Object
type OAuthFlow struct {
	AuthorizationURL string            `json:"authorizationUrl,omitempty" yaml:"authorizationUrl,omitempty"`
	TokenURL         string            `json:"tokenUrl,omitempty"         yaml:"tokenUrl,omitempty"`
	RefreshURL       string            `json:"refreshUrl,omitempty"       yaml:"refreshUrl,omitempty"`
	Scopes           map[string]string `json:"scopes,omitempty"           yaml:"scopes,omitempty"`
}

// Tag represents a categorization label for API operations.
//
// References:
//   - OpenAPI 3.1.0 §4.8.31 Tag Object
//   - Swagger 2.0 §5.15 Tag Object
type Tag struct {
	Name         string        `json:"name"                   yaml:"name"`
	Description  string        `json:"description,omitempty" yaml:"description,omitempty"`
	ExternalDocs *ExternalDocs `json:"externalDocs,omitempty" yaml:"externalDocs,omitempty"`
}

// ExternalDocs provides a reference to external documentation.
//
// References:
//   - OpenAPI 3.1.0 §4.8.32 External Documentation Object
//   - Swagger 2.0 §5.8 External Documentation Object
type ExternalDocs struct {
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	URL         string `json:"url"                   yaml:"url"`
}
