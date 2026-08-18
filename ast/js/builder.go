// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package js

import (
	"fmt"
	"strings"
)

// NewProgram creates a root Program node from a list of statements.
func NewProgram(stmts ...Stmt) *Program {
	return &Program{Body: stmts}
}

// Raw creates a raw expression snippet.
func Raw(code string) RawExpr {
	return RawExpr{Code: code}
}

// StmtRaw creates a raw statement snippet.
func StmtRaw(code string) RawStmt {
	return RawStmt{Code: code}
}

// Require generates `const x = require('x');` statements for each module.
func Require(modules ...string) Stmt {
	var sb strings.Builder
	for i, m := range modules {
		if i > 0 {
			sb.WriteString("\n")
		}

		fmt.Fprintf(&sb, "const %s = require('%s');", m, m)
	}

	return RawStmt{Code: sb.String()}
}

// RequireFrom generates `const { a, b } = require('pkg');`.
func RequireFrom(pkg string, imports ...string) Stmt {
	return RawStmt{
		Code: fmt.Sprintf("const { %s } = require('%s');", strings.Join(imports, ", "), pkg),
	}
}

// Const declares a constant: `const name = value;`.
func Const(name string, value any) Stmt {
	return VarDecl{
		Kind:  "const",
		Name:  name,
		Value: ToExpr(value),
	}
}

// Let declares a mutable variable: `let name = value;`.
func Let(name string, value any) Stmt {
	return VarDecl{
		Kind:  "let",
		Name:  name,
		Value: ToExpr(value),
	}
}

// Return creates a return statement.
func Return(value any) Stmt {
	return ReturnStmt{Argument: ToExpr(value)}
}

// Throw creates a `throw new ErrorType(message);` statement.
func Throw(errType, message string) Stmt {
	if errType == "" {
		errType = "Error"
	}

	return ThrowStmt{
		Argument: RawExpr{
			Code: fmt.Sprintf("new %s(%q)", errType, message),
		},
	}
}

// Call creates a function or method invocation.
func Call(target string, args ...any) *CallExpr {
	exprArgs := make([]Expr, 0, len(args))
	for _, a := range args {
		exprArgs = append(exprArgs, ToExpr(a))
	}

	return &CallExpr{
		Callee: ToExpr(target),
		Args:   exprArgs,
	}
}

// Await creates an await expression or statement.
func Await(target string, args ...any) *AwaitExpr {
	if len(args) == 0 && strings.Contains(target, "(") {
		return &AwaitExpr{Argument: RawExpr{Code: target}}
	}

	return &AwaitExpr{
		Argument: Call(target, args...),
	}
}

// FuncBuilder provides a fluent chain to construct function declarations.
type FuncBuilder struct {
	fn FunctionDecl
}

// Fn starts building a named function declaration.
func Fn(name string) *FuncBuilder {
	return &FuncBuilder{
		fn: FunctionDecl{Name: name},
	}
}

// Async marks the function as async.
func (b *FuncBuilder) Async() *FuncBuilder {
	b.fn.Async = true
	return b
}

// Args specifies the function parameter names.
func (b *FuncBuilder) Args(params ...string) *FuncBuilder {
	b.fn.Params = params
	return b
}

// Body completes the function declaration with statements.
func (b *FuncBuilder) Body(stmts ...Stmt) *FunctionDecl {
	b.fn.Body = stmts
	return &b.fn
}

// IfBuilder provides a fluent chain to construct if/else statements.
type IfBuilder struct {
	ifStmt IfStmt
}

// If starts building an if conditional statement.
func If(cond any) *IfBuilder {
	return &IfBuilder{
		ifStmt: IfStmt{Test: ToExpr(cond)},
	}
}

// Then specifies the statements to execute when the condition is truthy.
func (b *IfBuilder) Then(stmts ...Stmt) *IfBuilder {
	b.ifStmt.Consequent = stmts
	return b
}

// Else specifies the statements to execute when the condition is falsy.
func (b *IfBuilder) Else(stmts ...Stmt) *IfStmt {
	b.ifStmt.Alternate = stmts
	return &b.ifStmt
}

// Node returns the constructed IfStmt node.
func (b *IfBuilder) Node() *IfStmt {
	return &b.ifStmt
}

func (*IfBuilder) isJSNode() {}
func (*IfBuilder) isJSStmt() {}

// TryBuilder provides a fluent chain to construct try/catch blocks.
type TryBuilder struct {
	stmt TryCatchStmt
}

// Try starts building a try/catch block.
func Try(stmts ...Stmt) *TryBuilder {
	return &TryBuilder{
		stmt: TryCatchStmt{Block: stmts},
	}
}

// Catch specifies the catch handler and statements.
func (b *TryBuilder) Catch(errVar string, stmts ...Stmt) *TryBuilder {
	b.stmt.Handler = errVar
	b.stmt.Catch = stmts
	return b
}

// Finally specifies the finally statements and completes the TryCatchStmt.
func (b *TryBuilder) Finally(stmts ...Stmt) *TryCatchStmt {
	b.stmt.Finally = stmts
	return &b.stmt
}

// Node returns the constructed TryCatchStmt node.
func (b *TryBuilder) Node() *TryCatchStmt {
	return &b.stmt
}

func (*TryBuilder) isJSNode() {}
func (*TryBuilder) isJSStmt() {}

// ToExpr coerces strings, primitives, and AST nodes into Expr.
func ToExpr(v any) Expr {
	if v == nil {
		return Literal{Value: nil}
	}

	switch val := v.(type) {
	case Expr:
		return val
	case string:
		if strings.ContainsAny(val, " .()=!+-/*[]><") {
			return RawExpr{Code: val}
		}

		return Ident{Name: val}

	case int, int64, float64, bool:
		return Literal{Value: val}
	default:
		return RawExpr{Code: fmt.Sprintf("%v", val)}
	}
}
