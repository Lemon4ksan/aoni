// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tui

// Badge formats a status badge with customized coloring.
func Badge(text string, colorFn func(string) string) string {
	if colorFn == nil {
		return text
	}

	return colorFn(text)
}

// BadgePass returns a green success badge (✔ PASS).
func BadgePass() string {
	return Green("✔ PASS")
}

// BadgePassed returns a green success badge (✔ PASSED).
func BadgePassed() string {
	return Green("✔ PASSED")
}

// BadgeWarn returns a yellow warning badge (⚠️ WARN).
func BadgeWarn() string {
	return Yellow("⚠️ WARN")
}

// BadgeFail returns a red failure badge (❌ FAIL).
func BadgeFail() string {
	return Red("❌ FAIL")
}

// BadgeInfo returns a cyan info badge (ℹ INFO).
func BadgeInfo() string {
	return Cyan("ℹ INFO")
}

// BadgeActive returns a magenta active badge (⚡ ACTIVE).
func BadgeActive() string {
	return Magenta("⚡ ACTIVE")
}

// BadgeSync returns a green synchronized badge (✔ Synchronized).
func BadgeSync() string {
	return Green("✔ Synchronized")
}
