// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package parser extracts Go interface declarations, structs, and doc comment directives into an Unchecked IR.
//
// The parser inspects Go source files using standard go/parser and go/ast tools, identifying
// compiler directives prefixed with `@` in Godoc comments.
package parser
