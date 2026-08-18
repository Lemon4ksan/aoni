// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package text

import (
	"fmt"
	"io"

	"github.com/lemon4ksan/foundation/silicon/bytesconv"
)

// TerminalRenderer converts a [Document] into ANSI colorized terminal output.
type TerminalRenderer struct {
	ColorEnabled bool
}

// NewTerminalRenderer constructs a fresh [TerminalRenderer] with ANSI colors enabled by default.
func NewTerminalRenderer() *TerminalRenderer {
	return &TerminalRenderer{ColorEnabled: true}
}

// ANSI Escape sequences.
const (
	ansiReset     = "\033[0m"
	ansiBold      = "\033[1m"
	ansiDim       = "\033[2m"
	ansiUnderline = "\033[4m"

	ansiRed     = "\033[31m"
	ansiGreen   = "\033[32m"
	ansiYellow  = "\033[33m"
	ansiBlue    = "\033[34m"
	ansiMagenta = "\033[35m"
	ansiCyan    = "\033[36m"
	ansiWhite   = "\033[37m"
)

func (r *TerminalRenderer) style(code, s string) string {
	if !r.ColorEnabled || code == "" {
		return s
	}

	return code + s + ansiReset
}

// Render writes the terminal-formatted representation of doc to w.
func (r *TerminalRenderer) Render(w io.Writer, doc *Document) error {
	if doc == nil || len(doc.Nodes) == 0 {
		return nil
	}

	for i, node := range doc.Nodes {
		switch n := node.(type) {
		case HeadingNode:
			title := n.Text
			if n.Icon != "" {
				title = n.Icon + " " + title
			}

			switch n.Level {
			case 1:
				fmt.Fprintf(w, "%s\n\n", r.style(ansiBold+ansiWhite+ansiUnderline, title))
			case 2:
				fmt.Fprintf(w, "%s\n\n", r.style(ansiBold+ansiCyan, title))
			default:
				fmt.Fprintf(w, "%s\n\n", r.style(ansiBold, title))
			}

		case SectionNode:
			sec := n.Name
			if n.Icon != "" {
				sec = n.Icon + " " + sec
			}

			fmt.Fprintf(w, "%s\n", r.style(ansiBold+ansiYellow, sec+":"))

		case ParagraphNode:
			fmt.Fprintf(w, "%s\n\n", n.Text)

		case FieldNode:
			key := r.style(ansiBold, n.Key)
			val := n.Value

			switch n.Style {
			case FieldCode:
				val = r.style(ansiCyan, "`"+n.Value+"`")
			case FieldBold:
				val = r.style(ansiBold, n.Value)
			}

			fmt.Fprintf(w, "  • %s: %s\n", key, val)

		case ListNode:
			for idx, item := range n.Items {
				if n.Kind == ListNumbered {
					fmt.Fprintf(w, "  %s %s\n", r.style(ansiDim, fmt.Sprintf("%d.", idx+1)), item)
				} else {
					fmt.Fprintf(w, "  %s %s\n", r.style(ansiCyan, "•"), item)
				}
			}

			fmt.Fprintln(w)

		case CalloutNode:
			r.renderCallout(w, n)

		case CodeBlockNode:
			fmt.Fprintf(w, "  %s\n", r.style(ansiDim, "┌── "+n.Language))

			for line := range bytesconv.ScanTokens(n.Code, '\n') {
				fmt.Fprintf(w, "  %s %s\n", r.style(ansiDim, "│"), r.style(ansiCyan, line))
			}

			fmt.Fprintf(w, "  %s\n\n", r.style(ansiDim, "└──"))

		case QuoteNode:
			for line := range bytesconv.ScanTokens(n.Text, '\n') {
				fmt.Fprintf(w, "  %s %s\n", r.style(ansiDim, "▎"), r.style(ansiDim, line))
			}

			fmt.Fprintln(w)

		case DividerNode:
			fmt.Fprintf(w, "%s\n\n", r.style(ansiDim, "────────────────────────────────────────"))

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

func (r *TerminalRenderer) renderCallout(w io.Writer, n CalloutNode) {
	color := ansiBlue
	switch n.Intent {
	case IntentSuccess:
		color = ansiGreen
	case IntentWarning:
		color = ansiYellow
	case IntentDanger:
		color = ansiRed
	case IntentInfo:
		color = ansiBlue
	case IntentMuted:
		color = ansiDim
	}

	title := n.Title
	if icon := n.Intent.Icon(); icon != "" {
		title = icon + " " + title
	}

	fmt.Fprintf(w, "%s %s\n", r.style(ansiBold+color, "▍"), r.style(ansiBold+color, title))

	if n.Body != "" {
		for line := range bytesconv.ScanTokens(n.Body, '\n') {
			fmt.Fprintf(w, "%s   %s\n", r.style(color, "▍"), line)
		}
	}

	fmt.Fprintln(w)
}

func (r *TerminalRenderer) renderTable(w io.Writer, t TableNode) {
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
		fmt.Fprintf(w, "%s  ", r.style(ansiBold+ansiUnderline, fmt.Sprintf("%-*s", colWidths[i], h)))
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
