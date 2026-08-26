// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ast

import "github.com/lemon4ksan/foundation/generic"

// NewFile initializes a new File AST with default imports and default 120 max line length.
func NewFile(pkgName string) *File {
	return &File{
		PackageName: pkgName,
		MaxLen:      120,
		Imports: []Import{
			{Path: "context"},
			{Path: "github.com/lemon4ksan/aoni"},
		},
	}
}

// WithMaxLen sets the maximum line length threshold for multiline wrapping.
func (f *File) WithMaxLen(n int) *File {
	f.MaxLen = n
	return f
}

// AddImport appends an import dependency.
func (f *File) AddImport(path, alias string) *File {
	if generic.Any(f.Imports, func(imp Import) bool { return imp.Path == path }) {
		return f
	}

	f.Imports = append(f.Imports, Import{Path: path, Alias: alias})

	return f
}

// NewService adds and returns a new Service AST node.
func (f *File) NewService(name string) *Service {
	svc := &Service{
		Name:     name,
		Protocol: ProtocolHTTP,
		Engine:   EngineFast,
		Casing:   CasingSnake,
	}
	f.Services = append(f.Services, svc)

	return svc
}

// WithBaseURL sets the base URL for the service.
func (s *Service) WithBaseURL(url string) *Service {
	s.BaseURL = url
	return s
}

// WithEngine sets the execution client engine.
func (s *Service) WithEngine(engine EngineKind) *Service {
	s.Engine = engine
	return s
}

// WithCasing sets the default casing strategy.
func (s *Service) WithCasing(casing CasingStrategy) *Service {
	s.Casing = casing
	return s
}

// WithDoc sets the documentation lines for the service.
func (s *Service) WithDoc(lines ...string) *Service {
	s.Doc = append(s.Doc, lines...)
	return s
}

// WithHeader adds a default static header to the service.
func (s *Service) WithHeader(name, value string) *Service {
	s.Headers = append(s.Headers, Header{Name: name, Value: value})
	return s
}

// WithUnwrap sets service-wide field unwrapping (@unwrap "result").
func (s *Service) WithUnwrap(field string) *Service {
	s.UnwrapField = field
	return s
}

// NewMethod adds and returns a new Method node on the service.
func (s *Service) NewMethod(name, httpMethod, path string) *Method {
	m := &Method{
		Name:       name,
		HTTPMethod: httpMethod,
		Path:       path,
	}
	s.Methods = append(s.Methods, m)

	return m
}

// WithDoc sets the documentation lines for the method.
func (m *Method) WithDoc(lines ...string) *Method {
	m.Doc = append(m.Doc, lines...)
	return m
}

// WithRequest sets the request model Go type (e.g. "*SendMessageRequest").
func (m *Method) WithRequest(reqType string) *Method {
	m.RequestModel = reqType
	return m
}

// WithResponse sets the response model Go type (e.g. "*Message", "bool").
func (m *Method) WithResponse(respType string) *Method {
	m.ResponseModel = respType
	return m
}

// WithForm marks the method as using form-encoded body (@form).
func (m *Method) WithForm() *Method {
	m.Form = true
	return m
}

// WithUnwrap sets field unwrapping directive (@unwrap "field").
func (m *Method) WithUnwrap(field string) *Method {
	m.UnwrapField = field
	return m
}

// AddParam appends a positional method parameter.
func (m *Method) AddParam(name, paramType string) *Method {
	m.Parameters = append(m.Parameters, Parameter{Name: name, Type: paramType})
	return m
}

// NewStruct adds and returns a new Struct DTO node.
func (f *File) NewStruct(name string) *Struct {
	st := &Struct{
		Name:      name,
		Casing:    CasingSnake,
		Omitempty: true,
	}
	f.Structs = append(f.Structs, st)

	return st
}

// WithDoc sets the documentation lines for the struct.
func (s *Struct) WithDoc(lines ...string) *Struct {
	s.Doc = append(s.Doc, lines...)
	return s
}

// AddField appends a field to the struct.
func (s *Struct) AddField(name, fieldType, jsonName string, required bool) *Struct {
	s.Fields = append(s.Fields, &Field{
		Name:     name,
		Type:     fieldType,
		JSONName: jsonName,
		Required: required,
	})

	return s
}

// NewField creates and appends a field node to the struct with full customization.
func (s *Struct) NewField(name, fieldType string) *Field {
	f := &Field{
		Name: name,
		Type: fieldType,
	}
	s.Fields = append(s.Fields, f)

	return f
}

// WithQuery sets the URL query parameter serialization field name.
func (f *Field) WithQuery(queryName string) *Field {
	f.QueryName = queryName
	return f
}

// WithJSON sets the JSON serialization field name.
func (f *Field) WithJSON(jsonName string) *Field {
	f.JSONName = jsonName
	return f
}

// WithURL sets the URL query serialization field name.
func (f *Field) WithURL(urlName string) *Field {
	f.URLName = urlName
	return f
}

// WithAoniTag sets the positional tuple index tag (e.g. "0", "3.1").
func (f *Field) WithAoniTag(tag string) *Field {
	f.AoniTag = tag
	return f
}

// WithDoc sets the documentation lines for the field.
func (f *Field) WithDoc(lines ...string) *Field {
	f.Doc = append(f.Doc, lines...)
	return f
}

// SetRequired sets whether the field is mandatory.
func (f *Field) SetRequired(req bool) *Field {
	f.Required = req
	return f
}
