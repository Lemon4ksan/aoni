// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lint

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/cfg"
)

// CategoryBorrow flags zero-copy lifetime violations, goroutine escapes, and Use-After-Free hazards.
const CategoryBorrow Category = "Borrow"

// RuleBorrowEscape (B001) flags zero-copy borrowed buffers escaping into goroutines or channels.
type RuleBorrowEscape struct{}

func (r *RuleBorrowEscape) ID() string                { return "B001" }
func (r *RuleBorrowEscape) Name() string              { return "borrow-no-escape" }
func (r *RuleBorrowEscape) Category() Category        { return CategoryBorrow }
func (r *RuleBorrowEscape) DefaultSeverity() Severity { return SeverityError }
func (r *RuleBorrowEscape) IsFixable() bool           { return false }
func (r *RuleBorrowEscape) Description() string {
	return "Prohibits zero-copy borrowed buffers from escaping into asynchronous goroutines or channels"
}

func (r *RuleBorrowEscape) Run(pass *Pass) []Diagnostic {
	if pass.ASTFile == nil {
		return nil
	}

	var diags []Diagnostic

	for _, decl := range pass.ASTFile.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}

		borrowedVars := make(map[string]bool)

		// Pass 1: Identify borrowed variables
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}

			for i, rhs := range assign.Rhs {
				rhsStr := exprToString(rhs)
				if isBorrowExpr(rhsStr) {
					if i < len(assign.Lhs) {
						if ident, ok := assign.Lhs[i].(*ast.Ident); ok {
							borrowedVars[ident.Name] = true
						}
					}
				}
			}

			return true
		})

		// Also check function parameters marked with borrow
		if fn.Type != nil && fn.Type.Params != nil {
			for _, param := range fn.Type.Params.List {
				typeStr := exprToString(param.Type)
				if isBorrowType(typeStr) {
					for _, name := range param.Names {
						borrowedVars[name.Name] = true
					}
				}
			}
		}

		if len(borrowedVars) == 0 {
			continue
		}

		// Pass 2: Check for escapes into goroutines (go func) or channels (ch <- x)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.GoStmt:
				for _, arg := range node.Call.Args {
					argName := exprToString(arg)
					if borrowedVars[argName] {
						pos := pass.FileSet.Position(node.Pos())
						msg := fmt.Sprintf(
							"unsafe zero-copy handle %q cannot escape into asynchronous goroutine (Use-After-Free hazard)",
							argName,
						)
						sug := fmt.Sprintf(
							"Clone the buffer using '%s.Clone()' or copy bytes before passing to goroutine",
							argName,
						)

						diags = append(diags, Diagnostic{
							RuleID:     r.ID(),
							RuleName:   r.Name(),
							Severity:   r.DefaultSeverity(),
							Category:   r.Category(),
							Target:     argName,
							Message:    msg,
							FilePath:   pass.FilePath,
							Line:       pos.Line,
							Column:     pos.Column,
							Suggestion: sug,
						})
					}
				}

				if funcLit, ok := node.Call.Fun.(*ast.FuncLit); ok && funcLit.Body != nil {
					ast.Inspect(funcLit.Body, func(inner ast.Node) bool {
						ident, ok := inner.(*ast.Ident)
						if ok && borrowedVars[ident.Name] {
							pos := pass.FileSet.Position(ident.Pos())
							msg := fmt.Sprintf(
								"zero-copy handle %q is captured inside asynchronous goroutine closure",
								ident.Name,
							)
							sug := fmt.Sprintf(
								"Take an explicit '%s.Clone()' snapshot before the goroutine",
								ident.Name,
							)

							diags = append(diags, Diagnostic{
								RuleID:     r.ID(),
								RuleName:   r.Name(),
								Severity:   r.DefaultSeverity(),
								Category:   r.Category(),
								Target:     ident.Name,
								Message:    msg,
								FilePath:   pass.FilePath,
								Line:       pos.Line,
								Column:     pos.Column,
								Suggestion: sug,
							})
						}

						return true
					})
				}

			case *ast.SendStmt:
				valName := exprToString(node.Value)
				if borrowedVars[valName] {
					pos := pass.FileSet.Position(node.Pos())
					diags = append(diags, Diagnostic{
						RuleID:     r.ID(),
						RuleName:   r.Name(),
						Severity:   r.DefaultSeverity(),
						Category:   r.Category(),
						Target:     valName,
						Message:    fmt.Sprintf("cannot send borrowed zero-copy handle %q into channel", valName),
						FilePath:   pass.FilePath,
						Line:       pos.Line,
						Column:     pos.Column,
						Suggestion: fmt.Sprintf("Send cloned bytes '%s.Clone()' through the channel instead", valName),
					})
				}
			}

			return true
		})
	}

	return diags
}

