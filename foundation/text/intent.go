// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package text

// Intent represents the semantic tone or severity of a document block or callout.
type Intent uint8

const (
	// IntentNeutral represents neutral, un-styled informational content.
	IntentNeutral Intent = iota
	// IntentInfo represents standard informational notes or hints.
	IntentInfo
	// IntentSuccess represents successful operations, confirmations, or positive milestones.
	IntentSuccess
	// IntentWarning represents warnings, rate limits, or non-fatal cautions.
	IntentWarning
	// IntentDanger represents fatal errors, security alerts, or destructive operations.
	IntentDanger
	// IntentMuted represents dimmed, secondary, or auxiliary metadata.
	IntentMuted
)

// String returns a human-readable name for the intent.
func (i Intent) String() string {
	switch i {
	case IntentNeutral:
		return "neutral"
	case IntentInfo:
		return "info"
	case IntentSuccess:
		return "success"
	case IntentWarning:
		return "warning"
	case IntentDanger:
		return "danger"
	case IntentMuted:
		return "muted"
	default:
		return "unknown"
	}
}

// Icon returns the canonical Unicode symbol or emoji corresponding to the intent.
func (i Intent) Icon() string {
	switch i {
	case IntentInfo:
		return "ℹ️"
	case IntentSuccess:
		return "✅"
	case IntentWarning:
		return "⚠️"
	case IntentDanger:
		return "❌"
	case IntentMuted:
		return "•"
	default:
		return ""
	}
}
