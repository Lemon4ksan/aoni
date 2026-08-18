// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package text

import (
	"fmt"
	"io"
	"strings"
)

// PlainRenderer converts a [Document] into unadorned plaintext.
type PlainRenderer struct{}

// NewPlainRenderer constructs a fresh [PlainRenderer].
func NewPlainRenderer() *PlainRenderer {
	return &PlainRenderer{}
}

// Render writes the plaintext representation of doc to w.
func (r *PlainRenderer) Render(w io.Writer, doc *Document) error {
	if doc == nil || len(doc.Nodes) == 0 {
		return nil
	}

	for i, node := range doc.Nodes {
		switch n := node.(type) {
		case HeadingNode:
			if n.Icon != "" {
				fmt.Fprintf(w, "%s %s\n\n", n.Icon, n.Text)
			} else {
				fmt.Fprintf(w, "%s\n\n", n.Text)
			}

		case SectionNode:
			if n.Icon != "" {
				fmt.Fprintf(w, "%s %s:\n", n.Icon, n.Name)
			} else {
				fmt.Fprintf(w, "%s:\n", n.Name)
			}

		case ParagraphNode:
			fmt.Fprintf(w, "%s\n\n", n.Text)

		case FieldNode:
			fmt.Fprintf(w, "  %s: %s\n", n.Key, n.Value)

		case ListNode:
			for idx, item := range n.Items {
				if n.Kind == ListNumbered {
					fmt.Fprintf(w, "  %d. %s\n", idx+1, item)
				} else {
					fmt.Fprintf(w, "  • %s\n", item)
				}
			}

			fmt.Fprintln(w)

		case CalloutNode:
			icon := n.Intent.Icon()
			if icon != "" {
				fmt.Fprintf(w, "[%s] %s %s\n", strings.ToUpper(n.Intent.String()), icon, n.Title)
			} else {
				fmt.Fprintf(w, "[%s] %s\n", strings.ToUpper(n.Intent.String()), n.Title)
			}

			if n.Body != "" {
				fmt.Fprintf(w, "  %s\n", n.Body)
			}

			fmt.Fprintln(w)

		case CodeBlockNode:
			fmt.Fprintf(w, "---\n%s\n---\n\n", n.Code)

		case QuoteNode:
			lines := strings.Split(n.Text, "\n")
			for _, line := range lines {
				fmt.Fprintf(w, "  | %s\n", line)
			}

			fmt.Fprintln(w)

		case DividerNode:
			fmt.Fprintf(w, "----------------------------------------\n\n")

		case TableNode:
			r.renderTable(w, n)

		case RawNode:
			fmt.Fprint(w, n.Content)
		}

		if _, isField := node.(FieldNode); isField && i+1 < len(doc.Nodes) {
			if _, nextIsField := doc.Nodes[i+1].(FieldNode); !nextIsField {
				fmt.Fprintln(w)
			}
		}
	}

	return nil
}

func (r *PlainRenderer) renderTable(w io.Writer, t TableNode) {
	if len(t.Headers) == 0 {
		return
	}

	colWidths := make([]int, len(t.Headers))
	for i, h := range t.Headers {
		colWidths[i] = len(h)
	}

	for _, row := range t.Rows {
		for i, cell := range row {
			if i < len(colWidths) && len(cell) > colWidths[i] {
				colWidths[i] = len(cell)
			}
		}
	}

	// Print headers
	for i, h := range t.Headers {
		fmt.Fprintf(w, "%-*s  ", colWidths[i], h)
	}

	fmt.Fprintln(w)

	// Print rows
	for _, row := range t.Rows {
		for i := range t.Headers {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}

			fmt.Fprintf(w, "%-*s  ", colWidths[i], cell)
		}

		fmt.Fprintln(w)
	}

	fmt.Fprintln(w)
}
