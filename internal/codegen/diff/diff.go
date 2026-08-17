// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package diff implements semantic contract drift analysis between local Go interfaces and OpenAPI specifications.
package diff

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DriftSeverity classifies the breaking impact of a contract discrepancy.
type DriftSeverity string

const (
	// SeverityBreaking indicates an incompatible API break (e.g. missing required parameter, removed route, type drift).
	SeverityBreaking DriftSeverity = "BREAKING"

	// SeverityNonBreaking indicates a backwards-compatible addition or modification (e.g. new optional query param, doc updates).
	SeverityNonBreaking DriftSeverity = "NON_BREAKING"

	// SeverityGhost indicates an endpoint declared in one contract but completely missing in the other.
	SeverityGhost DriftSeverity = "GHOST"
)

// DriftKind identifies the specific nature of a drift item.
type DriftKind string

const (
	// DriftMissingEndpoint indicates an endpoint present in one contract but absent in the other.
	DriftMissingEndpoint DriftKind = "missing-endpoint"

	// DriftHTTPMethodMismatch indicates differing HTTP methods on the same route.
	DriftHTTPMethodMismatch DriftKind = "method-mismatch"

	// DriftPathMismatch indicates differing path variable structures.
	DriftPathMismatch DriftKind = "path-mismatch"

	// DriftMissingParam indicates a required or optional parameter missing in Go contract.
	DriftMissingParam DriftKind = "missing-param"

	// DriftExtraParam indicates a parameter present in Go contract that does not exist in remote spec.
	DriftExtraParam DriftKind = "extra-param"

	// DriftTypeMismatch indicates incompatible type mapping for a parameter or body.
	DriftTypeMismatch DriftKind = "type-mismatch"

	// DriftResponseMismatch indicates divergent success model or status code definitions.
	DriftResponseMismatch DriftKind = "response-mismatch"

	// DriftDeprecationMismatch indicates one contract is deprecated while the other is active.
	DriftDeprecationMismatch DriftKind = "deprecation-mismatch"
)

// DiffOptions configures semantic contract comparison behavior.
type DiffOptions struct {
	// Additive mode ignores local Go methods absent from remote specification/HAR (suppresses ghost noise).
	Additive bool
}

// DriftItem represents a single detected discrepancy between contracts.
type DriftItem struct {
	Severity   DriftSeverity `json:"severity"`
	Kind       DriftKind     `json:"kind"`
	Service    string        `json:"service,omitempty"`
	Method     string        `json:"method,omitempty"`
	Endpoint   string        `json:"endpoint"`
	Param      string        `json:"param,omitempty"`
	Expected   string        `json:"expected,omitempty"`
	Actual     string        `json:"actual,omitempty"`
	Message    string        `json:"message"`
	Suggestion string        `json:"suggestion,omitempty"`
}

// DiffReport aggregates all detected discrepancies and summary metrics.
type DiffReport struct {
	LocalTarget           string      `json:"local_target"`
	RemoteTarget          string      `json:"remote_target"`
	TotalEndpointsChecked int         `json:"total_endpoints_checked"`
	Additive              bool        `json:"additive,omitempty"`
	Drifts                []DriftItem `json:"drifts"`
}

// BreakingCount returns the number of breaking changes detected.
func (r *DiffReport) BreakingCount() int {
	count := 0
	for _, d := range r.Drifts {
		if d.Severity == SeverityBreaking {
			count++
		}
	}

	return count
}

// NonBreakingCount returns the number of non-breaking changes detected.
func (r *DiffReport) NonBreakingCount() int {
	count := 0
	for _, d := range r.Drifts {
		if d.Severity == SeverityNonBreaking {
			count++
		}
	}

	return count
}

// GhostCount returns the number of ghost endpoints detected.
func (r *DiffReport) GhostCount() int {
	count := 0
	for _, d := range r.Drifts {
		if d.Severity == SeverityGhost {
			count++
		}
	}

	return count
}

// HasBreaking reports whether any breaking changes were discovered.
func (r *DiffReport) HasBreaking() bool {
	return r.BreakingCount() > 0
}

// HasDrift reports whether any drifts of any severity exist.
func (r *DiffReport) HasDrift() bool {
	return len(r.Drifts) > 0
}