// RuleBorrowUseAfterRelease (B002) flags usage of resources after .Release() or .Move() using path-sensitive CFG.
type RuleBorrowUseAfterRelease struct{}

func (r *RuleBorrowUseAfterRelease) ID() string                { return "B002" }
func (r *RuleBorrowUseAfterRelease) Name() string              { return "borrow-use-after-release" }
func (r *RuleBorrowUseAfterRelease) Category() Category        { return CategoryBorrow }
func (r *RuleBorrowUseAfterRelease) DefaultSeverity() Severity { return SeverityError }
func (r *RuleBorrowUseAfterRelease) IsFixable() bool           { return false }
func (r *RuleBorrowUseAfterRelease) Description() string {
	return "Detects use of a linear resource after it has been released or moved across execution paths"
}

func (r *RuleBorrowUseAfterRelease) Run(pass *Pass) []Diagnostic {
	if pass.ASTFile == nil {
		return nil
	}

	var diags []Diagnostic

	for _, decl := range pass.ASTFile.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}

		borrowVars := make(map[string]bool)

		// Identify borrow variables
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}

			for i, rhs := range assign.Rhs {
				if isBorrowExpr(exprToString(rhs)) {
					if i < len(assign.Lhs) {
						if ident, ok := assign.Lhs[i].(*ast.Ident); ok {
							borrowVars[ident.Name] = true
						}
					}
				}
			}

			return true
		})

		if len(borrowVars) == 0 {
			continue
		}

		// Build CFG for the function body
		g := cfg.New(fn.Body, nil)
		reported := make(map[string]bool)

		// Walk all execution paths through CFG
		g.WalkPaths(func(path []*cfg.Block) {
			releasedAt := make(map[string]int)

			for _, block := range path {
				for _, node := range block.Nodes {
					// Skip defer statements
					if _, isDefer := node.(*ast.DeferStmt); isDefer {
						continue
					}

					// Reassignment resets release status
					if assign, ok := node.(*ast.AssignStmt); ok {
						for _, lhs := range assign.Lhs {
							if ident, ok := lhs.(*ast.Ident); ok {
								delete(releasedAt, ident.Name)
							}
						}
					}

					// Check for call to x.Release() or x.Move()
					ast.Inspect(node, func(inner ast.Node) bool {
						if call, ok := inner.(*ast.CallExpr); ok {
							if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
								if sel.Sel.Name == "Release" || sel.Sel.Name == "Move" {
									if ident, ok := sel.X.(*ast.Ident); ok && borrowVars[ident.Name] {
										pos := pass.FileSet.Position(sel.Pos())
										releasedAt[ident.Name] = pos.Line
									}
								}
							}
						}

						return true
					})

					// Check for use of already released variable on this path
					ast.Inspect(node, func(inner ast.Node) bool {
						if call, ok := inner.(*ast.CallExpr); ok {
							if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
								if sel.Sel.Name == "Release" || sel.Sel.Name == "Move" {
									return false
								}
							}
						}

						if ident, ok := inner.(*ast.Ident); ok && borrowVars[ident.Name] {
							if relLine, released := releasedAt[ident.Name]; released {
								pos := pass.FileSet.Position(ident.Pos())
								if pos.Line > relLine {
									key := fmt.Sprintf("%s:%d:%d", ident.Name, relLine, pos.Line)
									if !reported[key] {
										reported[key] = true
										msg := fmt.Sprintf(
											"use of borrowed resource %q after it was released/moved on line %d",
											ident.Name,
											relLine,
										)
										sug := fmt.Sprintf(
											"Do not access '%s' after calling Release() or Move()",
											ident.Name,
										)

										diags = append(diags, Diagnostic{
											RuleID:     r.ID(),
											RuleName:   r.Name(),
											Severity:   r.DefaultSeverity(),
											Category:   r.Category(),
											Target:     ident.Name,
											Message:    msg,
											FilePath:   pass.FilePath,
											Line:       pos.Line,
											Column:     pos.Column,
											Suggestion: sug,
										})
									}
								}
							}
						}

						return true
					})
				}
			}
		})
	}

	return diags
}

