// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package js provides a lightweight, fluent Abstract Syntax Tree (AST)
// and code generator for JavaScript/TypeScript.
// Core implementation is located in [github.com/lemon4ksan/foundation/ast/js].
package js

import (
	fjs "github.com/lemon4ksan/foundation/ast/js"
)

// Node represents any JavaScript AST node.
type Node = fjs.Node

// Stmt represents a JavaScript statement.
type Stmt = fjs.Stmt

// Expr represents a JavaScript expression.
type Expr = fjs.Expr

// Program is the root AST node representing a complete JS file/module.
type Program = fjs.Program

// RawStmt represents a raw JavaScript statement.
type RawStmt = fjs.RawStmt

// RawExpr represents a raw JavaScript expression.
type RawExpr = fjs.RawExpr

// Literal represents a literal primitive value.
type Literal = fjs.Literal

// Ident represents an identifier or variable name.
type Ident = fjs.Ident

// VarDecl represents const, let, or var declarations.
type VarDecl = fjs.VarDecl

// CallExpr represents a function or method invocation.
type CallExpr = fjs.CallExpr

// AwaitExpr represents an await expression.
type AwaitExpr = fjs.AwaitExpr

// FunctionDecl represents a function declaration.
type FunctionDecl = fjs.FunctionDecl

// IfStmt represents an if/else control flow statement.
type IfStmt = fjs.IfStmt

// ReturnStmt represents a return statement.
type ReturnStmt = fjs.ReturnStmt

// ThrowStmt represents a throw statement.
type ThrowStmt = fjs.ThrowStmt

// TryCatchStmt represents a try-catch-finally statement.
type TryCatchStmt = fjs.TryCatchStmt

// ObjectExpr represents a JavaScript object literal.
type ObjectExpr = fjs.ObjectExpr

// Property represents a key-value property in an ObjectExpr.
type Property = fjs.Property

// ArrayExpr represents an array literal.
type ArrayExpr = fjs.ArrayExpr

// FuncBuilder provides a fluent chain to construct function declarations.
type FuncBuilder = fjs.FuncBuilder

// IfBuilder provides a fluent chain to construct if/else statements.
type IfBuilder = fjs.IfBuilder

// TryBuilder provides a fluent chain to construct try/catch blocks.
type TryBuilder = fjs.TryBuilder

// NewProgram creates a root Program node from a list of statements.
func NewProgram(stmts ...Stmt) *Program {
	return fjs.NewProgram(stmts...)
}

// Raw creates a raw expression snippet.
func Raw(code string) RawExpr {
	return fjs.Raw(code)
}

// StmtRaw creates a raw statement snippet.
func StmtRaw(code string) RawStmt {
	return fjs.StmtRaw(code)
}

// Require generates `const x = require('x');` statements for each module.
func Require(modules ...string) Stmt {
	return fjs.Require(modules...)
}

// RequireFrom generates `const { a, b } = require('pkg');`.
func RequireFrom(pkg string, imports ...string) Stmt {
	return fjs.RequireFrom(pkg, imports...)
}

// Const declares a constant.
func Const(name string, value any) Stmt {
	return fjs.Const(name, value)
}

// Let declares a mutable variable.
func Let(name string, value any) Stmt {
	return fjs.Let(name, value)
}

// Return creates a return statement.
func Return(value any) Stmt {
	return fjs.Return(value)
}

// Throw creates a throw statement.
func Throw(errType, message string) Stmt {
	return fjs.Throw(errType, message)
}

// Call creates a function or method invocation.
func Call(target string, args ...any) *CallExpr {
	return fjs.Call(target, args...)
}

// Await creates an await expression or statement.
func Await(target string, args ...any) *AwaitExpr {
	return fjs.Await(target, args...)
}

// Fn starts building a named function declaration.
func Fn(name string) *FuncBuilder {
	return fjs.Fn(name)
}

// If starts building an if conditional statement.
func If(cond any) *IfBuilder {
	return fjs.If(cond)
}

// Try starts building a try/catch block.
func Try(stmts ...Stmt) *TryBuilder {
	return fjs.Try(stmts...)
}

// ToExpr coerces strings, primitives, and AST nodes into Expr.
func ToExpr(v any) Expr {
	return fjs.ToExpr(v)
}

// Format renders a JS AST Node into a formatted JavaScript code string.
func Format(node Node) (string, error) {
	return fjs.Format(node)
}
