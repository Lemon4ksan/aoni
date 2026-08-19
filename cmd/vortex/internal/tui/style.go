// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package tui provides a zero-dependency, pixel-perfect terminal UI presentation toolkit
// for CLI tools, performance profilers, and multi-repo workspace orchestrators.
package tui

import (
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"unicode/utf8"
)

var (
	ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	noColor   int32
)

func init() {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		atomic.StoreInt32(&noColor, 1)
	}
}

// SetColorEnabled toggles ANSI color formatting globally.
func SetColorEnabled(enabled bool) {
	if enabled {
		atomic.StoreInt32(&noColor, 0)
	} else {
		atomic.StoreInt32(&noColor, 1)
	}
}

// ColorEnabled reports whether ANSI color styling is active.
func ColorEnabled() bool {
	return atomic.LoadInt32(&noColor) == 0
}

// StripANSI removes all ANSI escape sequences from the string.
func StripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

// VisibleWidth returns the visual character width of a string in a monospaced terminal,
// ignoring any ANSI color codes.
func VisibleWidth(s string) int {
	clean := StripANSI(s)
	return utf8.RuneCountInString(clean)
}

// ANSI Escape Codes
const (
	ansiReset   = "\033[0m"
	ansiBold    = "\033[1m"
	ansiDim     = "\033[2m"
	ansiRed     = "\033[31m"
	ansiGreen   = "\033[32m"
	ansiYellow  = "\033[33m"
	ansiBlue    = "\033[34m"
	ansiMagenta = "\033[35m"
	ansiCyan    = "\033[36m"
	ansiGray    = "\033[90m"
	ansiWhite   = "\033[97m"
)

func style(code, text string) string {
	if !ColorEnabled() || text == "" {
		return text
	}

	return code + text + ansiReset
}

// Bold returns text wrapped in ANSI bold.
func Bold(text string) string { return style(ansiBold, text) }

// Dim returns text wrapped in ANSI dim.
func Dim(text string) string { return style(ansiDim, text) }

// Red returns text colored in Red.
func Red(text string) string { return style(ansiRed, text) }

// Green returns text colored in Green.
func Green(text string) string { return style(ansiGreen, text) }

// Yellow returns text colored in Yellow.
func Yellow(text string) string { return style(ansiYellow, text) }

// Blue returns text colored in Blue.
func Blue(text string) string { return style(ansiBlue, text) }

// Magenta returns text colored in Magenta.
func Magenta(text string) string { return style(ansiMagenta, text) }

// Cyan returns text colored in Cyan.
func Cyan(text string) string { return style(ansiCyan, text) }

// Gray returns text colored in Gray.
func Gray(text string) string { return style(ansiGray, text) }

// White returns text colored in Bright White.
func White(text string) string { return style(ansiWhite, text) }

// PadRight pads string with spaces to target visual width.
func PadRight(s string, width int) string {
	w := VisibleWidth(s)
	if w >= width {
		return s
	}

	return s + strings.Repeat(" ", width-w)
}

// PadLeft pads string on the left with spaces to target visual width.
func PadLeft(s string, width int) string {
	w := VisibleWidth(s)
	if w >= width {
		return s
	}

	return strings.Repeat(" ", width-w) + s
}

// PadCenter centers string within target visual width.
func PadCenter(s string, width int) string {
	w := VisibleWidth(s)
	if w >= width {
		return s
	}

	left := (width - w) / 2
	right := width - w - left

	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
}
