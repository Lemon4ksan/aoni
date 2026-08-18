// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package text

// Node represents an element in the semantic document tree.
type Node interface {
	isNode()
}

// FieldStyle defines the rendering style of a field value.
type FieldStyle uint8

const (
	// FieldPlain renders standard key-value text.
	FieldPlain FieldStyle = iota
	// FieldCode renders the value wrapped in monospace formatting.
	FieldCode
	// FieldBold renders the value in bold emphasis.
	FieldBold
)

// ListKind defines the list numbering and bullet type.
type ListKind uint8

const (
	// ListBullet renders unordered bullet points (•).
	ListBullet ListKind = iota
	// ListNumbered renders ordered sequential numbers (1., 2.).
	ListNumbered
)

// Align specifies table column alignment.
type Align uint8

const (
	// AlignLeft aligns column content to the left.
	AlignLeft Align = iota
	// AlignCenter aligns column content to the center.
	AlignCenter
	// AlignRight aligns column content to the right.
	AlignRight
)

// HeadingNode represents a top-level document heading.
type HeadingNode struct {
	Level int
	Text  string
	Icon  string
}

func (HeadingNode) isNode() {}

// SectionNode represents a categorized section divider with an optional icon.
type SectionNode struct {
	Name string
	Icon string
}

func (SectionNode) isNode() {}

// ParagraphNode represents a standard text paragraph followed by a newline separator.
type ParagraphNode struct {
	Text string
}

func (ParagraphNode) isNode() {}

// FieldNode represents a structured key-value line.
type FieldNode struct {
	Key   string
	Value string
	Style FieldStyle
}

func (FieldNode) isNode() {}

// ListNode represents a sequential or bulleted list of items.
type ListNode struct {
	Kind  ListKind
	Items []string
}

func (ListNode) isNode() {}

// CalloutNode represents an emphasized banner with an associated semantic intent.
type CalloutNode struct {
	Intent Intent
	Title  string
	Body   string
}

func (CalloutNode) isNode() {}

// CodeBlockNode represents a syntax-highlighted or fenced code snippet.
type CodeBlockNode struct {
	Language string
	Code     string
}

func (CodeBlockNode) isNode() {}

// QuoteNode represents an indented blockquote.
type QuoteNode struct {
	Text       string
	Expandable bool
}

func (QuoteNode) isNode() {}

// DividerNode represents a visual separator or horizontal rule.
type DividerNode struct{}

func (DividerNode) isNode() {}

// TableNode represents a multi-column structured data table.
type TableNode struct {
	Headers []string
	Aligns  []Align
	Rows    [][]string
}

func (TableNode) isNode() {}

// RawNode represents an unescaped, pre-formatted raw string inserted verbatim.
type RawNode struct {
	Content string
}

func (RawNode) isNode() {}
