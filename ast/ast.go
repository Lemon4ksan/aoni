// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package ast provides a public, programmatic Abstract Syntax Tree (AST) for constructing,
// inspecting, transforming, and rendering Aoni declarative API contracts.
package ast

// ProtocolKind identifies the underlying transport protocol.
type ProtocolKind string

const (
	ProtocolHTTP   ProtocolKind = "http"
	ProtocolRPC    ProtocolKind = "rpc"
	ProtocolSocket ProtocolKind = "socket"
	ProtocolWS     ProtocolKind = "ws"
	ProtocolGRPC   ProtocolKind = "grpc"
	ProtocolSSH    ProtocolKind = "ssh"
)

// EngineKind identifies the execution client engine to use.
type EngineKind string

const (
	EngineFast     EngineKind = "fast"
	EngineNetHTTP  EngineKind = "net/http"
	EngineCustom   EngineKind = "custom"
	EngineRequired EngineKind = "required"
)

// CasingStrategy defines the casing convention for serialization keys.
type CasingStrategy string

const (
	CasingSnake  CasingStrategy = "snake_case"
	CasingCamel  CasingStrategy = "camelCase"
	CasingKebab  CasingStrategy = "kebab-case"
	CasingPascal CasingStrategy = "PascalCase"
)

// File represents a complete Aoni Go contract file AST.
type File struct {
	PackageName string
	Doc         []string
	Imports     []Import
	Services    []*Service
	Structs     []*Struct
	Tuples      []*Tuple
	Unions      []*Union
	Bitpacks    []*Bitpack
	MaxLen      int // Max line length threshold (default 120)
}

// Import represents a Go import path with optional alias.
type Import struct {
	Alias string
	Path  string
}

// Service represents an API service interface definition and its client settings.
type Service struct {
	Name        string
	Doc         []string
	Protocol    ProtocolKind
	Engine      EngineKind
	BaseURL     string
	Casing      CasingStrategy
	Timeout     string
	Headers     []Header
	Methods     []*Method
	Tags        []string
	Version     string
	Description string
	UnwrapField string // Service-wide unwrap directive (@unwrap "result")
}

// Header represents a default static or dynamic HTTP header on a service.
type Header struct {
	Name  string
	Value string
}

// Method represents an RPC or HTTP endpoint method signature on a service interface.
type Method struct {
	Name          string
	Doc           []string
	HTTPMethod    string // GET, POST, PUT, DELETE, PATCH, etc.
	Path          string // e.g. "sendMessage", "v1/users/{id}"
	Form          bool   // @form tag (application/x-www-form-urlencoded or multipart)
	RequestModel  string // Go type name of request payload struct (e.g. "*SendMessageRequest")
	ResponseModel string // Go type name of return payload (e.g. "*Message", "bool", "[]*Update")
	StreamReturn  bool   // @stream tag
	Headers       []Header
	Parameters    []Parameter // Positional parameters when not using RequestModel
	UnwrapField   string      // @unwrap "result"
	Summary       string
}

// Parameter represents a positional method parameter.
type Parameter struct {
	Name string
	Type string
	Doc  string
}

// Struct represents a Data Transfer Object (DTO) struct definition.
type Struct struct {
	Name      string
	Doc       []string
	Casing    CasingStrategy
	Omitempty bool
	Fields    []*Field
}

// Field represents a struct field in a DTO.
type Field struct {
	Name       string
	Type       string // Go type name (e.g. "int64", "string", "*Chat", "[]*MessageEntity")
	JSONName   string
	URLName    string
	FormName   string
	AoniTag    string // e.g. "0", "3.1" for positional tuples
	IsPointer  bool
	IsSlice    bool
	Required   bool
	Doc        []string
	CustomTags string // custom struct tag string if any
}

// Tuple represents a positional Protobuf / JSPB sparse tuple definition.
type Tuple struct {
	Name     string
	Doc      []string
	Indices  map[string]string // fieldName -> index tag (e.g. "0", "3.1")
	Fields   []*Field
	Sentinel bool
}

// Union represents a polymorphic union type.
type Union struct {
	Name          string
	Doc           []string
	Discriminator string
	Cases         []string
}

// Bitpack represents a compact bit-packed struct definition.
type Bitpack struct {
	Name        string
	Doc         []string
	StorageType string // uint8, uint16, uint32, uint64
	Fields      []*BitpackField
}

// BitpackField represents a bitfield range within a bitpack.
type BitpackField struct {
	Name     string
	BitWidth int
	Doc      []string
}
