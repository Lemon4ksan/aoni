// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package analysis

import "github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"

// Rule function types partitioned by AST inspection level:
type (
	// RootRule inspects the top-level [ir.RootIR] package metadata.
	RootRule func(ctx *Context, root *ir.RootIR)

	// ServiceRule inspects an individual [ir.ServiceIR] interface.
	ServiceRule func(ctx *Context, svc *ir.ServiceIR)

	// MethodRule inspects an individual [ir.MethodIR] endpoint operation.
	MethodRule func(ctx *Context, svc *ir.ServiceIR, m *ir.MethodIR)

	// StructRule inspects a [ir.StructIR] DTO definition.
	StructRule func(ctx *Context, s *ir.StructIR)

	// TupleRule inspects a [ir.TupleIR] positional tuple.
	TupleRule func(ctx *Context, t *ir.TupleIR)

	// BitpackRule inspects a [ir.BitpackIR] compact bitfield.
	BitpackRule func(ctx *Context, b *ir.BitpackIR)
)

// Engine manages and executes an extensible pipeline of AST semantic verification rules.
type Engine struct {
	rootRules    []RootRule
	serviceRules []ServiceRule
	methodRules  []MethodRule
	structRules  []StructRule
	tupleRules   []TupleRule
	bitpackRules []BitpackRule
}

// NewEngine creates an empty semantic analysis [Engine].
func NewEngine() *Engine {
	return &Engine{
		rootRules:    make([]RootRule, 0, 8),
		serviceRules: make([]ServiceRule, 0, 8),
		methodRules:  make([]MethodRule, 0, 16),
		structRules:  make([]StructRule, 0, 8),
		tupleRules:   make([]TupleRule, 0, 4),
		bitpackRules: make([]BitpackRule, 0, 4),
	}
}

// AddRootRules registers rules operating on the root AST.
func (e *Engine) AddRootRules(rules ...RootRule) *Engine {
	e.rootRules = append(e.rootRules, rules...)
	return e
}

// AddServiceRules registers rules operating on service interfaces.
func (e *Engine) AddServiceRules(rules ...ServiceRule) *Engine {
	e.serviceRules = append(e.serviceRules, rules...)
	return e
}

// AddMethodRules registers rules operating on individual methods.
func (e *Engine) AddMethodRules(rules ...MethodRule) *Engine {
	e.methodRules = append(e.methodRules, rules...)
	return e
}

// AddStructRules registers rules operating on struct DTOs.
func (e *Engine) AddStructRules(rules ...StructRule) *Engine {
	e.structRules = append(e.structRules, rules...)
	return e
}

// AddTupleRules registers rules operating on tuple definitions.
func (e *Engine) AddTupleRules(rules ...TupleRule) *Engine {
	e.tupleRules = append(e.tupleRules, rules...)
	return e
}

// AddBitpackRules registers rules operating on bitpack definitions.
func (e *Engine) AddBitpackRules(rules ...BitpackRule) *Engine {
	e.bitpackRules = append(e.bitpackRules, rules...)
	return e
}

// Run executes the complete semantic verification pipeline over root and returns all findings.
func (e *Engine) Run(root *ir.RootIR) []Diagnostic {
	ctx := NewContext()

	if root == nil {
		ctx.Error("root/nil", "root", "IR root is nil")
		return ctx.Diagnostics()
	}

	for _, rule := range e.rootRules {
		rule(ctx, root)
	}

	for _, svc := range root.Services {
		for _, sRule := range e.serviceRules {
			sRule(ctx, svc)
		}

		for _, m := range svc.Methods {
			for _, mRule := range e.methodRules {
				mRule(ctx, svc, m)
			}
		}
	}

	for _, strct := range root.Structs {
		for _, stRule := range e.structRules {
			stRule(ctx, strct)
		}
	}

	for _, tuple := range root.Tuples {
		for _, tRule := range e.tupleRules {
			tRule(ctx, tuple)
		}
	}

	for _, bitpack := range root.Bitpacks {
		for _, bRule := range e.bitpackRules {
			bRule(ctx, bitpack)
		}
	}

	return ctx.Diagnostics()
}

// DefaultEngine constructs the canonical semantic verification engine with all standard rules enabled.
func DefaultEngine() *Engine {
	e := NewEngine()

	// Root rules (type collisions & unknown directives)
	e.AddRootRules(
		RuleUniqueTypeNames,
		RuleUnrecognizedDirectives,
	)

	// Service-level rules
	e.AddServiceRules(
		RuleServiceNameNotEmpty,
		RuleServiceMethodsDeclared,
		RuleServiceUniqueMethodNames,
		RuleServiceDurations,
		RuleServiceRetryStatus,
	)

	// Method-level rules
	e.AddMethodRules(
		RuleMethodHTTPDirective,
		RuleMethodContextParameter,
		RuleMethodUniqueParamNames,
		RuleMethodWireKeys,
		RuleMethodPathVariables,
		RuleMethodDynamicHeaders,
		RuleMethodReturnSignature,
		RuleMethodBodyPayloadLimit,
		RuleMethodHTTPPayloadSemantics,
		RuleMethodStatusAndDurations,
	)

	// Type DTO rules
	e.AddStructRules(RuleStructFieldWireNames)
	e.AddTupleRules(RuleTupleNonEmptyFields)
	e.AddBitpackRules(RuleBitpackNonEmptyFields)

	return e
}
