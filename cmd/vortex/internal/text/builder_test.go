// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package text_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/cmd/vortex/internal/text"
)

func TestDocumentBuilder_AllNodes(t *testing.T) {
	doc := text.NewDocument().
		Title("🚀", "Release Notes").
		Section("📦", "Artifacts").
		Field("Version", "v2.5.0").
		FieldCode("Commit", "a1b2c3d").
		FieldBold("Status", "STABLE").
		Paragraph("This is a major release with high performance enhancements.").
		Bullet("Zero allocation networking", "Pure-Go JA4 fingerprinting").
		Numbered("Download binary", "Run vortex init").
		Success("Build Passed", "All 50 unit tests executed cleanly").
		Warning("Deprecated API", "Method FetchOld will be removed in v3").
		Danger("Security Vulnerability", "Upgrade immediately").
		Info("Notice", "Maintenance scheduled for Sunday").
		Code("go", "client := aoni.NewClient(nil)").
		Quote("Fast silicon speed.\n0 allocations.").
		ExpandableQuote("Internal stack trace details").
		Divider().
		Table(
			[]string{"Package", "Coverage", "Status"},
			[]string{"fast", "94.2%", "PASS"},
			[]string{"codec", "98.1%", "PASS"},
		).
		Raw("VERBATIM_RAW_TEXT")

	defer doc.Release()

	// 1. Markdown Rendering
	md := doc.ToMarkdown()
	require.Contains(t, md, "# 🚀 Release Notes")
	require.Contains(t, md, "**📦 Artifacts:**")
	require.Contains(t, md, "• **Version**: v2.5.0")
	require.Contains(t, md, "• **Commit**: `a1b2c3d`")
	require.Contains(t, md, "• **Status**: **STABLE**")
	require.Contains(t, md, "• Zero allocation networking")
	require.Contains(t, md, "1. Download binary")
	require.Contains(t, md, "> [!TIP]")
	require.Contains(t, md, "> [!WARNING]")
	require.Contains(t, md, "> [!CAUTION]")
	require.Contains(t, md, "> [!NOTE]")
	require.Contains(t, md, "```go\nclient := aoni.NewClient(nil)\n```")
	require.Contains(t, md, "> Fast silicon speed.")
	require.Contains(t, md, "---")
	require.Contains(t, md, "| Package | Coverage | Status |")
	require.Contains(t, md, "VERBATIM_RAW_TEXT")

	// 2. Plain Text Rendering
	plain := doc.ToPlain()
	require.Contains(t, plain, "🚀 Release Notes")
	require.Contains(t, plain, "📦 Artifacts:")
	require.Contains(t, plain, "Version: v2.5.0")
	require.Contains(t, plain, "[SUCCESS]")
	require.Contains(t, plain, "[WARNING]")
	require.Contains(t, plain, "[DANGER]")
	require.Contains(t, plain, "Package")

	// 3. Terminal ANSI Rendering
	term := doc.ToTerminal()
	require.Contains(t, term, "Release Notes")
	require.Contains(t, term, "Artifacts")
	require.Contains(t, term, "┌── go")
}

func TestDocumentBuilder_PoolReuse(t *testing.T) {
	b1 := text.NewDocument()
	b1.Paragraph("Hello World")
	require.Equal(t, 1, b1.Build().Len())
	b1.Release()

	b2 := text.NewDocument()
	require.Equal(t, 0, b2.Build().Len())
	b2.Title("", "New Title")
	require.Equal(t, 1, b2.Build().Len())
	b2.Release()
}

func TestDocumentBuilder_TableAlignment(t *testing.T) {
	doc := text.NewDocument().
		TableWithAlign(
			[]string{"ID", "Description", "Price"},
			[]text.Align{text.AlignLeft, text.AlignCenter, text.AlignRight},
			[]string{"1", "Apple", "$1.00"},
			[]string{"2", "Banana", "$0.50"},
		)

	md := doc.ToMarkdown()
	require.Contains(t, md, "| :--- | :---: | ---: |")
}

func TestDocumentBuilder_CustomRenderer(t *testing.T) {
	doc := text.NewDocument().
		Title("🔥", "Hot Topic").
		Paragraph("Body text here.")

	var customBuf bytes.Buffer

	err := doc.RenderTo(&customBuf, text.DefaultPlainRenderer)
	require.NoError(t, err)
	require.True(t, strings.Contains(customBuf.String(), "Hot Topic"))
}
