// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cfg_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/cfg"
)

func getFuncCFG(t *testing.T, src string) (*cfg.CFG, *token.FileSet) {
	t.Helper()

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	require.NoError(t, err)
	require.NotEmpty(t, f.Decls)

	fn, ok := f.Decls[0].(*ast.FuncDecl)
	require.True(t, ok, "first decl must be FuncDecl")
	require.NotNil(t, fn.Body, "function must have body")

	g := cfg.New(fn.Body, nil)
	require.NotNil(t, g)

	return g, fset
}

func TestCFG_Sequential(t *testing.T) {
	t.Parallel()

	src := `package test
func Simple() {
	x := 1
	y := x + 2
	_ = y
}`

	g, fset := getFuncCFG(t, src)
	require.NotEmpty(t, g.Blocks)
	require.NotNil(t, g.Entry())
	require.NotEmpty(t, g.Format(fset))
}

func TestCFG_IfElse(t *testing.T) {
	t.Parallel()

	src := `package test
func Branch(c bool) int {
	if c {
		return 1
	} else {
		return 2
	}
}`

	g, _ := getFuncCFG(t, src)
	require.GreaterOrEqual(t, len(g.Blocks), 4)

	returns := g.ReturnBlocks()
	require.Len(t, returns, 2, "must have 2 return blocks")

	pathCount := 0

	g.WalkPaths(func(path []*cfg.Block) {
		pathCount++

		require.NotEmpty(t, path)
	})

	require.Equal(t, 2, pathCount, "must have 2 execution paths")
}

func TestCFG_ForLoop(t *testing.T) {
	t.Parallel()

	src := `package test
func Loop(n int) {
	for i := 0; i < n; i++ {
		println(i)
	}
}`

	g, _ := getFuncCFG(t, src)
	loops := g.FindLoopBlocks()
	require.NotEmpty(t, loops, "must find loop blocks")
}

func TestCFG_SwitchCase(t *testing.T) {
	t.Parallel()

	src := `package test
func Switch(val int) string {
	switch val {
	case 1:
		return "one"
	case 2:
		return "two"
	default:
		return "other"
	}
}`

	g, _ := getFuncCFG(t, src)
	returns := g.ReturnBlocks()
	require.Len(t, returns, 3, "must have 3 return blocks for switch cases")
}
