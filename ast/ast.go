// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ast

import "github.com/lemon4ksan/foundation/generic"

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
	QueryName  string
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

// FindService searches for a service node by name.
func (f *File) FindService(name string) (*Service, bool) {
	if f == nil {
		return nil, false
	}

	return generic.Find(f.Services, func(s *Service) bool {
		return s != nil && s.Name == name
	})
}

// FindServiceOptional returns FindService wrapped in a [generic.Optional].
func (f *File) FindServiceOptional(name string) generic.Optional[*Service] {
	return generic.From(f.FindService(name))
}

// FindStruct searches for a struct DTO node by name.
func (f *File) FindStruct(name string) (*Struct, bool) {
	if f == nil {
		return nil, false
	}

	return generic.Find(f.Structs, func(s *Struct) bool {
		return s != nil && s.Name == name
	})
}

// FindStructOptional returns FindStruct wrapped in a [generic.Optional].
func (f *File) FindStructOptional(name string) generic.Optional[*Struct] {
	return generic.From(f.FindStruct(name))
}

// FindTuple searches for a tuple node by name.
func (f *File) FindTuple(name string) (*Tuple, bool) {
	if f == nil {
		return nil, false
	}

	return generic.Find(f.Tuples, func(t *Tuple) bool {
		return t != nil && t.Name == name
	})
}

// FindTupleOptional returns FindTuple wrapped in a [generic.Optional].
func (f *File) FindTupleOptional(name string) generic.Optional[*Tuple] {
	return generic.From(f.FindTuple(name))
}

// FindUnion searches for a union node by name.
func (f *File) FindUnion(name string) (*Union, bool) {
	if f == nil {
		return nil, false
	}

	return generic.Find(f.Unions, func(u *Union) bool {
		return u != nil && u.Name == name
	})
}

// FindUnionOptional returns FindUnion wrapped in a [generic.Optional].
func (f *File) FindUnionOptional(name string) generic.Optional[*Union] {
	return generic.From(f.FindUnion(name))
}

// FindBitpack searches for a bitpack node by name.
func (f *File) FindBitpack(name string) (*Bitpack, bool) {
	if f == nil {
		return nil, false
	}

	return generic.Find(f.Bitpacks, func(b *Bitpack) bool {
		return b != nil && b.Name == name
	})
}

// FindBitpackOptional returns FindBitpack wrapped in a [generic.Optional].
func (f *File) FindBitpackOptional(name string) generic.Optional[*Bitpack] {
	return generic.From(f.FindBitpack(name))
}

// FindMethod searches for a method by name on the service interface.
func (s *Service) FindMethod(name string) (*Method, bool) {
	if s == nil {
		return nil, false
	}

	return generic.Find(s.Methods, func(m *Method) bool {
		return m != nil && m.Name == name
	})
}

// FindMethodOptional returns FindMethod wrapped in a [generic.Optional].
func (s *Service) FindMethodOptional(name string) generic.Optional[*Method] {
	return generic.From(s.FindMethod(name))
}

// HasMethods reports whether the service defines at least one endpoint method.
func (s *Service) HasMethods() bool {
	return s != nil && len(s.Methods) > 0
}

// FindField searches for a struct field by name.
func (s *Struct) FindField(name string) (*Field, bool) {
	if s == nil {
		return nil, false
	}

	return generic.Find(s.Fields, func(f *Field) bool {
		return f != nil && f.Name == name
	})
}

// FindFieldOptional returns FindField wrapped in a [generic.Optional].
func (s *Struct) FindFieldOptional(name string) generic.Optional[*Field] {
	return generic.From(s.FindField(name))
}

// HasFields reports whether the struct defines at least one field.
func (s *Struct) HasFields() bool {
	return s != nil && len(s.Fields) > 0
}
