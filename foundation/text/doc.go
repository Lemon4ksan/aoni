// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package text provides a platform-agnostic, zero-allocation semantic document
// model, fluent builder, token shielding engine, and multi-target renderers.
//
// The core philosophy follows a clean separation of concerns:
//  1. Document AST: Composed of semantic blocks (Headings, Fields, Lists, Code, Callouts).
//  2. Fluent Builder: Ergonomic, order-preserving construction with automatic spacing.
//  3. Shield Engine: Binary-safe lexer protection for fragile token spans during text transforms.
//  4. Pluggable Renderers: CommonMark Markdown, Terminal ANSI, and Plain Text emitters.
package text
