// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package text

import (
	"bytes"
	"fmt"
	"io"
	"sync"
)

// Document contains an ordered collection of semantic [Node] elements.
type Document struct {
	Nodes []Node
}

// Len returns the count of nodes in the document.
func (d *Document) Len() int {
	if d == nil {
		return 0
	}

	return len(d.Nodes)
}

// DocumentBuilder provides a fluent, ergonomic interface for constructing structured documents.
type DocumentBuilder struct {
	nodes []Node
}

var docPool = sync.Pool{
	New: func() any {
		return &DocumentBuilder{
			nodes: make([]Node, 0, 16),
		}
	},
}

// NewDocument creates a fresh, empty [DocumentBuilder].
func NewDocument() *DocumentBuilder {
	b, ok := docPool.Get().(*DocumentBuilder)
	if !ok {
		return &DocumentBuilder{nodes: make([]Node, 0, 16)}
	}

	b.nodes = b.nodes[:0]

	return b
}

// Release returns the builder to the internal sync pool for reuse.
// The builder must not be used after calling Release.
func (b *DocumentBuilder) Release() {
	if b == nil {
		return
	}

	b.nodes = b.nodes[:0]
	docPool.Put(b)
}

// Reset clears all accumulated nodes in the builder.
func (b *DocumentBuilder) Reset() *DocumentBuilder {
	b.nodes = b.nodes[:0]

	return b
}

// Title appends a level 1 heading with an optional icon prefix.
func (b *DocumentBuilder) Title(icon, text string) *DocumentBuilder {
	return b.Heading(1, icon, text)
}

// Heading appends a document heading at the given hierarchy level (1 to 6).
func (b *DocumentBuilder) Heading(level int, icon, text string) *DocumentBuilder {
	if level < 1 {
		level = 1
	} else if level > 6 {
		level = 6
	}

	b.nodes = append(b.nodes, HeadingNode{
		Level: level,
		Icon:  icon,
		Text:  text,
	})

	return b
}

// Section appends a categorized section heading with an optional icon prefix.
func (b *DocumentBuilder) Section(icon, name string) *DocumentBuilder {
	b.nodes = append(b.nodes, SectionNode{
		Icon: icon,
		Name: name,
	})

	return b
}

// Paragraph appends a standard text paragraph followed by a newline block separator.
func (b *DocumentBuilder) Paragraph(text string) *DocumentBuilder {
	b.nodes = append(b.nodes, ParagraphNode{Text: text})

	return b
}

// Text is an alias for [Paragraph].
func (b *DocumentBuilder) Text(text string) *DocumentBuilder {
	return b.Paragraph(text)
}

// Field appends a standard key-value line.
func (b *DocumentBuilder) Field(key, value string) *DocumentBuilder {
	b.nodes = append(b.nodes, FieldNode{
		Key:   key,
		Value: value,
		Style: FieldPlain,
	})

	return b
}

// FieldCode appends a key-value line where the value is styled as monospace code.
func (b *DocumentBuilder) FieldCode(key, value string) *DocumentBuilder {
	b.nodes = append(b.nodes, FieldNode{
		Key:   key,
		Value: value,
		Style: FieldCode,
	})

	return b
}

// FieldBold appends a key-value line where the value is styled with bold emphasis.
func (b *DocumentBuilder) FieldBold(key, value string) *DocumentBuilder {
	b.nodes = append(b.nodes, FieldNode{
		Key:   key,
		Value: value,
		Style: FieldBold,
	})

	return b
}

// Bullet appends one or more items formatted as an unordered bullet list.
func (b *DocumentBuilder) Bullet(items ...string) *DocumentBuilder {
	if len(items) == 0 {
		return b
	}

	b.nodes = append(b.nodes, ListNode{
		Kind:  ListBullet,
		Items: append([]string(nil), items...),
	})

	return b
}

// Numbered appends one or more items formatted as an ordered sequential list.
func (b *DocumentBuilder) Numbered(items ...string) *DocumentBuilder {
	if len(items) == 0 {
		return b
	}

	b.nodes = append(b.nodes, ListNode{
		Kind:  ListNumbered,
		Items: append([]string(nil), items...),
	})

	return b
}

// Callout appends an emphasized banner associated with the specified semantic intent.
func (b *DocumentBuilder) Callout(intent Intent, title, body string) *DocumentBuilder {
	b.nodes = append(b.nodes, CalloutNode{
		Intent: intent,
		Title:  title,
		Body:   body,
	})

	return b
}