// RuleBorrowMultipleMutable (B003) flags multiple concurrent mutable borrows along the same execution path.
type RuleBorrowMultipleMutable struct{}

func (r *RuleBorrowMultipleMutable) ID() string                { return "B003" }
func (r *RuleBorrowMultipleMutable) Name() string              { return "borrow-exclusive-violation" }
func (r *RuleBorrowMultipleMutable) Category() Category        { return CategoryBorrow }
func (r *RuleBorrowMultipleMutable) DefaultSeverity() Severity { return SeverityError }
func (r *RuleBorrowMultipleMutable) IsFixable() bool           { return false }
func (r *RuleBorrowMultipleMutable) Description() string {
	return "Detects multiple active mutable borrows violating Aliasing XOR Mutability along execution paths"
}

func (r *RuleBorrowMultipleMutable) Run(pass *Pass) []Diagnostic {
	if pass.ASTFile == nil {
		return nil
	}

	var diags []Diagnostic

	for _, decl := range pass.ASTFile.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}

		g := cfg.New(fn.Body, nil)
		reported := make(map[string]bool)

		g.WalkPaths(func(path []*cfg.Block) {
			mutBorrows := make(map[string][]token.Pos)

			for _, block := range path {
				for _, node := range block.Nodes {
					ast.Inspect(node, func(n ast.Node) bool {
						call, ok := n.(*ast.CallExpr)
						if !ok {
							return true
						}

						sel, ok := call.Fun.(*ast.SelectorExpr)
						if !ok {
							return true
						}

						if sel.Sel.Name == "BorrowMut" || sel.Sel.Name == "MustBorrowMut" {
							target := exprToString(sel.X)
							mutBorrows[target] = append(mutBorrows[target], sel.Pos())
						}

						return true
					})
				}
			}

			for target, positions := range mutBorrows {
				if len(positions) > 1 {
					secondPos := pass.FileSet.Position(positions[1])
					firstPos := pass.FileSet.Position(positions[0])
					key := fmt.Sprintf("%s:%d:%d", target, firstPos.Line, secondPos.Line)

					if !reported[key] {
						reported[key] = true
						msg := fmt.Sprintf(
							"multiple active mutable borrows on %q along execution path (first borrow at line %d)",
							target,
							firstPos.Line,
						)

						diags = append(diags, Diagnostic{
							RuleID:     r.ID(),
							RuleName:   r.Name(),
							Severity:   r.DefaultSeverity(),
							Category:   r.Category(),
							Target:     target,
							Message:    msg,
							FilePath:   pass.FilePath,
							Line:       secondPos.Line,
							Column:     secondPos.Column,
							Suggestion: "Freeze the previous borrow with '.Freeze()' or release it before borrowing again",
						})
					}
				}
			}
		})
	}

	return diags
}

// RuleBorrowLinearLeak (B004) flags linear Box resources that are never released or moved before reaching return blocks.
type RuleBorrowLinearLeak struct{}

func (r *RuleBorrowLinearLeak) ID() string                { return "B004" }
func (r *RuleBorrowLinearLeak) Name() string              { return "borrow-must-release" }
func (r *RuleBorrowLinearLeak) Category() Category        { return CategoryBorrow }
func (r *RuleBorrowLinearLeak) DefaultSeverity() Severity { return SeverityError }
func (r *RuleBorrowLinearLeak) IsFixable() bool           { return false }
func (r *RuleBorrowLinearLeak) Description() string {
	return "Warns when an owned linear resource is created but not explicitly released or moved on all return paths"
}

