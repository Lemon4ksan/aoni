// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package text

import (
	"fmt"
	"regexp"
	"strings"
)

// Pattern defines a regex pattern or rule used to identify protected token spans.
type Pattern struct {
	Regex *regexp.Regexp
}

// Common pattern constructors.
var (
	// PatternCodeBlock matches triple-backtick fenced code blocks with optional language tags.
	PatternCodeBlock = Pattern{Regex: regexp.MustCompile("(?s)```[a-zA-Z0-9_-]*\n?.*?```")}

	// PatternInlineCode matches single-backtick inline code spans.
	PatternInlineCode = Pattern{Regex: regexp.MustCompile("`[^`]+`")}

	// PatternMarkdownLink matches standard markdown hyperlinks [text](url).
	PatternMarkdownLink = Pattern{Regex: regexp.MustCompile(`\[([^\]]+)\]\((https?://[^\s\)]+)\)`)}

	// PatternURL matches raw HTTP/HTTPS URLs.
	PatternURL = Pattern{Regex: regexp.MustCompile(`https?://[^\s<>"'` + "`" + `\)]+`)}
)

// PatternXMLTag creates a pattern matching XML-style tags and their body, e.g. <tag>...</tag>.
func PatternXMLTag(tagName string) Pattern {
	rx := fmt.Sprintf(`(?si)<%s(?:\s+[^>]*)?>.*?</%s>`, regexp.QuoteMeta(tagName), regexp.QuoteMeta(tagName))

	return Pattern{Regex: regexp.MustCompile(rx)}
}

// PatternCustom creates a pattern wrapping a caller-defined regular expression.
func PatternCustom(rx *regexp.Regexp) Pattern {
	return Pattern{Regex: rx}
}

// Shield protects sensitive or literal spans from being altered during text transformation passes.
type Shield struct {
	patterns []Pattern
}

// NewShield constructs a [Shield] configured with the specified detection patterns.
func NewShield(patterns ...Pattern) *Shield {
	return &Shield{
		patterns: append([]Pattern(nil), patterns...),
	}
}

// NewStandardShield constructs a [Shield] configured to protect code blocks and inline code spans.
func NewStandardShield() *Shield {
	return NewShield(PatternCodeBlock, PatternInlineCode)
}

// Protect extracts all matched spans from input, replacing them with binary-safe control placeholders.
// It returns the masked string along with a restore function to reinstate the protected spans after transformation.
func (s *Shield) Protect(input string) (masked string, restore func(string) string) {
	if input == "" || len(s.patterns) == 0 {
		return input, func(s string) string { return s }
	}

	var vault []string

	result := input

	for _, p := range s.patterns {
		if p.Regex == nil {
			continue
		}

		result = p.Regex.ReplaceAllStringFunc(result, func(match string) string {
			idx := len(vault)
			vault = append(vault, match)

			return fmt.Sprintf("\x00SHIELD#%d#\x00", idx)
		})
	}

	if len(vault) == 0 {
		return input, func(s string) string { return s }
	}

	restore = func(transformed string) string {
		for i, token := range vault {
			placeholder := fmt.Sprintf("\x00SHIELD#%d#\x00", i)
			transformed = strings.ReplaceAll(transformed, placeholder, token)
		}

		return transformed
	}

	return result, restore
}

// Transform protects spans, executes the provided transform callback on the masked text, and restores protected spans.
func (s *Shield) Transform(input string, transformFn func(string) string) string {
	if input == "" || transformFn == nil {
		return input
	}

	masked, restore := s.Protect(input)
	transformed := transformFn(masked)

	return restore(transformed)
}
