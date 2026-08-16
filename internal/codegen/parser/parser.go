// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package parser

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/ir"
)

// Parser inspects Go source files and extracts declarative API service definitions and DTOs into an Unchecked IR.
type Parser struct {
	fset *token.FileSet
}

// NewParser creates a new Parser instance.
func NewParser() *Parser {
	return &Parser{
		fset: token.NewFileSet(),
	}
}

// ParseFile parses a Go source file from disk.
func (p *Parser) ParseFile(filePath string) (*ir.RootIR, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("aoni/codegen/parser: failed to read file %q: %w", filePath, err)
	}

	return p.ParseSource(filePath, data)
}

// ParsePackage parses all Go source files in the specified directory into a unified RootIR.
func (p *Parser) ParsePackage(dirPath string) (*ir.RootIR, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("aoni/codegen/parser: failed to read dir %q: %w", dirPath, err)
	}

	root := &ir.RootIR{
		Imports:  make([]ir.ImportIR, 0),
		Services: make([]*ir.ServiceIR, 0),
		Structs:  make([]*ir.StructIR, 0),
		Tuples:   make([]*ir.TupleIR, 0),
	}

	seenStructs := make(map[string]bool)
	seenServices := make(map[string]bool)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") ||
			strings.HasSuffix(entry.Name(), "_test.go") || strings.HasSuffix(entry.Name(), ".gen.go") {
			continue
		}

		fullPath := filepath.Join(dirPath, entry.Name())

		data, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}

		subRoot, err := p.ParseSource(fullPath, data)
		if err != nil {
			continue
		}

		if root.PackageName == "" {
			root.PackageName = subRoot.PackageName
		}

		root.Imports = append(root.Imports, subRoot.Imports...)

		for _, s := range subRoot.Services {
			if !seenServices[s.Name] {
				seenServices[s.Name] = true
				root.Services = append(root.Services, s)
			}
		}

		for _, st := range subRoot.Structs {
			if !seenStructs[st.Name] {
				seenStructs[st.Name] = true
				root.Structs = append(root.Structs, st)
			}
		}

		root.Tuples = append(root.Tuples, subRoot.Tuples...)
	}

	return root, nil
}

// ParseSource parses Go source code from bytes.
func (p *Parser) ParseSource(filename string, src []byte) (*ir.RootIR, error) {
	file, err := parser.ParseFile(p.fset, filename, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("aoni/codegen/parser: syntax error in %q: %w", filename, err)
	}

	root := &ir.RootIR{
		PackageName: file.Name.Name,
		SourceFile:  filepath.ToSlash(filename),
		Imports:     make([]ir.ImportIR, 0, len(file.Imports)),
		Services:    make([]*ir.ServiceIR, 0),
		Structs:     make([]*ir.StructIR, 0),
		Tuples:      make([]*ir.TupleIR, 0),
	}

	for _, imp := range file.Imports {
		alias := ""
		if imp.Name != nil {
			alias = imp.Name.Name
		}

		pathVal := strings.Trim(imp.Path.Value, "\"")
		root.Imports = append(root.Imports, ir.ImportIR{
			Alias: alias,
			Path:  pathVal,
		})
	}

	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}

		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}

			docLines := extractDocLines(genDecl.Doc, typeSpec.Doc)

			switch t := typeSpec.Type.(type) {
			case *ast.InterfaceType:
				if svc := p.parseInterface(root, file.Comments, typeSpec.Name.Name, docLines, t); svc != nil {
					root.Services = append(root.Services, svc)
				}
			case *ast.StructType:
				if strct := p.parseStruct(root, typeSpec.Name.Name, docLines, t); strct != nil {
					root.Structs = append(root.Structs, strct)
				}

				if tuple := p.parseTuple(root, typeSpec.Name.Name, docLines, t); tuple != nil {
					root.Tuples = append(root.Tuples, tuple)
				}

				if bitpack := p.parseBitpack(root, typeSpec.Name.Name, docLines, t); bitpack != nil {
					root.Bitpacks = append(root.Bitpacks, bitpack)
				}

				if union := p.parseUnion(root, typeSpec.Name.Name, docLines, t); union != nil {
					root.Unions = append(root.Unions, union)
				}
			}
		}
	}

	p.linkUnionsAndDefaults(root)

	return root, nil
}

func (p *Parser) linkUnionsAndDefaults(root *ir.RootIR) {
	unionMap := make(map[string]*ir.UnionIR)
	for _, u := range root.Unions {
		unionMap[u.Name] = u
	}

	for _, svc := range root.Services {
		for _, m := range svc.Methods {
			if m.Return == nil {
				continue
			}

			if m.Return.ErrorModelType == "" && svc.DefaultErrorModel != "" {
				m.Return.ErrorModelType = svc.DefaultErrorModel
			}

			cleanReturn := strings.TrimPrefix(m.Return.SuccessType.Name, "*")
			if u, ok := unionMap[cleanReturn]; ok {
				m.Return.UnionType = u
				if m.Return.StatusMap == nil {
					m.Return.StatusMap = make(map[int]ir.GoTypeIR)
				}

				for code, variant := range u.Variants {
					if _, exists := m.Return.StatusMap[code]; !exists {
						m.Return.StatusMap[code] = variant
					}
				}
			}
		}
	}
}

func extractDocLines(docs ...*ast.CommentGroup) []string {
	var lines []string
	for _, doc := range docs {
		if doc == nil {
			continue
		}

		for _, comment := range doc.List {
			lines = append(lines, comment.Text)
		}
	}

	return lines
}

func (p *Parser) extractDirectives(root *ir.RootIR, target string, lines []string) []*Directive {
	var list []*Directive
	for _, l := range lines {
		if d := ParseDirective(l); d != nil {
			list = append(list, d)
			if root != nil && !IsKnownDirective(d.Name) {
				root.UnrecognizedDirectives = append(root.UnrecognizedDirectives, ir.UnrecognizedDirectiveIR{
					Target: target,
					Name:   d.Name,
				})
			}
		}
	}

	return list
}

func hasServiceDirective(directives []*Directive) bool {
	for _, d := range directives {
		if d.Name == "aoni:service" || d.Name == "service" || d.Name == "base_url" ||
			d.Name == "aoni:socket" || d.Name == "socket" {
			return true
		}
	}

	return false
}