func (r *RuleBorrowLinearLeak) Run(pass *Pass) []Diagnostic {
	if pass.ASTFile == nil {
		return nil
	}

	var diags []Diagnostic

	for _, decl := range pass.ASTFile.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}

		g := cfg.New(fn.Body, nil)
		reported := make(map[string]bool)

		g.WalkPaths(func(path []*cfg.Block) {
			createdBoxes := make(map[string]token.Pos)
			consumedBoxes := make(map[string]bool)

			for _, block := range path {
				for _, node := range block.Nodes {
					switch stmt := node.(type) {
					case *ast.AssignStmt:
						for i, rhs := range stmt.Rhs {
							rhsStr := exprToString(rhs)
							if strings.Contains(rhsStr, "borrow.NewBox") || strings.Contains(rhsStr, "borrow.Alloc") {
								if i < len(stmt.Lhs) {
									if ident, ok := stmt.Lhs[i].(*ast.Ident); ok {
										createdBoxes[ident.Name] = stmt.Pos()
									}
								}
							}
						}

					case *ast.DeferStmt:
						if call, ok := stmt.Call.Fun.(*ast.SelectorExpr); ok {
							if call.Sel.Name == "Release" || call.Sel.Name == "Move" {
								if ident, ok := call.X.(*ast.Ident); ok {
									consumedBoxes[ident.Name] = true
								}
							}
						}

					case *ast.ExprStmt:
						if call, ok := stmt.X.(*ast.CallExpr); ok {
							if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
								if sel.Sel.Name == "Release" || sel.Sel.Name == "Move" {
									if ident, ok := sel.X.(*ast.Ident); ok {
										consumedBoxes[ident.Name] = true
									}
								}
							}
						}

					case *ast.ReturnStmt:
						for _, res := range stmt.Results {
							if ident, ok := res.(*ast.Ident); ok {
								consumedBoxes[ident.Name] = true
							}
						}
					}
				}
			}

			for name, pos := range createdBoxes {
				if !consumedBoxes[name] && !reported[name] {
					reported[name] = true
					filePos := pass.FileSet.Position(pos)
					msg := fmt.Sprintf("owned linear resource %q is not released or moved on this return path", name)
					sug := fmt.Sprintf(
						"Add 'defer %s.Release()' or call '%s.Release()' before function return",
						name,
						name,
					)

					diags = append(diags, Diagnostic{
						RuleID:     r.ID(),
						RuleName:   r.Name(),
						Severity:   r.DefaultSeverity(),
						Category:   r.Category(),
						Target:     name,
						Message:    msg,
						FilePath:   pass.FilePath,
						Line:       filePos.Line,
						Column:     filePos.Column,
						Suggestion: sug,
					})
				}
			}
		})
	}

	return diags
}

// RuleBorrowUnsafeEscape (B005) flags converting borrowed slices into unsafe.Pointer escaping local scope.
type RuleBorrowUnsafeEscape struct{}

func (r *RuleBorrowUnsafeEscape) ID() string                { return "B005" }
func (r *RuleBorrowUnsafeEscape) Name() string              { return "borrow-unsafe-escape" }
func (r *RuleBorrowUnsafeEscape) Category() Category        { return CategoryBorrow }
func (r *RuleBorrowUnsafeEscape) DefaultSeverity() Severity { return SeverityError }
func (r *RuleBorrowUnsafeEscape) IsFixable() bool           { return false }
func (r *RuleBorrowUnsafeEscape) Description() string {
	return "Prohibits unchecked casting of borrowed zero-copy handles to unsafe.Pointer"
}

