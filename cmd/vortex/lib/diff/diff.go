// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package diff implements semantic contract drift analysis between local Go interfaces and OpenAPI specifications.
// Core data models are located in [github.com/lemon4ksan/foundation/text/diff].
package diff

import (
	fdiff "github.com/lemon4ksan/foundation/text/diff"
)

// DriftSeverity classifies the breaking impact of a contract discrepancy.
type DriftSeverity = fdiff.DriftSeverity

const (
	SeverityBreaking    = fdiff.SeverityBreaking
	SeverityNonBreaking = fdiff.SeverityNonBreaking
	SeverityGhost       = fdiff.SeverityGhost
)

// DriftKind identifies the specific nature of a drift item.
type DriftKind = fdiff.DriftKind

const (
	DriftMissingEndpoint     = fdiff.DriftMissingEndpoint
	DriftHTTPMethodMismatch  = fdiff.DriftHTTPMethodMismatch
	DriftPathMismatch        = fdiff.DriftPathMismatch
	DriftMissingParam        = fdiff.DriftMissingParam
	DriftExtraParam          = fdiff.DriftExtraParam
	DriftTypeMismatch        = fdiff.DriftTypeMismatch
	DriftResponseMismatch    = fdiff.DriftResponseMismatch
	DriftDeprecationMismatch = fdiff.DriftDeprecationMismatch
)

// DiffOptions configures semantic contract comparison behavior.
type DiffOptions = fdiff.DiffOptions

// DriftItem represents a single detected discrepancy between contracts.
type DriftItem = fdiff.DriftItem

// DiffReport aggregates all detected discrepancies and summary metrics.
type DiffReport = fdiff.DiffReport

// DiffEngine performs semantic comparison between local Go contract IR and remote OpenAPI specifications.
type DiffEngine struct{}

// NewEngine creates an initialized DiffEngine instance.
func NewEngine() *DiffEngine {
	return &DiffEngine{}
}