// Render formats the report as human-readable terminal output.
func (r *DiffReport) Render(color bool) string {
	if len(r.Drifts) == 0 {
		if r.Additive {
			return "✔ All incoming captured endpoints and schemas are 100% satisfied by local contract! (0 new drifts)\n"
		}

		return "✔ All contracts are 100% in sync with OpenAPI specification! (0 drifts detected)\n"
	}

	var sb strings.Builder
	if r.Additive {
		sb.WriteString("⚡ Vortex Schema Drift Inspector (Additive Mode: Ghost Endpoints Suppressed)\n")
	} else {
		sb.WriteString("⚡ Vortex Schema Drift Inspector\n")
	}

	fmt.Fprintf(&sb, "Local:  %s\n", r.LocalTarget)
	fmt.Fprintf(&sb, "Remote: %s\n", r.RemoteTarget)

	if r.Additive {
		fmt.Fprintf(&sb, "Checked %d incoming endpoint(s) against local contract\n\n", r.TotalEndpointsChecked)
	} else {
		fmt.Fprintf(&sb, "Checked %d endpoint(s)\n\n", r.TotalEndpointsChecked)
	}

	var (
		breaking    []DriftItem
		nonBreaking []DriftItem
		ghosts      []DriftItem
	)

	for _, d := range r.Drifts {
		switch d.Severity {
		case SeverityBreaking:
			breaking = append(breaking, d)
		case SeverityNonBreaking:
			nonBreaking = append(nonBreaking, d)
		case SeverityGhost:
			ghosts = append(ghosts, d)
		}
	}

	if len(breaking) > 0 {
		fmt.Fprintf(&sb, "◆ Breaking Changes (%d)\n", len(breaking))

		for _, b := range breaking {
			fmt.Fprintf(&sb, "  ↳ [BREAKING:%s] %s\n", b.Kind, b.Endpoint)
			fmt.Fprintf(&sb, "    %s\n", b.Message)

			if b.Expected != "" && b.Actual != "" {
				fmt.Fprintf(&sb, "    Expected (Remote): %s | Actual (Local): %s\n", b.Expected, b.Actual)
			}

			if b.Suggestion != "" {
				fmt.Fprintf(&sb, "    ↳ Suggestion: %s\n", b.Suggestion)
			}
		}

		sb.WriteString("\n")
	}

	if len(nonBreaking) > 0 {
		fmt.Fprintf(&sb, "◆ Non-Breaking Drifts (%d)\n", len(nonBreaking))

		for _, nb := range nonBreaking {
			fmt.Fprintf(&sb, "  ↳ [INFO:%s] %s\n", nb.Kind, nb.Endpoint)
			fmt.Fprintf(&sb, "    %s\n", nb.Message)

			if nb.Suggestion != "" {
				fmt.Fprintf(&sb, "    ↳ Suggestion: %s\n", nb.Suggestion)
			}
		}

		sb.WriteString("\n")
	}

	if len(ghosts) > 0 {
		fmt.Fprintf(&sb, "◆ Ghost Endpoints (%d)\n", len(ghosts))

		for _, g := range ghosts {
			fmt.Fprintf(&sb, "  ↳ [GHOST:%s] %s\n", g.Kind, g.Endpoint)
			fmt.Fprintf(&sb, "    %s\n", g.Message)
		}

		sb.WriteString("\n")
	}

	if r.Additive {
		fmt.Fprintf(&sb, "Summary: %d breaking, %d non-breaking (ghost endpoints ignored in additive mode)\n",
			len(breaking),
			len(nonBreaking))
	} else {
		fmt.Fprintf(&sb, "Summary: %d breaking, %d non-breaking, %d ghost(s)\n",
			len(breaking),
			len(nonBreaking),
			len(ghosts))
	}

	return sb.String()
}

// RenderJSON returns machine-readable JSON bytes.
func (r *DiffReport) RenderJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// DiffEngine performs semantic comparison between local Go contract IR and remote OpenAPI specifications.
type DiffEngine struct{}

// NewEngine creates an initialized DiffEngine instance.
func NewEngine() *DiffEngine {
	return &DiffEngine{}
}
