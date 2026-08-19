// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package text_test

import (
	"io"
	"testing"

	"github.com/lemon4ksan/aoni/cmd/vortex/internal/text"
)

func BenchmarkDocumentBuilder_Build(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		doc := text.NewDocument().
			Title("🚀", "Benchmark").
			Section("📦", "Details").
			Field("Key1", "Val1").
			FieldCode("Key2", "Val2").
			Paragraph("Some text").
			Bullet("Item 1", "Item 2").
			Success("OK", "Done")

		_ = doc.Build()
		doc.Release()
	}
}

func BenchmarkDocumentBuilder_RenderMarkdown(b *testing.B) {
	doc := text.NewDocument().
		Title("🚀", "Benchmark").
		Section("📦", "Details").
		Field("Key1", "Val1").
		FieldCode("Key2", "Val2").
		Paragraph("Some text").
		Bullet("Item 1", "Item 2").
		Success("OK", "Done")

	d := doc.Build()
	renderer := text.DefaultMarkdownRenderer

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = renderer.Render(io.Discard, d)
	}
}

func BenchmarkShield_ProtectAndRestore(b *testing.B) {
	shield := text.NewStandardShield()
	input := "Prefix ```go\nvar x = 10\n``` middle `code` suffix"

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		masked, restore := shield.Protect(input)
		_ = restore(masked)
	}
}
