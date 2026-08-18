// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tui

import (
	"fmt"
	"math"
	"strings"
)

// RenderBar generates a proportional ASCII/Unicode filled progress bar (e.g. ████████░░░░░░░░).
func RenderBar(ratio float64, width int) string {
	if width <= 0 {
		return ""
	}

	if ratio < 0 {
		ratio = 0
	} else if ratio > 1.0 {
		ratio = 1.0
	}

	filled := int(math.Round(ratio * float64(width)))
	if filled > width {
		filled = width
	}

	empty := width - filled

	return Cyan(strings.Repeat("█", filled)) + Gray(strings.Repeat("░", empty))
}

var sparklineGlyphs = []rune{' ', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// RenderSparkline generates an inline visual trend graph from a sequence of floating-point values.
func RenderSparkline(values []float64) string {
	if len(values) == 0 {
		return ""
	}

	minVal := values[0]
	maxVal := values[0]

	for _, v := range values {
		if v < minVal {
			minVal = v
		}

		if v > maxVal {
			maxVal = v
		}
	}

	span := maxVal - minVal

	var sb strings.Builder

	for _, v := range values {
		if span == 0 {
			sb.WriteRune(sparklineGlyphs[len(sparklineGlyphs)/2])
			continue
		}

		idx := int((v - minVal) / span * float64(len(sparklineGlyphs)-1))
		if idx < 0 {
			idx = 0
		} else if idx >= len(sparklineGlyphs) {
			idx = len(sparklineGlyphs) - 1
		}

		sb.WriteRune(sparklineGlyphs[idx])
	}

	return sb.String()
}

// TaxStage represents a single phase in network roundtrip decomposition.
type TaxStage struct {
	Name     string
	Duration string
	Share    string
	Ratio    float64
}

// RenderTaxDecomposition formats an elegant Latency Tax Decomposition card.
func RenderTaxDecomposition(stages []TaxStage, barWidth int) string {
	if barWidth <= 0 {
		barWidth = 30
	}

	box := NewBox("", 75)
	box.AddLine(fmt.Sprintf("%-16s %-14s %-10s %s", "Stage", "Duration", "Share", "Proportional Breakdown"))
	box.AddDivider()

	for _, st := range stages {
		bar := RenderBar(st.Ratio, barWidth)
		line := fmt.Sprintf("%-16s %-14s %-10s %s", st.Name, st.Duration, st.Share, bar)
		box.AddLine(line)
	}

	return box.String()
}
