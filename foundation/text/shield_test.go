// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package text_test

import (
	"html"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/foundation/text"
)

func TestShield_StandardCodeBlocks(t *testing.T) {
	shield := text.NewStandardShield()

	input := "Check out this code:\n```go\nfmt.Println(\"x < 10 & y > 20\")\n```\nAnd inline `a < b` condition."

	// Protect
	masked, restore := shield.Protect(input)
	require.NotContains(t, masked, "fmt.Println")
	require.NotContains(t, masked, "a < b")
	require.Contains(t, masked, "\x00SHIELD#0#\x00")
	require.Contains(t, masked, "\x00SHIELD#1#\x00")

	// Apply HTML entity escaping to text outside code
	escaped := html.EscapeString(masked)

	// Restore
	output := restore(escaped)
	require.Contains(t, output, "```go\nfmt.Println(\"x < 10 & y > 20\")\n```")
	require.Contains(t, output, "`a < b`")
}

func TestShield_XMLTags(t *testing.T) {
	shield := text.NewShield(
		text.PatternXMLTag("thought"),
		text.PatternCodeBlock,
	)

	input := "<thought>\nReasoning: if a & b then return true\n</thought>\n\nFinal Answer: ```json\n{\"ok\": true}\n```"

	result := shield.Transform(input, strings.ToUpper)

	require.Contains(t, result, "<thought>\nReasoning: if a & b then return true\n</thought>")
	require.Contains(t, result, "FINAL ANSWER:")
	require.Contains(t, result, "```json\n{\"ok\": true}\n```")
}

func TestShield_EmptyAndNoMatch(t *testing.T) {
	shield := text.NewStandardShield()

	emptyOut := shield.Transform("", func(s string) string {
		return s + "_transformed"
	})
	require.Equal(t, "", emptyOut)

	noMatch := "Simple plain text with no code."
	out := shield.Transform(noMatch, strings.ToUpper)
	require.Equal(t, "SIMPLE PLAIN TEXT WITH NO CODE.", out)
}
