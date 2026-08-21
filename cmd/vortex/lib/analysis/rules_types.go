// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package analysis

import (
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
)

// RuleUniqueTypeNames guards against duplicate type declarations across structs, tuples, and bitpacks.
func RuleUniqueTypeNames(ctx *Context, root *ir.RootIR) {
	typeNames := make(map[string]string, len(root.Structs)+len(root.Tuples)+len(root.Bitpacks))

	for _, s := range root.Structs {
		if s.Name == "" {
			ctx.Error("type/empty-name", "struct", "struct name cannot be empty")
			continue
		}

		if prevKind, exists := typeNames[s.Name]; exists {
			ctx.Errorf(
				"type/duplicate-declaration",
				s.Name,
				"duplicate type declaration %q (conflicts with existing %s)",
				s.Name,
				prevKind,
			)
		}
		typeNames[s.Name] = "struct"
	}

	for _, t := range root.Tuples {
		if t.Name == "" {
			ctx.Error("type/empty-name", "tuple", "tuple name cannot be empty")
			continue
		}

		if prevKind, exists := typeNames[t.Name]; exists {
			ctx.Errorf(
				"type/duplicate-declaration",
				t.Name,
				"duplicate type declaration %q (conflicts with existing %s)",
				t.Name,
				prevKind,
			)
		}
		typeNames[t.Name] = "tuple"
	}

	for _, b := range root.Bitpacks {
		if b.Name == "" {
			ctx.Error("type/empty-name", "bitpack", "bitpack name cannot be empty")
			continue
		}

		if prevKind, exists := typeNames[b.Name]; exists {
			ctx.Errorf(
				"type/duplicate-declaration",
				b.Name,
				"duplicate type declaration %q (conflicts with existing %s)",
				b.Name,
				prevKind,
			)
		}
		typeNames[b.Name] = "bitpack"
	}
}

// RuleStructFieldWireNames ensures fields declare non-empty and non-conflicting wire names.
func RuleStructFieldWireNames(ctx *Context, s *ir.StructIR) {
	wireNames := make(map[string]string, len(s.Fields))

	for _, f := range s.Fields {
		if f.WireName == "" {
			ctx.Errorf(
				"type/empty-wire-name",
				ctx.Target(s.Name, f.GoName),
				"wire field name cannot be empty",
			)
			continue
		}

		if prevGoName, exists := wireNames[f.WireName]; exists {
			ctx.Errorf(
				"type/duplicate-wire-name",
				ctx.Target(s.Name, f.GoName),
				"duplicate wire name %q (conflicts with %s)",
				f.WireName,
				prevGoName,
			)
			continue
		}

		wireNames[f.WireName] = f.GoName
	}
}

// RuleTupleNonEmptyFields ensures every tuple defines at least one element field.
func RuleTupleNonEmptyFields(ctx *Context, t *ir.TupleIR) {
	if len(t.Fields) == 0 {
		ctx.Error("type/empty-tuple", t.Name, "tuple must contain at least one field")
	}
}

// RuleBitpackNonEmptyFields ensures every bitpack defines at least one bit field.
func RuleBitpackNonEmptyFields(ctx *Context, b *ir.BitpackIR) {
	if len(b.Fields) == 0 {
		ctx.Error("type/empty-bitpack", b.Name, "bitpack must contain at least one field")
	}
}
