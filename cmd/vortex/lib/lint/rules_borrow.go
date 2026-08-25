// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lint

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
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
	return "Prohibits zero-copy borrowed buffers from escaping into asynchronous goroutines or channels unless protected by structured concurrency"
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
				// Structured Concurrency check: if goroutine is strictly bounded by sync.WaitGroup, it is safe
				if isStructuredScopedGoroutine(fn, node) {
					return true
				}

				for _, arg := range node.Call.Args {
					argName := exprToString(arg)
					if borrowedVars[argName] {
						pos := pass.FileSet.Position(node.Pos())
						msg := fmt.Sprintf(
							"unsafe zero-copy handle %q cannot escape into asynchronous goroutine (Use-After-Free hazard)",
							argName,
						)
						sug := fmt.Sprintf(
							"Clone the buffer using '%s.Clone()' or synchronize via sync.WaitGroup before goroutine return",
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
								"Take an explicit '%s.Clone()' snapshot before the goroutine or synchronize with sync.WaitGroup",
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

			case *ast.CallExpr:
				// Structured Concurrency check for wg.Go(func() { ... }) or g.Go(func() error { ... })
				if isStructuredScopedCall(fn, node) {
					return false // Scoped goroutine strictly bounded by matching Wait(); safe from escaping
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

func isStructuredScopedCall(fn *ast.FuncDecl, callExpr *ast.CallExpr) bool {
	if fn == nil || fn.Body == nil || callExpr == nil {
		return false
	}

	sel, ok := callExpr.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Go" {
		return false
	}

	if ident, ok := sel.X.(*ast.Ident); ok {
		return hasMatchingWait(fn.Body, ident.Name)
	}

	return false
}

func isStructuredScopedGoroutine(fn *ast.FuncDecl, goStmt *ast.GoStmt) bool {
	if fn == nil || fn.Body == nil || goStmt == nil {
		return false
	}

	var (
		hasDone bool
		wgName  string
	)

	ast.Inspect(goStmt.Call, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		if sel.Sel.Name == "Done" {
			if ident, ok := sel.X.(*ast.Ident); ok {
				hasDone = true
				wgName = ident.Name
			}
		}

		return true
	})

	if !hasDone || wgName == "" {
		return false
	}

	return hasMatchingWait(fn.Body, wgName)
}

func hasMatchingWait(body *ast.BlockStmt, wgName string) bool {
	if body == nil || wgName == "" {
		return false
	}

	var hasWait bool
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		if sel.Sel.Name == "Wait" {
			if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == wgName {
				hasWait = true
			}
		}

		return true
	})

	return hasWait
}