func (r *RuleBorrowUnsafeEscape) Run(pass *Pass) []Diagnostic {
	if pass.ASTFile == nil {
		return nil
	}

	var diags []Diagnostic

	for _, decl := range pass.ASTFile.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}

		borrowVars := make(map[string]bool)

		// Check parameters
		if fn.Type != nil && fn.Type.Params != nil {
			for _, param := range fn.Type.Params.List {
				if isBorrowType(exprToString(param.Type)) {
					for _, name := range param.Names {
						borrowVars[name.Name] = true
					}
				}
			}
		}

		// Check assignments in body
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if assign, ok := n.(*ast.AssignStmt); ok {
				for i, rhs := range assign.Rhs {
					if isBorrowExpr(exprToString(rhs)) {
						if i < len(assign.Lhs) {
							if ident, ok := assign.Lhs[i].(*ast.Ident); ok {
								borrowVars[ident.Name] = true
							}
						}
					}
				}
			}

			return true
		})

		if len(borrowVars) == 0 {
			continue
		}

		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			callTarget := exprToString(call.Fun)
			if callTarget == "unsafe.Pointer" || callTarget == "uintptr" {
				for _, arg := range call.Args {
					argStr := exprToString(arg)
					for bVar := range borrowVars {
						if strings.Contains(argStr, bVar) {
							pos := pass.FileSet.Position(call.Pos())
							msg := fmt.Sprintf(
								"unsafe pointer conversion of borrowed handle %q bypasses borrow checker safety guarantees",
								bVar,
							)
							sug := fmt.Sprintf(
								"Use '%s.AsSlice()' or safe generic accessors instead of unsafe pointer casting",
								bVar,
							)

							diags = append(diags, Diagnostic{
								RuleID:     r.ID(),
								RuleName:   r.Name(),
								Severity:   r.DefaultSeverity(),
								Category:   r.Category(),
								Target:     bVar,
								Message:    msg,
								FilePath:   pass.FilePath,
								Line:       pos.Line,
								Column:     pos.Column,
								Suggestion: sug,
							})
						}
					}
				}
			}

			return true
		})
	}

	return diags
}

// RuleBorrowLoopCarried (B006) flags storing borrowed buffers from a loop iteration into variables across iterations.
type RuleBorrowLoopCarried struct{}

func (r *RuleBorrowLoopCarried) ID() string                { return "B006" }
func (r *RuleBorrowLoopCarried) Name() string              { return "borrow-loop-carried" }
func (r *RuleBorrowLoopCarried) Category() Category        { return CategoryBorrow }
func (r *RuleBorrowLoopCarried) DefaultSeverity() Severity { return SeverityError }
func (r *RuleBorrowLoopCarried) IsFixable() bool           { return false }
func (r *RuleBorrowLoopCarried) Description() string {
	return "Prevents borrowed buffers created within a loop from leaking to outer scopes across loop cycles"
}

func (r *RuleBorrowLoopCarried) Run(pass *Pass) []Diagnostic {
	if pass.ASTFile == nil {
		return nil
	}

	var diags []Diagnostic

	for _, decl := range pass.ASTFile.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}

		g := cfg.New(fn.Body, nil)
		loopBlocks := g.FindLoopBlocks()

		if len(loopBlocks) == 0 {
			continue
		}

		// Find variables declared/assigned outside loops
		outerVars := make(map[string]bool)

		for _, block := range g.Blocks {
			if !loopBlocks[block] {
				for _, node := range block.Nodes {
					switch s := node.(type) {
					case *ast.AssignStmt:
						for _, lhs := range s.Lhs {
							if ident, ok := lhs.(*ast.Ident); ok {
								outerVars[ident.Name] = true
							}
						}

					case *ast.ValueSpec:
						for _, name := range s.Names {
							outerVars[name.Name] = true
						}
					}
				}
			}
		}

		// Identify borrow variables created inside loop
		loopBorrows := make(map[string]bool)

		for block := range loopBlocks {
			for _, node := range block.Nodes {
				if assign, ok := node.(*ast.AssignStmt); ok {
					for i, rhs := range assign.Rhs {
						if isBorrowExpr(exprToString(rhs)) {
							if i < len(assign.Lhs) {
								if ident, ok := assign.Lhs[i].(*ast.Ident); ok {
									loopBorrows[ident.Name] = true
								}
							}
						}
					}
				}
			}
		}

		// Check assignments inside loop blocks that assign a loop-borrow to an outer variable
		for block := range loopBlocks {
			for _, node := range block.Nodes {
				if assign, ok := node.(*ast.AssignStmt); ok {
					for i, lhs := range assign.Lhs {
						if ident, ok := lhs.(*ast.Ident); ok && outerVars[ident.Name] {
							if i < len(assign.Rhs) {
								rhsStr := exprToString(assign.Rhs[i])
								if loopBorrows[rhsStr] || isBorrowExpr(rhsStr) {
									pos := pass.FileSet.Position(assign.Pos())
									msg := fmt.Sprintf(
										"borrowed buffer created inside loop is assigned to outer variable %q (Use-After-Free on next cycle)",
										ident.Name,
									)
									sug := fmt.Sprintf(
										"Clone the buffer via '%s = %s.Clone()' or copy bytes into pre-allocated slice",
										ident.Name,
										rhsStr,
									)

									diags = append(diags, Diagnostic{
										RuleID:     r.ID(),
										RuleName:   r.Name(),
										Severity:   r.DefaultSeverity(),
										Category:   r.Category(),
										Target:     ident.Name,
										Message:    msg,
										FilePath:   pass.FilePath,
										Line:       pos.Line,
										Column:     pos.Column,
										Suggestion: sug,
									})
								}
							}
						}
					}
				}
			}
		}
	}

	return diags
}

