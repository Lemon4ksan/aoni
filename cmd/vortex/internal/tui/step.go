// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tui

import (
	"fmt"
	"strings"
)

// RenderStep formats a numbered pipeline stage with dotted connector dots.
// Example: "[1/3] Pre-flight Contract Audit ........... ✔ PASSED (100% clean)"
func RenderStep(current, total int, label, status string, totalLabelWidth int) string {
	prefix := fmt.Sprintf("[%d/%d] %s ", current, total, label)
	vis := VisibleWidth(prefix)

	if totalLabelWidth <= 0 {
		totalLabelWidth = 44
	}

	dots := max(totalLabelWidth-vis, 3)

	return prefix + Gray(strings.Repeat(".", dots)) + " " + status
}

// RenderDivider returns a horizontal divider line of the given width.
func RenderDivider(width int) string {
	if width <= 0 {
		width = 77
	}

	return Gray(strings.Repeat("─", width))
}

// RenderHeader formats an emphasized section header.
func RenderHeader(title string) string {
	return Bold(title)
}
