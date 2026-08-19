// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package js provides a lightweight, fluent Abstract Syntax Tree (AST)
// and code generator for JavaScript/TypeScript.
package js

// Node represents any JavaScript AST node.
type Node interface {
	isJSNode()
}

// Stmt represents a JavaScript statement.
type Stmt interface {
	Node
	isJSStmt()
}

// Expr represents a JavaScript expression.
type Expr interface {
	Node
	isJSExpr()
}

// Program is the root AST node representing a complete JS file/module.
type Program struct {
	Body []Stmt
}

func (Program) isJSNode() {}

// RawStmt represents a raw JavaScript statement.
type RawStmt struct {
	Code string
}

func (RawStmt) isJSNode() {}
func (RawStmt) isJSStmt() {}

// RawExpr represents a raw JavaScript expression.
type RawExpr struct {
	Code string
}

func (RawExpr) isJSNode() {}
func (RawExpr) isJSExpr() {}

// Literal represents a literal primitive value (string, number, boolean, null).
type Literal struct {
	Value any
}

func (Literal) isJSNode() {}
func (Literal) isJSExpr() {}

// Ident represents an identifier or variable name.
type Ident struct {
	Name string
}

func (Ident) isJSNode() {}
func (Ident) isJSExpr() {}

// VarDecl represents const, let, or var declarations.
type VarDecl struct {
	Kind  string // "const", "let", "var"
	Name  string
	Value Expr
}

func (VarDecl) isJSNode() {}
func (VarDecl) isJSStmt() {}

// CallExpr represents a function or method invocation.
type CallExpr struct {
	Callee Expr
	Args   []Expr
}

func (CallExpr) isJSNode() {}
func (CallExpr) isJSExpr() {}
func (CallExpr) isJSStmt() {}

// AwaitExpr represents an await expression.
type AwaitExpr struct {
	Argument Expr
}

func (AwaitExpr) isJSNode() {}
func (AwaitExpr) isJSExpr() {}
func (AwaitExpr) isJSStmt() {}

// FunctionDecl represents a function declaration.
type FunctionDecl struct {
	Name   string
	Async  bool
	Params []string
	Body   []Stmt
}

func (FunctionDecl) isJSNode() {}
func (FunctionDecl) isJSStmt() {}
func (FunctionDecl) isJSExpr() {}

// IfStmt represents an if/else control flow statement.
type IfStmt struct {
	Test       Expr
	Consequent []Stmt
	Alternate  []Stmt
}

func (IfStmt) isJSNode() {}
func (IfStmt) isJSStmt() {}

// ReturnStmt represents a return statement.
type ReturnStmt struct {
	Argument Expr
}

func (ReturnStmt) isJSNode() {}
func (ReturnStmt) isJSStmt() {}

// ThrowStmt represents a throw statement.
type ThrowStmt struct {
	Argument Expr
}

func (ThrowStmt) isJSNode() {}
func (ThrowStmt) isJSStmt() {}

// TryCatchStmt represents a try-catch-finally statement.
type TryCatchStmt struct {
	Block   []Stmt
	Handler string
	Catch   []Stmt
	Finally []Stmt
}

func (TryCatchStmt) isJSNode() {}
func (TryCatchStmt) isJSStmt() {}

// ObjectExpr represents a JavaScript object literal { key: value }.
type ObjectExpr struct {
	Properties []Property
}

// Property represents a key-value property in an ObjectExpr.
type Property struct {
	Key   string
	Value Expr
}

func (ObjectExpr) isJSNode() {}
func (ObjectExpr) isJSExpr() {}

// ArrayExpr represents an array literal [ a, b, c ].
type ArrayExpr struct {
	Elements []Expr
}

func (ArrayExpr) isJSNode() {}
func (ArrayExpr) isJSExpr() {}
