// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tui

import (
	"bytes"
	"io"
	"strings"
)

// BorderStyle defines the glyph set for box outlines.
type BorderStyle struct {
	TopLeft      string
	TopRight     string
	BottomLeft   string
	BottomRight  string
	Horizontal   string
	Vertical     string
	DividerLeft  string
	DividerRight string
}

var (
	// BorderSingle is the standard crisp rectangular outline (┌─┐│└─┘).
	BorderSingle = BorderStyle{
		TopLeft:      "┌",
		TopRight:     "┐",
		BottomLeft:   "└",
		BottomRight:  "┘",
		Horizontal:   "─",
		Vertical:     "│",
		DividerLeft:  "├",
		DividerRight: "┤",
	}

	// BorderRounded uses smooth rounded corners (╭─╮│╰─╯).
	BorderRounded = BorderStyle{
		TopLeft:      "╭",
		TopRight:     "╮",
		BottomLeft:   "╰",
		BottomRight:  "╯",
		Horizontal:   "─",
		Vertical:     "│",
		DividerLeft:  "├",
		DividerRight: "┤",
	}

	// BorderHeavy uses bold heavy lines (┏━┓┃┗━┛).
	BorderHeavy = BorderStyle{
		TopLeft:      "┏",
		TopRight:     "┓",
		BottomLeft:   "┗",
		BottomRight:  "┛",
		Horizontal:   "━",
		Vertical:     "┃",
		DividerLeft:  "┣",
		DividerRight: "┫",
	}
)

type boxEntryType int

const (
	entryLine boxEntryType = iota
	entryDivider
	entryRow
)

type boxEntry struct {
	entryType boxEntryType
	left      string
	right     string
}

// Box formats enclosed text cards and panels with guaranteed straight right borders.
type Box struct {
	Width       int
	Title       string
	Style       BorderStyle
	Indent      int
	entries     []boxEntry
	AutoPadding bool
}

// NewBox creates a new rectangular box with the given title and inner width constraint.
func NewBox(title string, innerWidth int) *Box {
	return &Box{
		Width:       innerWidth,
		Title:       title,
		Style:       BorderSingle,
		Indent:      2,
		AutoPadding: true,
	}
}

// SetStyle configures the border style (e.g. BorderRounded, BorderHeavy).
func (b *Box) SetStyle(s BorderStyle) *Box {
	b.Style = s
	return b
}

// SetIndent configures the left margin indentation.
func (b *Box) SetIndent(spaces int) *Box {
	b.Indent = spaces
	return b
}

// AddLine adds a full-width line of text inside the box.
func (b *Box) AddLine(text string) *Box {
	b.entries = append(b.entries, boxEntry{entryType: entryLine, left: text})
	return b
}

// AddDivider adds an internal horizontal dividing rule (├───┤).
func (b *Box) AddDivider() *Box {
	b.entries = append(b.entries, boxEntry{entryType: entryDivider})
	return b
}

// AddRow adds a two-column row with left and right aligned content.
func (b *Box) AddRow(left, right string) *Box {
	b.entries = append(b.entries, boxEntry{entryType: entryRow, left: left, right: right})
	return b
}

// Render writes the formatted box card to the writer.
func (b *Box) Render(w io.Writer) error {
	innerW := b.Width

	// Auto-compute width if not specified
	if innerW <= 0 {
		maxLen := VisibleWidth(b.Title)
		for _, e := range b.entries {
			var w int
			switch e.entryType {
			case entryRow:
				w = VisibleWidth(e.left) + VisibleWidth(e.right) + 2
			case entryLine:
				w = VisibleWidth(e.left)
			}

			if w > maxLen {
				maxLen = w
			}
		}

		innerW = maxLen + 2
	}

	indentStr := strings.Repeat(" ", b.Indent)

	var buf bytes.Buffer

	// 1. Top border
	buf.WriteString(indentStr)
	buf.WriteString(b.Style.TopLeft)

	if b.Title != "" {
		titleVis := VisibleWidth(b.Title)
		if titleVis+3 <= innerW {
			buf.WriteString(b.Style.Horizontal)
			buf.WriteString(" ")
			buf.WriteString(Bold(b.Title))
			buf.WriteString(" ")

			rem := innerW - (titleVis + 3)
			if rem > 0 {
				buf.WriteString(strings.Repeat(b.Style.Horizontal, rem))
			}
		} else {
			buf.WriteString(strings.Repeat(b.Style.Horizontal, innerW))
		}
	} else {
		buf.WriteString(strings.Repeat(b.Style.Horizontal, innerW))
	}

	buf.WriteString(b.Style.TopRight)
	buf.WriteString("\n")

	// 2. Entries
	for _, e := range b.entries {
		switch e.entryType {
		case entryDivider:
			buf.WriteString(indentStr)
			buf.WriteString(b.Style.DividerLeft)
			buf.WriteString(strings.Repeat(b.Style.Horizontal, innerW))
			buf.WriteString(b.Style.DividerRight)
			buf.WriteString("\n")

		case entryRow:
			buf.WriteString(indentStr)
			buf.WriteString(b.Style.Vertical)
			buf.WriteString(" ")

			leftVis := VisibleWidth(e.left)
			rightVis := VisibleWidth(e.right)
			avail := innerW - 2

			gap := avail - (leftVis + rightVis)
			if gap < 1 {
				gap = 1
			}

			buf.WriteString(e.left)
			buf.WriteString(strings.Repeat(" ", gap))
			buf.WriteString(e.right)
			buf.WriteString(" ")
			buf.WriteString(b.Style.Vertical)
			buf.WriteString("\n")

		case entryLine:
			buf.WriteString(indentStr)
			buf.WriteString(b.Style.Vertical)
			buf.WriteString(" ")

			lineVis := VisibleWidth(e.left)
			avail := innerW - 2

			buf.WriteString(e.left)

			if lineVis < avail {
				buf.WriteString(strings.Repeat(" ", avail-lineVis))
			}

			buf.WriteString(" ")
			buf.WriteString(b.Style.Vertical)
			buf.WriteString("\n")
		}
	}

	// 3. Bottom border
	buf.WriteString(indentStr)
	buf.WriteString(b.Style.BottomLeft)
	buf.WriteString(strings.Repeat(b.Style.Horizontal, innerW))
	buf.WriteString(b.Style.BottomRight)
	buf.WriteString("\n")

	_, err := w.Write(buf.Bytes())

	return err
}

// String returns the box as a string.
func (b *Box) String() string {
	var buf bytes.Buffer

	_ = b.Render(&buf)

	return buf.String()
}
