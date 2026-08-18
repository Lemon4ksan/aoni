// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tui_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/cmd/vortex/internal/tui"
)

func TestTUI_VisibleWidthAndStripANSI(t *testing.T) {
	styled := tui.Cyan("Hello") + " " + tui.Bold(tui.Green("World!"))
	assert.Equal(t, "Hello World!", tui.StripANSI(styled))
	assert.Equal(t, 12, tui.VisibleWidth(styled))

	// Multi-byte Unicode glyphs
	unicodeStr := "⚡ Microsecond ✔ PASS"
	assert.Equal(t, 20, tui.VisibleWidth(unicodeStr))
}

func TestTUI_Padding(t *testing.T) {
	s := "test"
	assert.Equal(t, "test      ", tui.PadRight(s, 10))
	assert.Equal(t, "      test", tui.PadLeft(s, 10))
	assert.Equal(t, "   test   ", tui.PadCenter(s, 10))

	// Padding with ANSI strings
	colored := tui.Red("err")
	assert.Equal(t, 10, tui.VisibleWidth(tui.PadRight(colored, 10)))
}

func TestTUI_Table(t *testing.T) {
	tbl := tui.NewTable("SERVICE", "METHOD", "THROUGHPUT", "STATUS")
	tbl.SetAlignment(2, tui.AlignRight)
	tbl.SetAlignment(3, tui.AlignCenter)
	tbl.SetMinWidth(0, 15)
	tbl.SetMinWidth(1, 20)

	tbl.AddRow("Mannco", "GetUserBuyOrders", "8.35B ops/s", tui.BadgePass())
	tbl.AddRow("Pricedb_Spell", "GetSpellByName", "8.19B ops/s", tui.BadgePass())

	out := tbl.String()
	require.NotEmpty(t, out)

	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 4) // Header, Divider, 2 Rows

	// Check that lines have content and clean structure
	assert.Contains(t, lines[0], "SERVICE")
	assert.Contains(t, lines[0], "METHOD")
	assert.Contains(t, lines[1], "─")
	assert.Contains(t, lines[2], "Mannco")
	assert.Contains(t, lines[3], "Pricedb_Spell")
}

func TestTUI_Box_PixelPerfectBorders(t *testing.T) {
	box := tui.NewBox("Latency Tax", 60)
	box.AddLine("Stage 1: Encode")
	box.AddDivider()
	box.AddRow("Remote Transit", "12.40 ms")
	box.AddLine("Stage 2: Decode")

	out := strings.TrimRight(box.String(), "\n")
	require.NotEmpty(t, out)

	lines := strings.Split(out, "\n")
	require.Len(t, lines, 6) // Top, Line, Divider, Row, Line, Bottom

	// In monospaced terminal, every line of the box MUST have the exact same visible width!
	for i, l := range lines {
		w := tui.VisibleWidth(l)
		assert.Equal(t, 64, w, "Line %d (%q) visual width %d != 64", i, l, w)
	}
}

func TestTUI_BarAndTaxDecomposition(t *testing.T) {
	bar0 := tui.RenderBar(0.0, 20)
	assert.Equal(t, 20, tui.VisibleWidth(bar0))

	bar50 := tui.RenderBar(0.5, 20)
	assert.Equal(t, 20, tui.VisibleWidth(bar50))

	bar100 := tui.RenderBar(1.0, 20)
	assert.Equal(t, 20, tui.VisibleWidth(bar100))

	stages := []tui.TaxStage{
		{Name: "Client Encode", Duration: "0.12 ns", Share: "< 0.001%", Ratio: 0.0001},
		{Name: "Wire Transit", Duration: "12.40 ms", Share: "27.500%", Ratio: 0.275},
		{Name: "Remote Server", Duration: "32.60 ms", Share: "72.499%", Ratio: 0.725},
		{Name: "Client Decode", Duration: "0.13 ns", Share: "< 0.001%", Ratio: 0.0001},
	}

	card := strings.TrimRight(tui.RenderTaxDecomposition(stages, 30), "\n")
	require.NotEmpty(t, card)

	cardLines := strings.Split(card, "\n")
	firstW := tui.VisibleWidth(cardLines[0])
	assert.Equal(t, 79, firstW)

	for idx, cl := range cardLines {
		w := tui.VisibleWidth(cl)
		assert.Equal(t, firstW, w, "Tax line %d visual width %d != %d", idx, w, firstW)
	}
}

func TestTUI_Sparkline(t *testing.T) {
	spark := tui.RenderSparkline([]float64{10, 20, 50, 80, 100, 40, 20})
	assert.Equal(t, 7, tui.VisibleWidth(spark))
}

func TestTUI_StepAndBadges(t *testing.T) {
	step := tui.RenderStep(1, 3, "Pre-flight Contract Audit", tui.BadgePassed(), 44)
	assert.Contains(t, step, "[1/3] Pre-flight Contract Audit")
	assert.Contains(t, step, "✔ PASSED")

	var buf bytes.Buffer
	buf.WriteString(tui.BadgePass() + "\n")
	buf.WriteString(tui.BadgeWarn() + "\n")
	buf.WriteString(tui.BadgeFail() + "\n")
	buf.WriteString(tui.BadgeInfo() + "\n")
	buf.WriteString(tui.BadgeActive() + "\n")

	assert.Contains(t, buf.String(), "PASS")
	assert.Contains(t, buf.String(), "WARN")
	assert.Contains(t, buf.String(), "FAIL")
}