// Success appends a success banner (IntentSuccess).
func (b *DocumentBuilder) Success(title, body string) *DocumentBuilder {
	return b.Callout(IntentSuccess, title, body)
}

// Warning appends a warning banner (IntentWarning).
func (b *DocumentBuilder) Warning(title, body string) *DocumentBuilder {
	return b.Callout(IntentWarning, title, body)
}

// Danger appends an error / danger banner (IntentDanger).
func (b *DocumentBuilder) Danger(title, body string) *DocumentBuilder {
	return b.Callout(IntentDanger, title, body)
}

// Info appends an informational note banner (IntentInfo).
func (b *DocumentBuilder) Info(title, body string) *DocumentBuilder {
	return b.Callout(IntentInfo, title, body)
}

// Code appends a syntax-highlighted or fenced code snippet.
func (b *DocumentBuilder) Code(lang, body string) *DocumentBuilder {
	b.nodes = append(b.nodes, CodeBlockNode{
		Language: lang,
		Code:     body,
	})

	return b
}

// Quote appends an indented blockquote.
func (b *DocumentBuilder) Quote(text string) *DocumentBuilder {
	b.nodes = append(b.nodes, QuoteNode{
		Text:       text,
		Expandable: false,
	})

	return b
}

// ExpandableQuote appends a collapsible blockquote.
func (b *DocumentBuilder) ExpandableQuote(text string) *DocumentBuilder {
	b.nodes = append(b.nodes, QuoteNode{
		Text:       text,
		Expandable: true,
	})

	return b
}

// Divider appends a visual horizontal rule or block separator.
func (b *DocumentBuilder) Divider() *DocumentBuilder {
	b.nodes = append(b.nodes, DividerNode{})

	return b
}

// Table appends a multi-column data table with default left-aligned columns.
func (b *DocumentBuilder) Table(headers []string, rows ...[]string) *DocumentBuilder {
	return b.TableWithAlign(headers, nil, rows...)
}

// TableWithAlign appends a multi-column data table with explicit per-column alignments.
func (b *DocumentBuilder) TableWithAlign(headers []string, aligns []Align, rows ...[]string) *DocumentBuilder {
	headerCopy := append([]string(nil), headers...)
	rowsCopy := make([][]string, len(rows))

	for i, r := range rows {
		rowsCopy[i] = append([]string(nil), r...)
	}

	var alignsCopy []Align
	if len(aligns) > 0 {
		alignsCopy = append([]Align(nil), aligns...)
	}

	b.nodes = append(b.nodes, TableNode{
		Headers: headerCopy,
		Aligns:  alignsCopy,
		Rows:    rowsCopy,
	})

	return b
}

// Raw appends unescaped pre-formatted content verbatim.
func (b *DocumentBuilder) Raw(content string) *DocumentBuilder {
	b.nodes = append(b.nodes, RawNode{Content: content})

	return b
}

// Build snapshots the accumulated nodes into an immutable [Document].
func (b *DocumentBuilder) Build() *Document {
	copied := make([]Node, len(b.nodes))
	copy(copied, b.nodes)

	return &Document{Nodes: copied}
}

// Render compiles the document into the target format using the supplied [Renderer].
func (b *DocumentBuilder) Render(r Renderer) (string, error) {
	doc := b.Build()

	var buf bytes.Buffer
	if err := r.Render(&buf, doc); err != nil {
		return "", fmt.Errorf("rendering document: %w", err)
	}

	return buf.String(), nil
}

// RenderTo streams the compiled document directly into the provided [io.Writer].
func (b *DocumentBuilder) RenderTo(w io.Writer, r Renderer) error {
	doc := b.Build()
	if err := r.Render(w, doc); err != nil {
		return fmt.Errorf("rendering document: %w", err)
	}

	return nil
}

// ToMarkdown renders the document in GitHub-compatible CommonMark markdown.
func (b *DocumentBuilder) ToMarkdown() string {
	str, _ := b.Render(DefaultMarkdownRenderer)

	return str
}

// ToTerminal renders the document in ANSI-styled terminal text.
func (b *DocumentBuilder) ToTerminal() string {
	str, _ := b.Render(DefaultTerminalRenderer)

	return str
}

// ToPlain renders the document in unadorned plaintext suitable for logs.
func (b *DocumentBuilder) ToPlain() string {
	str, _ := b.Render(DefaultPlainRenderer)

	return str
}
