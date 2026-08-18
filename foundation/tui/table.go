// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tui

import (
	"bytes"
	"io"
	"strings"
)

// Alignment defines the text alignment for a table column.
type Alignment int

const (
	AlignLeft Alignment = iota
	AlignRight
	AlignCenter
)

// Table manages columnar data formatting with guaranteed pixel-perfect alignment.
type Table struct {
	Headers    []string
	Alignments []Alignment
	MinWidths  []int
	Rows       [][]string
	Indent     int
}

// NewTable constructs a new Table with the specified header titles.
func NewTable(headers ...string) *Table {
	alignments := make([]Alignment, len(headers))
	minWidths := make([]int, len(headers))

	return &Table{
		Headers:    headers,
		Alignments: alignments,
		MinWidths:  minWidths,
		Indent:     2,
	}
}

// SetAlignment configures the alignment of a column index.
func (t *Table) SetAlignment(col int, align Alignment) *Table {
	if col >= 0 && col < len(t.Alignments) {
		t.Alignments[col] = align
	}

	return t
}

// SetMinWidth sets a minimum width constraint for a specific column.
func (t *Table) SetMinWidth(col, width int) *Table {
	if col >= 0 && col < len(t.MinWidths) {
		t.MinWidths[col] = width
	}

	return t
}

// SetIndent sets the left margin spacing.
func (t *Table) SetIndent(spaces int) *Table {
	t.Indent = spaces
	return t
}

// AddRow adds a row of cell values to the table.
func (t *Table) AddRow(cells ...string) *Table {
	t.Rows = append(t.Rows, cells)
	return t
}

// Render writes the formatted table to the destination writer.
func (t *Table) Render(w io.Writer) error {
	colCount := len(t.Headers)
	for _, row := range t.Rows {
		if len(row) > colCount {
			colCount = len(row)
		}
	}

	if colCount == 0 {
		return nil
	}

	widths := make([]int, colCount)

	for i := 0; i < colCount; i++ {
		if i < len(t.MinWidths) {
			widths[i] = t.MinWidths[i]
		}

		if i < len(t.Headers) {
			hw := VisibleWidth(t.Headers[i])
			if hw > widths[i] {
				widths[i] = hw
			}
		}
	}

	for _, row := range t.Rows {
		for i, cell := range row {
			cw := VisibleWidth(cell)
			if cw > widths[i] {
				widths[i] = cw
			}
		}
	}

	indentStr := strings.Repeat(" ", t.Indent)

	var buf bytes.Buffer

	// 1. Render Headers
	if len(t.Headers) > 0 {
		buf.WriteString(indentStr)

		for i, h := range t.Headers {
			aligned := t.formatCell(h, widths[i], t.getAlignment(i))
			buf.WriteString(Bold(aligned))

			if i < colCount-1 {
				buf.WriteString("  ")
			}
		}

		buf.WriteString("\n")

		// 2. Render Divider
		totalWidth := 0
		for i, w := range widths {
			totalWidth += w
			if i < colCount-1 {
				totalWidth += 2
			}
		}

		buf.WriteString(indentStr)
		buf.WriteString(Gray(strings.Repeat("─", totalWidth)))
		buf.WriteString("\n")
	}

	// 3. Render Rows
	for _, row := range t.Rows {
		buf.WriteString(indentStr)

		for i := 0; i < colCount; i++ {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}

			aligned := t.formatCell(cell, widths[i], t.getAlignment(i))
			buf.WriteString(aligned)

			if i < colCount-1 {
				buf.WriteString("  ")
			}
		}

		buf.WriteString("\n")
	}

	_, err := w.Write(buf.Bytes())

	return err
}

// String returns the rendered table as a formatted string.
func (t *Table) String() string {
	var buf bytes.Buffer

	_ = t.Render(&buf)

	return buf.String()
}

func (t *Table) getAlignment(col int) Alignment {
	if col < len(t.Alignments) {
		return t.Alignments[col]
	}

	return AlignLeft
}

func (t *Table) formatCell(s string, width int, align Alignment) string {
	switch align {
	case AlignRight:
		return PadLeft(s, width)
	case AlignCenter:
		return PadCenter(s, width)
	default:
		return PadRight(s, width)
	}
}
