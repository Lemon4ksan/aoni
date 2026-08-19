// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package js

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Format renders a JS AST Node into a formatted JavaScript code string.
func Format(node Node) (string, error) {
	if node == nil {
		return "", nil
	}

	p := &printer{indent: 0}
	p.printNode(node)

	return p.buf.String(), nil
}

type printer struct {
	buf    bytes.Buffer
	indent int
}

func (p *printer) writeIndent() {
	for i := 0; i < p.indent; i++ {
		p.buf.WriteString("  ")
	}
}

func (p *printer) printNode(node Node) {
	switch n := node.(type) {
	case *Program:
		for i, stmt := range n.Body {
			if i > 0 {
				p.buf.WriteString("\n")
			}

			p.printStmt(stmt)
		}

	case Program:
		p.printNode(&n)

	case Stmt:
		p.printStmt(n)

	case Expr:
		p.printExpr(n)
	}
}

func (p *printer) printStmt(stmt Stmt) {
	if stmt == nil {
		return
	}

	p.writeIndent()

	switch s := stmt.(type) {
	case RawStmt:
		p.buf.WriteString(s.Code)

		if !strings.HasSuffix(s.Code, ";") && !strings.HasSuffix(s.Code, "}") {
			p.buf.WriteString(";")
		}

		p.buf.WriteString("\n")

	case VarDecl:
		p.buf.WriteString(s.Kind + " " + s.Name + " = ")
		p.printExpr(s.Value)
		p.buf.WriteString(";\n")

	case ReturnStmt:
		p.buf.WriteString("return")

		if s.Argument != nil {
			p.buf.WriteString(" ")
			p.printExpr(s.Argument)
		}

		p.buf.WriteString(";\n")

	case ThrowStmt:
		p.buf.WriteString("throw ")
		p.printExpr(s.Argument)
		p.buf.WriteString(";\n")

	case *FunctionDecl:
		p.printFunction(s)

	case FunctionDecl:
		p.printFunction(&s)

	case *IfStmt:
		p.printIf(s)

	case IfStmt:
		p.printIf(&s)

	case *IfBuilder:
		p.printIf(s.Node())

	case *TryCatchStmt:
		p.printTryCatch(s)

	case TryCatchStmt:
		p.printTryCatch(&s)

	case *TryBuilder:
		p.printTryCatch(s.Node())

	case *CallExpr:
		p.printExpr(s)
		p.buf.WriteString(";\n")

	case *AwaitExpr:
		p.printExpr(s)
		p.buf.WriteString(";\n")

	default:
		p.buf.WriteString("// unknown statement\n")
	}
}

func (p *printer) printFunction(fn *FunctionDecl) {
	if fn.Async {
		p.buf.WriteString("async ")
	}

	p.buf.WriteString("function")

	if fn.Name != "" {
		p.buf.WriteString(" " + fn.Name)
	}

	p.buf.WriteString("(" + strings.Join(fn.Params, ", ") + ") {\n")

	p.indent++
	for _, s := range fn.Body {
		p.printStmt(s)
	}

	p.indent--

	p.writeIndent()
	p.buf.WriteString("}\n")
}

func (p *printer) printIf(s *IfStmt) {
	p.buf.WriteString("if (")
	p.printExpr(s.Test)
	p.buf.WriteString(") {\n")

	p.indent++
	for _, st := range s.Consequent {
		p.printStmt(st)
	}

	p.indent--

	p.writeIndent()
	p.buf.WriteString("}")

	if len(s.Alternate) > 0 {
		p.buf.WriteString(" else {\n")

		p.indent++
		for _, st := range s.Alternate {
			p.printStmt(st)
		}

		p.indent--
		p.writeIndent()
		p.buf.WriteString("}")
	}

	p.buf.WriteString("\n")
}

func (p *printer) printTryCatch(s *TryCatchStmt) {
	p.buf.WriteString("try {\n")

	p.indent++
	for _, st := range s.Block {
		p.printStmt(st)
	}

	p.indent--

	p.writeIndent()

	handler := s.Handler
	if handler == "" {
		handler = "err"
	}

	p.buf.WriteString("} catch (" + handler + ") {\n")

	p.indent++
	for _, st := range s.Catch {
		p.printStmt(st)
	}

	p.indent--

	p.writeIndent()
	p.buf.WriteString("}")

	if len(s.Finally) > 0 {
		p.buf.WriteString(" finally {\n")

		p.indent++
		for _, st := range s.Finally {
			p.printStmt(st)
		}

		p.indent--
		p.writeIndent()
		p.buf.WriteString("}")
	}

	p.buf.WriteString("\n")
}

func (p *printer) printExpr(expr Expr) {
	if expr == nil {
		return
	}

	switch e := expr.(type) {
	case RawExpr:
		p.buf.WriteString(e.Code)

	case Ident:
		p.buf.WriteString(e.Name)

	case Literal:
		b, _ := json.Marshal(e.Value)
		p.buf.Write(b)

	case *CallExpr:
		p.printExpr(e.Callee)
		p.buf.WriteString("(")

		for i, a := range e.Args {
			if i > 0 {
				p.buf.WriteString(", ")
			}

			p.printExpr(a)
		}

		p.buf.WriteString(")")

	case *AwaitExpr:
		p.buf.WriteString("await ")
		p.printExpr(e.Argument)

	case ObjectExpr:
		p.buf.WriteString("{")

		for i, prop := range e.Properties {
			if i > 0 {
				p.buf.WriteString(", ")
			}

			p.buf.WriteString(prop.Key + ": ")
			p.printExpr(prop.Value)
		}

		p.buf.WriteString("}")

	case ArrayExpr:
		p.buf.WriteString("[")

		for i, elem := range e.Elements {
			if i > 0 {
				p.buf.WriteString(", ")
			}

			p.printExpr(elem)
		}

		p.buf.WriteString("]")

	default:
		p.buf.WriteString("null")
	}
}