// RuleBorrowReturnEscape (B007) flags functions returning raw slices derived from borrowed parameters without lifetime annotations.
type RuleBorrowReturnEscape struct{}

func (r *RuleBorrowReturnEscape) ID() string                { return "B007" }
func (r *RuleBorrowReturnEscape) Name() string              { return "borrow-return-escape" }
func (r *RuleBorrowReturnEscape) Category() Category        { return CategoryBorrow }
func (r *RuleBorrowReturnEscape) DefaultSeverity() Severity { return SeverityError }
func (r *RuleBorrowReturnEscape) IsFixable() bool           { return false }
func (r *RuleBorrowReturnEscape) Description() string {
	return "Prohibits returning raw byte slices derived directly from borrowed input parameters"
}

func (r *RuleBorrowReturnEscape) Run(pass *Pass) []Diagnostic {
	if pass.ASTFile == nil {
		return nil
	}

	var diags []Diagnostic

	for _, decl := range pass.ASTFile.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || fn.Type == nil || fn.Type.Params == nil {
			continue
		}

		borrowParams := make(map[string]bool)

		for _, param := range fn.Type.Params.List {
			if isBorrowType(exprToString(param.Type)) {
				for _, name := range param.Names {
					borrowParams[name.Name] = true
				}
			}
		}

		if len(borrowParams) == 0 {
			continue
		}

		ast.Inspect(fn.Body, func(n ast.Node) bool {
			ret, ok := n.(*ast.ReturnStmt)
			if !ok {
				return true
			}

			for _, res := range ret.Results {
				resStr := exprToString(res)
				for pName := range borrowParams {
					if strings.Contains(resStr, pName+".AsSlice") || strings.Contains(resStr, pName+".Get") {
						pos := pass.FileSet.Position(ret.Pos())
						msg := fmt.Sprintf(
							"returning raw slice derived from borrowed parameter %q causes dangling reference at caller",
							pName,
						)
						sug := fmt.Sprintf(
							"Return owned 'borrow.Bytes' or clone the slice with '%s.Clone()' before returning",
							pName,
						)

						diags = append(diags, Diagnostic{
							RuleID:     r.ID(),
							RuleName:   r.Name(),
							Severity:   r.DefaultSeverity(),
							Category:   r.Category(),
							Target:     pName,
							Message:    msg,
							FilePath:   pass.FilePath,
							Line:       pos.Line,
							Column:     pos.Column,
							Suggestion: sug,
						})
					}
				}
			}

			return true
		})
	}

	return diags
}

// RuleBorrowAliasInvalidation (B008) flags access to derived sub-slices or pointers after their parent buffer has been released or moved.
type RuleBorrowAliasInvalidation struct{}

func (r *RuleBorrowAliasInvalidation) ID() string                { return "B008" }
func (r *RuleBorrowAliasInvalidation) Name() string              { return "borrow-alias-invalidation" }
func (r *RuleBorrowAliasInvalidation) Category() Category        { return CategoryBorrow }
func (r *RuleBorrowAliasInvalidation) DefaultSeverity() Severity { return SeverityError }
func (r *RuleBorrowAliasInvalidation) IsFixable() bool           { return false }
func (r *RuleBorrowAliasInvalidation) Description() string {
	return "Detects access to derived sub-slices or pointers after their parent borrowed buffer has been released or moved"
}

