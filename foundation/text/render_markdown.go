// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package text

import (
	"fmt"
	"io"
	"strings"
)

// MarkdownRenderer converts a [Document] into GitHub-compatible CommonMark markdown.
type MarkdownRenderer struct {
	// CalloutStyle determines how callouts are rendered ("github" for [!NOTE], "bold" for **Title**).
	CalloutStyle string
}

// NewMarkdownRenderer constructs a fresh [MarkdownRenderer] with GitHub style callouts.
func NewMarkdownRenderer() *MarkdownRenderer {
	return &MarkdownRenderer{CalloutStyle: "github"}
}

// Render writes the Markdown formatted document to w.
func (r *MarkdownRenderer) Render(w io.Writer, doc *Document) error {
	if doc == nil || len(doc.Nodes) == 0 {
		return nil
	}

	for i, node := range doc.Nodes {
		switch n := node.(type) {
		case HeadingNode:
			prefix := strings.Repeat("#", n.Level)
			if n.Icon != "" {
				fmt.Fprintf(w, "%s %s %s\n\n", prefix, n.Icon, n.Text)
			} else {
				fmt.Fprintf(w, "%s %s\n\n", prefix, n.Text)
			}

		case SectionNode:
			if n.Icon != "" {
				fmt.Fprintf(w, "**%s %s:**\n", n.Icon, n.Name)
			} else {
				fmt.Fprintf(w, "**%s:**\n", n.Name)
			}

		case ParagraphNode:
			fmt.Fprintf(w, "%s\n\n", n.Text)

		case FieldNode:
			switch n.Style {
			case FieldCode:
				fmt.Fprintf(w, "• **%s**: `%s`\n", n.Key, n.Value)
			case FieldBold:
				fmt.Fprintf(w, "• **%s**: **%s**\n", n.Key, n.Value)
			default:
				fmt.Fprintf(w, "• **%s**: %s\n", n.Key, n.Value)
			}

		case ListNode:
			for idx, item := range n.Items {
				if n.Kind == ListNumbered {
					fmt.Fprintf(w, "%d. %s\n", idx+1, item)
				} else {
					fmt.Fprintf(w, "• %s\n", item)
				}
			}

			fmt.Fprintln(w)

		case CalloutNode:
			r.renderCallout(w, n)

		case CodeBlockNode:
			fmt.Fprintf(w, "```%s\n%s\n```\n\n", n.Language, n.Code)

		case QuoteNode:
			lines := strings.Split(n.Text, "\n")
			for _, line := range lines {
				fmt.Fprintf(w, "> %s\n", line)
			}

			fmt.Fprintln(w)

		case DividerNode:
			fmt.Fprintf(w, "---\n\n")

		case TableNode:
			r.renderTable(w, n)

		case RawNode:
			fmt.Fprint(w, n.Content)
		}

		// Ensure spacing after fields if next node is a major block
		if _, isField := node.(FieldNode); isField && i+1 < len(doc.Nodes) {
			if _, nextIsField := doc.Nodes[i+1].(FieldNode); !nextIsField {
				fmt.Fprintln(w)
			}
		}
	}

	return nil
}

func (r *MarkdownRenderer) renderCallout(w io.Writer, n CalloutNode) {
	tag := "NOTE"

	switch n.Intent {
	case IntentInfo:
		tag = "NOTE"
	case IntentSuccess:
		tag = "TIP"
	case IntentWarning:
		tag = "WARNING"
	case IntentDanger:
		tag = "CAUTION"
	case IntentMuted:
		tag = "NOTE"
	}

	if r.CalloutStyle == "github" {
		fmt.Fprintf(w, "> [!%s]\n", tag)

		if n.Title != "" {
			fmt.Fprintf(w, "> **%s**\n", n.Title)
		}

		if n.Body != "" {
			for _, line := range strings.Split(n.Body, "\n") {
				fmt.Fprintf(w, "> %s\n", line)
			}
		}

		fmt.Fprintln(w)
	} else {
		icon := n.Intent.Icon()
		if icon != "" {
			fmt.Fprintf(w, "%s **%s**\n", icon, n.Title)
		} else {
			fmt.Fprintf(w, "**%s**\n", n.Title)
		}

		if n.Body != "" {
			fmt.Fprintf(w, "%s\n", n.Body)
		}

		fmt.Fprintln(w)
	}
}

func (r *MarkdownRenderer) renderTable(w io.Writer, t TableNode) {
	if len(t.Headers) == 0 {
		return
	}

	// Header row
	fmt.Fprintf(w, "| %s |\n", strings.Join(t.Headers, " | "))

	// Separator row
	separators := make([]string, len(t.Headers))
	for i := range t.Headers {
		var align Align
		if i < len(t.Aligns) {
			align = t.Aligns[i]
		}

		switch align {
		case AlignCenter:
			separators[i] = ":---:"
		case AlignRight:
			separators[i] = "---:"
		default:
			separators[i] = ":---"
		}
	}

	fmt.Fprintf(w, "| %s |\n", strings.Join(separators, " | "))

	// Data rows
	for _, row := range t.Rows {
		cells := make([]string, len(t.Headers))
		for i := range t.Headers {
			if i < len(row) {
				cells[i] = row[i]
			} else {
				cells[i] = ""
			}
		}

		fmt.Fprintf(w, "| %s |\n", strings.Join(cells, " | "))
	}

	fmt.Fprintln(w)
}