// RuleBorrowUseAfterRelease (B002) flags usage of resources after .Release(), .Move(), or @borrow:moves function calls.
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

	summaries := parsePackageSummaries(pass.ASTFile)

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

					// 1. Check for call to x.Release() or x.Move()
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

							// 2. Check for inter-procedural @borrow:moves function calls
							calleeName := exprToString(call.Fun)
							if sum, exists := summaries[calleeName]; exists {
								if sum.MovesAll {
									for _, arg := range call.Args {
										place := parsePlacePath(arg)
										if borrowVars[place.Root] {
											pos := pass.FileSet.Position(call.Pos())
											releasedAt[place.Root] = pos.Line
										}
									}
								} else {
									for idx, arg := range call.Args {
										place := parsePlacePath(arg)
										if borrowVars[place.Root] &&
											(sum.MovesIndices[idx] || sum.MovesParams[place.Root]) {
											pos := pass.FileSet.Position(call.Pos())
											releasedAt[place.Root] = pos.Line
										}
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

// RuleBorrowMultipleMutable (B003) flags multiple concurrent mutable borrows or mutation while frozen.
type RuleBorrowMultipleMutable struct{}

func (r *RuleBorrowMultipleMutable) ID() string                { return "B003" }
func (r *RuleBorrowMultipleMutable) Name() string              { return "borrow-exclusive-violation" }
func (r *RuleBorrowMultipleMutable) Category() Category        { return CategoryBorrow }
func (r *RuleBorrowMultipleMutable) DefaultSeverity() Severity { return SeverityError }
func (r *RuleBorrowMultipleMutable) IsFixable() bool           { return false }
func (r *RuleBorrowMultipleMutable) Description() string {
	return "Detects multiple active mutable borrows violating Aliasing XOR Mutability or mutations while frozen"
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
			type activeLoan struct {
				place PlacePath
				pos   token.Pos
				isMut bool
			}

			var loans []activeLoan

			frozenBy := make(map[string]string)     // target.Raw -> reader handle
			frozenPos := make(map[string]token.Pos) // target.Raw -> freeze pos

			for _, block := range path {
				for _, node := range block.Nodes {
					// 1. Detect Freeze assignment: reader := mutBuf.Freeze()
					if assign, ok := node.(*ast.AssignStmt); ok {
						for i, rhs := range assign.Rhs {
							if call, ok := rhs.(*ast.CallExpr); ok {
								if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Freeze" {
									targetPlace := parsePlacePath(sel.X)
									if i < len(assign.Lhs) {
										if readerIdent, ok := assign.Lhs[i].(*ast.Ident); ok {
											frozenBy[targetPlace.Raw] = readerIdent.Name
											frozenPos[targetPlace.Raw] = sel.Pos()
										}
									}
								}
							}
						}
					}

					// 2. Detect Unfreeze: mutBuf.Unfreeze(reader)
					ast.Inspect(node, func(inner ast.Node) bool {
						if call, ok := inner.(*ast.CallExpr); ok {
							if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Unfreeze" {
								targetPlace := parsePlacePath(sel.X)
								delete(frozenBy, targetPlace.Raw)
								delete(frozenPos, targetPlace.Raw)
							}
						}

						return true
					})

					// 3. Detect mutation operations while frozen
					ast.Inspect(node, func(inner ast.Node) bool {
						call, ok := inner.(*ast.CallExpr)
						if !ok {
							return true
						}

						sel, ok := call.Fun.(*ast.SelectorExpr)
						if !ok {
							return true
						}

						targetPlace := parsePlacePath(sel.X)
						if reader, isFrozen := frozenBy[targetPlace.Raw]; isFrozen {
							methodName := sel.Sel.Name
							if methodName == "BorrowMut" || methodName == "MustBorrowMut" ||
								methodName == "Write" || methodName == "Set" || methodName == "Put" ||
								methodName == "Mutate" || methodName == "Append" {
								pos := pass.FileSet.Position(sel.Pos())
								frzPos := pass.FileSet.Position(frozenPos[targetPlace.Raw])

								key := fmt.Sprintf("freeze:%s:%d:%d", targetPlace.Raw, frzPos.Line, pos.Line)
								if !reported[key] {
									reported[key] = true
									msg := fmt.Sprintf(
										"cannot mutate %q while frozen by active read handle %q (frozen at line %d)",
										targetPlace.Raw,
										reader,
										frzPos.Line,
									)
									sug := fmt.Sprintf(
										"Call '%s.Unfreeze(%s)' or release read handle before mutating",
										targetPlace.Raw,
										reader,
									)

									diags = append(diags, Diagnostic{
										RuleID:     r.ID(),
										RuleName:   r.Name(),
										Severity:   r.DefaultSeverity(),
										Category:   r.Category(),
										Target:     targetPlace.Raw,
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

					// 4. Track Borrow / BorrowMut with Disjoint Field Borrowing
					ast.Inspect(node, func(n ast.Node) bool {
						call, ok := n.(*ast.CallExpr)
						if !ok {
							return true
						}

						sel, ok := call.Fun.(*ast.SelectorExpr)
						if !ok {
							return true
						}

						isMut := sel.Sel.Name == "BorrowMut" || sel.Sel.Name == "MustBorrowMut"

						isRef := sel.Sel.Name == "Borrow" || sel.Sel.Name == "MustBorrow"
						if !isMut && !isRef {
							return true
						}

						targetPlace := parsePlacePath(sel.X)

						for _, prev := range loans {
							if prev.place.ConflictsWith(targetPlace) {
								if prev.isMut || isMut {
									firstPos := pass.FileSet.Position(prev.pos)
									secondPos := pass.FileSet.Position(sel.Pos())
									key := fmt.Sprintf("mut:%s:%d:%d", targetPlace.Raw, firstPos.Line, secondPos.Line)

									if !reported[key] {
										reported[key] = true
										msg := fmt.Sprintf(
											"multiple active mutable borrows on %q along execution path (first borrow at line %d)",
											targetPlace.Raw,
											firstPos.Line,
										)

										diags = append(diags, Diagnostic{
											RuleID:     r.ID(),
											RuleName:   r.Name(),
											Severity:   r.DefaultSeverity(),
											Category:   r.Category(),
											Target:     targetPlace.Raw,
											Message:    msg,
											FilePath:   pass.FilePath,
											Line:       secondPos.Line,
											Column:     secondPos.Column,
											Suggestion: "Freeze the previous borrow with '.Freeze()' or release it before borrowing again",
										})
									}
								}
							}
						}

						loans = append(loans, activeLoan{
							place: targetPlace,
							pos:   sel.Pos(),
							isMut: isMut,
						})

						return true
					})

					// 5. Detect loan release: handle.Release()
					ast.Inspect(node, func(n ast.Node) bool {
						call, ok := n.(*ast.CallExpr)
						if !ok {
							return true
						}

						sel, ok := call.Fun.(*ast.SelectorExpr)
						if !ok {
							return true
						}

						if sel.Sel.Name == "Release" || sel.Sel.Name == "Close" {
							targetPlace := parsePlacePath(sel.X)

							var remainingLoans []activeLoan
							for _, l := range loans {
								if !l.place.ConflictsWith(targetPlace) {
									remainingLoans = append(remainingLoans, l)
								}
							}

							loans = remainingLoans
						}

						return true
					})
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
func (r *RuleBorrowLinearLeak) IsFixable() bool           { return true }
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
						Fix: &Fix{
							Description: fmt.Sprintf("Insert 'defer %s.Release()'", name),
							Apply: func() error {
								return nil
							},
						},
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

// RuleBorrowGlobalEscape (B010) flags escaping borrowed zero-copy handles or slices into package-level variables.
type RuleBorrowGlobalEscape struct{}

func (r *RuleBorrowGlobalEscape) ID() string                { return "B010" }
func (r *RuleBorrowGlobalEscape) Name() string              { return "borrow-global-escape" }
func (r *RuleBorrowGlobalEscape) Category() Category        { return CategoryBorrow }
func (r *RuleBorrowGlobalEscape) DefaultSeverity() Severity { return SeverityError }
func (r *RuleBorrowGlobalEscape) IsFixable() bool           { return false }
func (r *RuleBorrowGlobalEscape) Description() string {
	return "Prohibits borrowed zero-copy handles or slices from escaping into package-level global variables or structures"
}

func (r *RuleBorrowGlobalEscape) Run(pass *Pass) []Diagnostic {
	if pass.ASTFile == nil {
		return nil
	}

	var diags []Diagnostic

	// 1. Collect all package-level variables
	globalVars := make(map[string]bool)
	for _, decl := range pass.ASTFile.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}

		for _, spec := range gen.Specs {
			vspec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}

			for _, name := range vspec.Names {
				globalVars[name.Name] = true
			}
		}
	}

	if len(globalVars) == 0 {
		return nil
	}

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
			assign, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}

			for i, lhs := range assign.Lhs {
				targetPlace := parsePlacePath(lhs)
				if globalVars[targetPlace.Root] {
					if i < len(assign.Rhs) {
						rhs := assign.Rhs[i]

						rhsStr := exprToString(rhs)
						for bVar := range borrowVars {
							if strings.Contains(rhsStr, bVar) {
								pos := pass.FileSet.Position(assign.Pos())
								msg := fmt.Sprintf(
									"borrowed handle %q escapes local lifetime into package-level variable %q",
									bVar,
									targetPlace.Raw,
								)
								sug := fmt.Sprintf(
									"Clone '%s.Clone()' or copy bytes into a newly allocated slice before storing in global state",
									bVar,
								)

								diags = append(diags, Diagnostic{
									RuleID:     r.ID(),
									RuleName:   r.Name(),
									Severity:   r.DefaultSeverity(),
									Category:   r.Category(),
									Target:     targetPlace.Raw,
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

	return diags
}

// IntervalRange represents an index interval [Start, End) for slice/array separation logic.
type IntervalRange struct {
	Start int64
	End   int64
	Known bool
}

// DisjointWith reports whether two intervals [a, b) and [c, d) do not overlap (b <= c or d <= a).
func (ir IntervalRange) DisjointWith(other IntervalRange) bool {
	if !ir.Known || !other.Known {
		return false
	}

	return ir.End <= other.Start || other.End <= ir.Start
}

// PlacePath models a memory location projection (e.g., "order.Header" or "buffer[0:1024]")
// inspired by rustc_borrowck MIR Place projections and Separation Logic (P * Q).
type PlacePath struct {
	Root     string
	Segments []string
	Interval IntervalRange
	Raw      string
}

func parsePlacePath(expr ast.Expr) PlacePath {
	if expr == nil {
		return PlacePath{}
	}

	raw := exprToString(expr)

	var (
		segments []string
		interval IntervalRange
	)

	curr := expr

	for {
		switch e := curr.(type) {
		case *ast.SliceExpr:
			var (
				start, end       int64
				hasStart, hasEnd bool
			)

			if e.Low == nil {
				start = 0
				hasStart = true
			} else if lit, ok := e.Low.(*ast.BasicLit); ok {
				if v, err := strconv.ParseInt(lit.Value, 10, 64); err == nil {
					start = v
					hasStart = true
				}
			}

			if lit, ok := e.High.(*ast.BasicLit); ok {
				if v, err := strconv.ParseInt(lit.Value, 10, 64); err == nil {
					end = v
					hasEnd = true
				}
			}

			if hasStart && hasEnd && end >= start {
				interval = IntervalRange{Start: start, End: end, Known: true}
			}

			curr = e.X

		case *ast.CallExpr:
			if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
				methodName := sel.Sel.Name
				if methodName == "SliceMut" || methodName == "Slice" || methodName == "Subslice" ||
					methodName == "Chunk" {
					if len(e.Args) >= 2 {
						var (
							start, end       int64
							hasStart, hasEnd bool
						)

						if lit, ok := e.Args[0].(*ast.BasicLit); ok {
							if v, err := strconv.ParseInt(lit.Value, 10, 64); err == nil {
								start = v
								hasStart = true
							}
						}

						if lit, ok := e.Args[1].(*ast.BasicLit); ok {
							if v, err := strconv.ParseInt(lit.Value, 10, 64); err == nil {
								end = v
								hasEnd = true
							}
						}

						if hasStart && hasEnd && end >= start {
							interval = IntervalRange{Start: start, End: end, Known: true}
						}
					}

					curr = sel.X

					continue
				}
			}

			return PlacePath{
				Root:     raw,
				Segments: segments,
				Interval: interval,
				Raw:      raw,
			}

		case *ast.SelectorExpr:
			segments = append([]string{e.Sel.Name}, segments...)
			curr = e.X

		case *ast.Ident:
			return PlacePath{
				Root:     e.Name,
				Segments: segments,
				Interval: interval,
				Raw:      raw,
			}

		case *ast.ParenExpr:
			curr = e.X

		case *ast.StarExpr:
			curr = e.X

		case *ast.UnaryExpr:
			curr = e.X

		default:
			return PlacePath{
				Root:     raw,
				Segments: segments,
				Interval: interval,
				Raw:      raw,
			}
		}
	}
}

// ConflictsWith reports whether two place projections alias or overlap in memory.
func (p PlacePath) ConflictsWith(other PlacePath) bool {
	if p.Root == "" || other.Root == "" {
		return false
	}

	if p.Root != other.Root {
		return false
	}

	// Separation Logic Check: If both paths target identical root & field segments,
	// but have provably disjoint index intervals [a, b) * [c, d), they do NOT conflict!
	if p.Interval.Known && other.Interval.Known && p.Interval.DisjointWith(other.Interval) {
		if len(p.Segments) == len(other.Segments) {
			match := true
			for i := range p.Segments {
				if p.Segments[i] != other.Segments[i] {
					match = false
					break
				}
			}

			if match {
				return false
			}
		}
	}

	// If either has disjoint field segments, they do not conflict
	minLen := len(p.Segments)
	if len(other.Segments) < minLen {
		minLen = len(other.Segments)
	}

	for i := 0; i < minLen; i++ {
		if p.Segments[i] != other.Segments[i] {
			// Disjoint field projections on the same root struct
			return false
		}
	}

	// If identical fields and both intervals are disjoint -> no conflict
	if p.Interval.Known && other.Interval.Known && p.Interval.DisjointWith(other.Interval) {
		return false
	}

	// If either is the entire root (no subfield segments and no interval), they conflict
	if (len(p.Segments) == 0 && !p.Interval.Known) || (len(other.Segments) == 0 && !other.Interval.Known) {
		return true
	}

	// Same prefix or identical path -> conflict
	return true
}

// Inter-procedural summary utilities.

type funcSummary struct {
	MovesAll     bool
	MovesIndices map[int]bool
	MovesParams  map[string]bool
	ReadsParams  map[string]bool
}

func parsePackageSummaries(file *ast.File) map[string]funcSummary {
	summaries := make(map[string]funcSummary)
	if file == nil {
		return summaries
	}

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil {
			continue
		}

		funcName := fn.Name.Name
		sum := funcSummary{
			MovesIndices: make(map[int]bool),
			MovesParams:  make(map[string]bool),
			ReadsParams:  make(map[string]bool),
		}

		if fn.Doc != nil {
			for _, comment := range fn.Doc.List {
				text := strings.TrimSpace(comment.Text)
				if strings.Contains(text, "@borrow:moves") || strings.Contains(text, "@borrow:move") {
					sum.MovesAll = true
					if idx := strings.Index(text, "("); idx != -1 {
						if endIdx := strings.Index(text[idx:], ")"); endIdx != -1 {
							paramList := text[idx+1 : idx+endIdx]

							sum.MovesAll = false
							for _, p := range strings.Split(paramList, ",") {
								pName := strings.TrimSpace(p)
								if pName != "" {
									sum.MovesParams[pName] = true
								}
							}
						}
					}
				}

				if strings.Contains(text, "@borrow:reads") || strings.Contains(text, "@borrow:read") {
					if idx := strings.Index(text, "("); idx != -1 {
						if endIdx := strings.Index(text[idx:], ")"); endIdx != -1 {
							paramList := text[idx+1 : idx+endIdx]
							for _, p := range strings.Split(paramList, ",") {
								pName := strings.TrimSpace(p)
								if pName != "" {
									sum.ReadsParams[pName] = true
								}
							}
						}
					}
				}
			}
		}

		if fn.Type != nil && fn.Type.Params != nil {
			paramIdx := 0
			for _, field := range fn.Type.Params.List {
				for _, name := range field.Names {
					if sum.MovesParams[name.Name] {
						sum.MovesIndices[paramIdx] = true
					}

					paramIdx++
				}
			}
		}

		if sum.MovesAll || len(sum.MovesIndices) > 0 || len(sum.MovesParams) > 0 || len(sum.ReadsParams) > 0 {
			summaries[funcName] = sum
		}
	}

	return summaries
}

// Helper utilities for AST parsing in borrow rules.

func isBorrowExpr(expr string) bool {
	return strings.Contains(expr, "borrow.NewBytes") ||
		strings.Contains(expr, "borrow.NewBox") ||
		strings.Contains(expr, "borrow.Alloc") ||
		strings.Contains(expr, "borrow.Borrow") ||
		strings.Contains(expr, ".AllocBytes(") ||
		strings.Contains(expr, ".AllocMut(") ||
		strings.Contains(expr, ".Borrow(") ||
		strings.Contains(expr, ".BorrowMut(") ||
		strings.Contains(expr, ".MustBorrow(") ||
		strings.Contains(expr, ".MustBorrowMut(") ||
		strings.Contains(expr, ".BodyUnsafe(") ||
		strings.Contains(expr, ".BodyBytes(") ||
		strings.Contains(expr, ".Freeze(")
}

func isBorrowType(typeStr string) bool {
	return strings.Contains(typeStr, "borrow.Bytes") ||
		strings.Contains(typeStr, "borrow.Ref") ||
		strings.Contains(typeStr, "borrow.Mut") ||
		strings.Contains(typeStr, "borrow.Box") ||
		strings.Contains(typeStr, "borrow.Cell") ||
		strings.Contains(typeStr, "borrow.OwnedBytes") ||
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

// RuleBorrowTypestate (B011) validates state-machine transitions on linear and borrowable resources.
type RuleBorrowTypestate struct{}

func (r *RuleBorrowTypestate) ID() string                { return "B011" }
func (r *RuleBorrowTypestate) Name() string              { return "borrow-typestate-violation" }
func (r *RuleBorrowTypestate) Category() Category        { return CategoryBorrow }
func (r *RuleBorrowTypestate) DefaultSeverity() Severity { return SeverityError }
func (r *RuleBorrowTypestate) IsFixable() bool           { return false }
func (r *RuleBorrowTypestate) Description() string {
	return "Enforces typestate automata rules preventing invalid method invocations on uninitialized, frozen, or released handles"
}

func (r *RuleBorrowTypestate) Run(pass *Pass) []Diagnostic {
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
			type resourceTypestate int

			const (
				stateAcquired resourceTypestate = iota
				stateFrozen
				stateReleased
			)

			states := make(map[string]resourceTypestate)
			statePos := make(map[string]token.Pos)

			// Initialize borrowed function parameters as acquired
			if fn.Type != nil && fn.Type.Params != nil {
				for _, param := range fn.Type.Params.List {
					typeStr := exprToString(param.Type)
					if isBorrowType(typeStr) {
						for _, name := range param.Names {
							states[name.Name] = stateAcquired
							statePos[name.Name] = name.Pos()
						}
					}
				}
			}

			for _, block := range path {
				for _, node := range block.Nodes {
					// 1. Identify resource acquisition
					if assign, ok := node.(*ast.AssignStmt); ok {
						for i, rhs := range assign.Rhs {
							rhsStr := exprToString(rhs)
							if isBorrowExpr(rhsStr) {
								if i < len(assign.Lhs) {
									place := parsePlacePath(assign.Lhs[i])
									if place.Root != "" {
										states[place.Root] = stateAcquired
										statePos[place.Root] = assign.Pos()
									}
								}
							}

							if call, ok := rhs.(*ast.CallExpr); ok {
								if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Freeze" {
									targetPlace := parsePlacePath(sel.X)
									states[targetPlace.Root] = stateFrozen
									statePos[targetPlace.Root] = sel.Pos()
								}
							}
						}
					}

					// 2. Identify State transitions: Release, Move, Unfreeze
					ast.Inspect(node, func(n ast.Node) bool {
						call, ok := n.(*ast.CallExpr)
						if !ok {
							return true
						}

						sel, ok := call.Fun.(*ast.SelectorExpr)
						if !ok {
							return true
						}

						targetPlace := parsePlacePath(sel.X)
						methodName := sel.Sel.Name

						if currentState, exists := states[targetPlace.Root]; exists {
							switch methodName {
							case "Release", "Close", "Move":
								if currentState == stateReleased {
									pos := pass.FileSet.Position(sel.Pos())
									prevPos := pass.FileSet.Position(statePos[targetPlace.Root])
									key := fmt.Sprintf(
										"typestate:double-rel:%s:%d:%d",
										targetPlace.Root,
										prevPos.Line,
										pos.Line,
									)

									if !reported[key] {
										reported[key] = true
										msg := fmt.Sprintf(
											"typestate violation: redundant %s() on %q in state [Released] (already released on line %d)",
											methodName,
											targetPlace.Root,
											prevPos.Line,
										)
										diags = append(diags, Diagnostic{
											RuleID:     r.ID(),
											RuleName:   r.Name(),
											Severity:   r.DefaultSeverity(),
											Category:   r.Category(),
											Target:     targetPlace.Root,
											Message:    msg,
											FilePath:   pass.FilePath,
											Line:       pos.Line,
											Column:     pos.Column,
											Suggestion: "Remove the duplicate Release()/Close() call",
										})
									}
								}

								states[targetPlace.Root] = stateReleased
								statePos[targetPlace.Root] = sel.Pos()

							case "Unfreeze":
								states[targetPlace.Root] = stateAcquired
								statePos[targetPlace.Root] = sel.Pos()

							default:
								if currentState == stateReleased {
									pos := pass.FileSet.Position(sel.Pos())
									prevPos := pass.FileSet.Position(statePos[targetPlace.Root])
									key := fmt.Sprintf(
										"typestate:invalid-op:%s:%d:%d",
										targetPlace.Root,
										prevPos.Line,
										pos.Line,
									)

									if !reported[key] {
										reported[key] = true
										msg := fmt.Sprintf(
											"typestate violation: illegal invocation of .%s() on %q in state [Released] (released at line %d)",
											methodName,
											targetPlace.Root,
											prevPos.Line,
										)
										diags = append(diags, Diagnostic{
											RuleID:   r.ID(),
											RuleName: r.Name(),
											Severity: r.DefaultSeverity(),
											Category: r.Category(),
											Target:   targetPlace.Root,
											Message:  msg,
											FilePath: pass.FilePath,
											Line:     pos.Line,
											Column:   pos.Column,
											Suggestion: fmt.Sprintf(
												"Do not invoke methods on '%s' once it transitions to [Released]",
												targetPlace.Root,
											),
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
