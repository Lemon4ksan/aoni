// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package patcher

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/ir"
	"github.com/lemon4ksan/aoni/internal/codegen/merge"
)

// Patcher performs non-destructive, surgical AST modifications on Go source files.
type Patcher struct{}

// NewPatcher creates an initialized Patcher instance.
func NewPatcher() *Patcher {
	return &Patcher{}
}

// PatchBytes applies semantic merge instructions directly onto Go source code bytes in-memory.
func (p *Patcher) PatchBytes(src []byte, plan *merge.ReconcileResult) ([]byte, error) {
	fset := token.NewFileSet()

	fileNode, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Go AST: %w", err)
	}

	// Map existing interfaces and structs in AST
	interfaces := make(map[string]*ast.InterfaceType)
	structs := make(map[string]*ast.StructType)

	for _, decl := range fileNode.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}

		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}

			if iface, isIface := typeSpec.Type.(*ast.InterfaceType); isIface {
				interfaces[typeSpec.Name.Name] = iface
			}

			if st, isSt := typeSpec.Type.(*ast.StructType); isSt {
				structs[typeSpec.Name.Name] = st
			}
		}
	}

	// 1. Patch Methods into Interfaces
	for _, mPlan := range plan.MethodPlans {
		iface, exists := interfaces[mPlan.Service]
		if !exists {
			continue
		}

		if mPlan.IsNew {
			newField := p.renderMethodField(mPlan.TargetMethod)

			if iface.Methods == nil {
				iface.Methods = &ast.FieldList{}
			}

			iface.Methods.List = append(iface.Methods.List, newField)
		}
	}

	// 2. Patch Structs and Fields
	for _, sPlan := range plan.StructPlans {
		if sPlan.IsNew {
			newDecl := p.renderStructDecl(sPlan.Target)
			fileNode.Decls = append(fileNode.Decls, newDecl)
			continue
		}

		st, exists := structs[sPlan.StructName]
		if !exists {
			continue
		}

		for _, nf := range sPlan.NewFields {
			fieldNode := p.renderStructField(nf)

			if st.Fields == nil {
				st.Fields = &ast.FieldList{}
			}

			st.Fields.List = append(st.Fields.List, fieldNode)
		}
	}

	// Render AST back to bytes
	var buf bytes.Buffer

	cfg := &printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
	if err := cfg.Fprint(&buf, fset, fileNode); err != nil {
		return nil, fmt.Errorf("failed to print patched AST: %w", err)
	}

	// Clean gofmt pass
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to gofmt patched AST: %w", err)
	}

	return formatted, nil
}

// PatchFile reads a file from disk, applies surgical AST patches in-memory, and writes back cleanly.
func (p *Patcher) PatchFile(targetPath string, plan *merge.ReconcileResult) error {
	cleanPath := filepath.Clean(targetPath)

	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return fmt.Errorf("cannot read target file %s: %w", cleanPath, err)
	}

	patched, err := p.PatchBytes(data, plan)
	if err != nil {
		return err
	}

	return os.WriteFile(cleanPath, patched, 0o600)
}

func (p *Patcher) renderMethodField(m *ir.MethodIR) *ast.Field {
	rawPath := ""
	if m.Path != nil {
		rawPath = m.Path.RawTemplate
	}

	docText := fmt.Sprintf("// %s calls %s %s.\n//\n// @%s %q\n",
		m.Name,
		strings.ToUpper(m.HTTPMethod),
		rawPath,
		strings.ToLower(m.HTTPMethod),
		rawPath,
	)

	lines := strings.Split(strings.TrimSpace(docText), "\n")

	docComments := make([]*ast.Comment, 0, len(lines))
	for _, line := range lines {
		docComments = append(docComments, &ast.Comment{Text: line})
	}

	params := make([]*ast.Field, 0, 2+len(m.Params))
	// 1. Context parameter
	params = append(params, &ast.Field{
		Names: []*ast.Ident{ast.NewIdent("ctx")},
		Type: &ast.SelectorExpr{
			X:   ast.NewIdent("context"),
			Sel: ast.NewIdent("Context"),
		},
	})

	// 2. Explicit method parameters
	for _, param := range m.Params {
		paramType := p.parseTypeExpr(param.GoType.Name)
		params = append(params, &ast.Field{
			Names: []*ast.Ident{ast.NewIdent(param.GoName)},
			Type:  paramType,
		})
	}

	// 3. Variadic modifier parameter
	params = append(params, &ast.Field{
		Names: []*ast.Ident{ast.NewIdent("mods")},
		Type: &ast.Ellipsis{
			Elt: &ast.SelectorExpr{
				X:   ast.NewIdent("aoni"),
				Sel: ast.NewIdent("RequestModifier"),
			},
		},
	})

	// Returns: (*json.RawMessage, error)
	returns := make([]*ast.Field, 0, 2)
	returns = append(returns, &ast.Field{
		Type: &ast.StarExpr{
			X: &ast.SelectorExpr{
				X:   ast.NewIdent("json"),
				Sel: ast.NewIdent("RawMessage"),
			},
		},
	})
	returns = append(returns, &ast.Field{
		Type: ast.NewIdent("error"),
	})

	funcType := &ast.FuncType{
		Params:  &ast.FieldList{List: params},
		Results: &ast.FieldList{List: returns},
	}

	return &ast.Field{
		Doc:   &ast.CommentGroup{List: docComments},
		Names: []*ast.Ident{ast.NewIdent(m.Name)},
		Type:  funcType,
	}
}

func (p *Patcher) renderStructDecl(st *ir.StructIR) *ast.GenDecl {
	docComments := []*ast.Comment{
		{
			Text: fmt.Sprintf(
				"// %s represents request parameters.\n//\n// @aoni:dto casing=snake_case omitempty=true",
				st.Name,
			),
		},
	}

	fields := make([]*ast.Field, 0, len(st.Fields))
	for _, f := range st.Fields {
		fields = append(fields, p.renderStructField(f))
	}

	structType := &ast.StructType{
		Fields: &ast.FieldList{List: fields},
	}

	typeSpec := &ast.TypeSpec{
		Name: ast.NewIdent(st.Name),
		Type: structType,
	}

	return &ast.GenDecl{
		Doc:   &ast.CommentGroup{List: docComments},
		Tok:   token.TYPE,
		Specs: []ast.Spec{typeSpec},
	}
}

func (p *Patcher) renderStructField(f *ir.FieldIR) *ast.Field {
	tagValue := f.WireName
	if tagValue == "" {
		tagValue = strings.ToLower(f.GoName)
	}

	tagLit := &ast.BasicLit{
		Kind:  token.STRING,
		Value: fmt.Sprintf("`url:%q`", tagValue+",omitempty"),
	}

	typeExpr := p.parseTypeExpr(f.Type.Name)

	return &ast.Field{
		Names: []*ast.Ident{ast.NewIdent(f.GoName)},
		Type:  typeExpr,
		Tag:   tagLit,
	}
}

func (p *Patcher) parseTypeExpr(typeName string) ast.Expr {
	if typeName == "" {
		return ast.NewIdent("string")
	}

	expr, err := parser.ParseExpr(typeName)
	if err == nil && expr != nil {
		return expr
	}

	return ast.NewIdent(typeName)
}
