// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lint

import (
	"strings"
	"sync"
)

// Registry manages the set of active and available linting rules.
type Registry struct {
	mu       sync.RWMutex
	rules    []Rule
	byID     map[string]Rule
	byName   map[string]Rule
	disabled map[string]bool
}

// NewRegistry constructs an empty rule Registry.
func NewRegistry() *Registry {
	return &Registry{
		rules:    make([]Rule, 0),
		byID:     make(map[string]Rule),
		byName:   make(map[string]Rule),
		disabled: make(map[string]bool),
	}
}

// DefaultRegistry constructs a Registry pre-populated with all standard built-in rules.
func DefaultRegistry() *Registry {
	reg := NewRegistry()
	reg.Register(
		// 1. Errors (E) - Compiler invariants & RFC standards
		&RuleStaleCodegen{},
		&RuleUnmatchedPath{},
		&RuleMissingHTTPMethod{},
		&RuleMissingContext{},
		&RuleUnrecognizedDirective{},
		&RuleConflictingPayload{},
		&RuleIllegalBodyMethod{},
		&RuleMissingErrorReturn{},
		&RuleUnboundHeaderVariable{},
		&RuleDuplicateRouteCollision{},
		&RuleInvalidCheckField{},
		&RuleInvalidBitpack{},
		&RuleShadowedWireName{},
		&RuleInvalidUnionStatus{},
		&RuleMirrorSourceNotFound{},
		&RuleMirrorSignatureDrift{},

		// 2. Performance (P) - Zero-alloc & stack safety
		&RuleMissingDTOEncoder{},
		&RuleAnyParamBoxing{},
		&RuleUnformattedSliceStrategy{},
		&RuleOversizedStackFrame{},
		&RuleMissingCoalesceOnHeavyGet{},

		// 3. Security (S) - Secrets & stealth protection
		&RuleSensitiveQueryParam{},
		&RuleHardcodedSigningSecret{},
		&RuleHeaderCRLFInjectionRisk{},
		&RuleNakedScraperContract{},

		// 4. Style / Hygiene (W) - Contract clarity & format
		&RuleParamLifting{},
		&RuleDeprecatedAlias{},
		&RuleHTTPVerbMismatch{},
		&RuleRedundantTag{},
		&RuleCanonicalFormat{},
		&RuleDeadDirective{},
		&RuleUnusedParam{},
		&RuleInvalidStatusCodeRange{},
		&RuleDuplicateOperationID{},
		&RuleDeprecatedTargetValidation{},
		&RuleMirrorGhostMethod{},
	)

	return reg
}

// Register registers one or more pluggable rules into the Registry.
func (r *Registry) Register(rules ...Rule) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, rule := range rules {
		id := strings.ToLower(rule.ID())
		name := strings.ToLower(rule.Name())

		r.rules = append(r.rules, rule)
		r.byID[id] = rule
		r.byName[name] = rule
	}
}

// Disable disables rules by ID or name.
func (r *Registry) Disable(names ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, n := range names {
		clean := strings.ToLower(strings.TrimSpace(n))
		r.disabled[clean] = true
	}
}

// Enable re-enables previously disabled rules.
func (r *Registry) Enable(names ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, n := range names {
		clean := strings.ToLower(strings.TrimSpace(n))
		delete(r.disabled, clean)
	}
}

// ActiveRules returns all registered rules that are currently enabled.
func (r *Registry) ActiveRules() []Rule {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var active []Rule
	for _, rule := range r.rules {
		id := strings.ToLower(rule.ID())
		name := strings.ToLower(rule.Name())

		if r.disabled[id] || r.disabled[name] {
			continue
		}

		active = append(active, rule)
	}

	return active
}