func (r *RuleBorrowAliasInvalidation) Run(pass *Pass) []Diagnostic {
	if pass.ASTFile == nil {
		return nil
	}

	var diags []Diagnostic

	for _, decl := range pass.ASTFile.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}

		borrowVars := make(map[string]bool)

		if fn.Type != nil && fn.Type.Params != nil {
			for _, param := range fn.Type.Params.List {
				if isBorrowType(exprToString(param.Type)) {
					for _, name := range param.Names {
						borrowVars[name.Name] = true
					}
				}
			}
		}

		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if assign, ok := n.(*ast.AssignStmt); ok {
				for i, rhs := range assign.Rhs {
					if isBorrowExpr(exprToString(rhs)) {
						if i < len(assign.Lhs) {
							if ident, ok := assign.Lhs[i].(*ast.Ident); ok {
								borrowVars[ident.Name] = true
							}
						}
					}
				}
			}

			return true
		})

		if len(borrowVars) == 0 {
			continue
		}

		g := cfg.New(fn.Body, nil)
		reported := make(map[string]bool)

		g.WalkPaths(func(path []*cfg.Block) {
			aliases := make(map[string]string) // aliasName -> parentBorrowVar
			releasedAt := make(map[string]int)

			for _, block := range path {
				for _, node := range block.Nodes {
					if _, isDefer := node.(*ast.DeferStmt); isDefer {
						continue
					}

					// Track alias assignments: sub := b.AsSlice()[:10]
					if assign, ok := node.(*ast.AssignStmt); ok {
						for i, rhs := range assign.Rhs {
							ast.Inspect(rhs, func(inner ast.Node) bool {
								if call, ok := inner.(*ast.CallExpr); ok {
									callStr := exprToString(call.Fun)
									for bVar := range borrowVars {
										hasAlias := strings.Contains(callStr, bVar+".AsSlice") ||
											strings.Contains(callStr, bVar+".Get")

										if hasAlias && i < len(assign.Lhs) {
											if ident, ok := assign.Lhs[i].(*ast.Ident); ok {
												aliases[ident.Name] = bVar
											}
										}
									}
								}

								return true
							})
						}
					}

					// Track parent release
					ast.Inspect(node, func(inner ast.Node) bool {
						if call, ok := inner.(*ast.CallExpr); ok {
							if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
								if sel.Sel.Name == "Release" || sel.Sel.Name == "Move" {
									if ident, ok := sel.X.(*ast.Ident); ok && borrowVars[ident.Name] {
										pos := pass.FileSet.Position(sel.Pos())
										releasedAt[ident.Name] = pos.Line
									}
								}
							}
						}

						return true
					})

					// Check access to alias after parent was released
					ast.Inspect(node, func(inner ast.Node) bool {
						if ident, ok := inner.(*ast.Ident); ok {
							if parent, isAlias := aliases[ident.Name]; isAlias {
								if relLine, released := releasedAt[parent]; released {
									pos := pass.FileSet.Position(ident.Pos())
									if pos.Line > relLine {
										key := fmt.Sprintf("%s:%s:%d:%d", ident.Name, parent, relLine, pos.Line)
										if !reported[key] {
											reported[key] = true
											msg := fmt.Sprintf(
												"use of derived sub-slice %q after parent resource %q was released on line %d",
												ident.Name,
												parent,
												relLine,
											)
											sug := fmt.Sprintf(
												"Do not access derived slice '%s' after '%s.Release()'",
												ident.Name,
												parent,
											)

											diags = append(diags, Diagnostic{
												RuleID:     r.ID(),
												RuleName:   r.Name(),
												Severity:   r.DefaultSeverity(),
												Category:   r.Category(),
												Target:     ident.Name,
												Message:    msg,
												FilePath:   pass.FilePath,
												Line:       pos.Line,
												Column:     pos.Column,
												Suggestion: sug,
											})
										}
									}
								}
							}
						}

						return true
					})
				}
			}
		})
	}

	return diags
}

// RuleBorrowClosureEscape (B009) flags returning closures that capture local borrowed handles.
type RuleBorrowClosureEscape struct{}

