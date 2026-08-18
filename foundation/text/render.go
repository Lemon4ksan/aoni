// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package text

import "io"

// Renderer converts a semantic [Document] tree into a concrete string format.
type Renderer interface {
	// Render writes the formatted document representation to w.
	Render(w io.Writer, doc *Document) error
}

var (
	// DefaultMarkdownRenderer is the standard CommonMark / GitHub Markdown renderer.
	DefaultMarkdownRenderer = NewMarkdownRenderer()

	// DefaultTerminalRenderer is the standard ANSI color & tabular terminal renderer.
	DefaultTerminalRenderer = NewTerminalRenderer()

	// DefaultPlainRenderer is the unadorned plaintext renderer.
	DefaultPlainRenderer = NewPlainRenderer()
)