func (r *RuleBorrowClosureEscape) ID() string                { return "B009" }
func (r *RuleBorrowClosureEscape) Name() string              { return "borrow-closure-escape" }
func (r *RuleBorrowClosureEscape) Category() Category        { return CategoryBorrow }
func (r *RuleBorrowClosureEscape) DefaultSeverity() Severity { return SeverityError }
func (r *RuleBorrowClosureEscape) IsFixable() bool           { return false }
func (r *RuleBorrowClosureEscape) Description() string {
	return "Prevents returning closures that capture local borrowed handles or sub-slices"
}

func (r *RuleBorrowClosureEscape) Run(pass *Pass) []Diagnostic {
	if pass.ASTFile == nil {
		return nil
	}

	var diags []Diagnostic

	for _, decl := range pass.ASTFile.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}

		borrowVars := make(map[string]bool)

		if fn.Type != nil && fn.Type.Params != nil {
			for _, param := range fn.Type.Params.List {
				if isBorrowType(exprToString(param.Type)) {
					for _, name := range param.Names {
						borrowVars[name.Name] = true
					}
				}
			}
		}

		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if assign, ok := n.(*ast.AssignStmt); ok {
				for i, rhs := range assign.Rhs {
					if isBorrowExpr(exprToString(rhs)) {
						if i < len(assign.Lhs) {
							if ident, ok := assign.Lhs[i].(*ast.Ident); ok {
								borrowVars[ident.Name] = true
							}
						}
					}
				}
			}

			return true
		})

		if len(borrowVars) == 0 {
			continue
		}

		ast.Inspect(fn.Body, func(n ast.Node) bool {
			ret, ok := n.(*ast.ReturnStmt)
			if !ok {
				return true
			}

			for _, res := range ret.Results {
				if funcLit, ok := res.(*ast.FuncLit); ok && funcLit.Body != nil {
					ast.Inspect(funcLit.Body, func(inner ast.Node) bool {
						if ident, ok := inner.(*ast.Ident); ok && borrowVars[ident.Name] {
							pos := pass.FileSet.Position(ident.Pos())
							msg := fmt.Sprintf(
								"borrowed handle %q escapes local function lifetime inside returned closure",
								ident.Name,
							)
							sug := fmt.Sprintf(
								"Clone '%s.Clone()' or copy bytes into heap memory before capturing in returned closure",
								ident.Name,
							)

							diags = append(diags, Diagnostic{
								RuleID:     r.ID(),
								RuleName:   r.Name(),
								Severity:   r.DefaultSeverity(),
								Category:   r.Category(),
								Target:     ident.Name,
								Message:    msg,
								FilePath:   pass.FilePath,
								Line:       pos.Line,
								Column:     pos.Column,
								Suggestion: sug,
							})
						}

						return true
					})
				}
			}

			return true
		})
	}

	return diags
}

// Helper utilities for AST parsing in borrow rules.

func isBorrowExpr(expr string) bool {
	return strings.Contains(expr, "borrow.NewBytes") ||
		strings.Contains(expr, "AllocBytes") ||
		strings.Contains(expr, "Borrow") ||
		strings.Contains(expr, "BorrowMut") ||
		strings.Contains(expr, "BodyUnsafe") ||
		strings.Contains(expr, "BodyBytes") ||
		strings.Contains(expr, "borrow.NewBox")
}

func isBorrowType(typeStr string) bool {
	return strings.Contains(typeStr, "borrow.Bytes") ||
		strings.Contains(typeStr, "borrow.Ref") ||
		strings.Contains(typeStr, "borrow.Mut") ||
		strings.Contains(typeStr, "borrow.Box") ||
		strings.Contains(typeStr, "UnsafeBytes")
}

func exprToString(expr ast.Expr) string {
	if expr == nil {
		return ""
	}

	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return exprToString(e.X) + "." + e.Sel.Name
	case *ast.CallExpr:
		return exprToString(e.Fun)
	case *ast.StarExpr:
		return "*" + exprToString(e.X)
	case *ast.UnaryExpr:
		return exprToString(e.X)
	case *ast.IndexExpr:
		return exprToString(e.X)
	case *ast.SliceExpr:
		return exprToString(e.X)
	default:
		return ""
	}
}
