// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lint_test

import (
	"context"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/lint"
)

func runBorrowRule(t *testing.T, rule lint.Rule, src string) []lint.Diagnostic {
	t.Helper()

	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	require.NoError(t, err)

	pass := &lint.Pass{
		Context:     context.Background(),
		RootIR:      &ir.RootIR{},
		FileSet:     fset,
		ASTFile:     astFile,
		SourceBytes: []byte(src),
		FilePath:    "test.go",
		RootDir:     ".",
		Ignores:     lint.ParseIgnores(fset, astFile),
	}

	return rule.Run(pass)
}

func TestRuleBorrowEscape_Goroutine(t *testing.T) {
	t.Parallel()

	src := `package test
import "time"

func Handle(b borrow.Bytes) {
	go func() {
		time.Sleep(10 * time.Millisecond)
		println(string(b.AsSlice()))
	}()
}`

	rule := &lint.RuleBorrowEscape{}
	diags := runBorrowRule(t, rule, src)
	require.NotEmpty(t, diags, "must detect escape of borrowed handle into goroutine")
	require.Equal(t, "B001", diags[0].RuleID)
}

func TestRuleBorrowEscape_Channel(t *testing.T) {
	t.Parallel()

	src := `package test

func Process(b borrow.Bytes, ch chan borrow.Bytes) {
	ch <- b
}`

	rule := &lint.RuleBorrowEscape{}
	diags := runBorrowRule(t, rule, src)
	require.NotEmpty(t, diags, "must detect escape into channel")
	require.Equal(t, "B001", diags[0].RuleID)
}

func TestRuleBorrowUseAfterRelease_Sequential(t *testing.T) {
	t.Parallel()

	src := `package test

func Work() {
	box := borrow.NewBox(100)
	box.Release()
	println(*box.Get())
}`

	rule := &lint.RuleBorrowUseAfterRelease{}
	diags := runBorrowRule(t, rule, src)
	require.NotEmpty(t, diags, "must detect use after release")
	require.Equal(t, "B002", diags[0].RuleID)
}

func TestRuleBorrowUseAfterRelease_BranchAware(t *testing.T) {
	t.Parallel()

	// Branch A releases box, then uses it -> triggers B002
	srcErr := `package test

func BranchErr(cond bool) {
	box := borrow.NewBox(100)
	if cond {
		box.Release()
		println(*box.Get())
	}
}`

	rule := &lint.RuleBorrowUseAfterRelease{}
	diags := runBorrowRule(t, rule, srcErr)
	require.NotEmpty(t, diags, "must detect use after release in then-branch")
	require.Equal(t, "B002", diags[0].RuleID)
}

func TestRuleBorrowMultipleMutable(t *testing.T) {
	t.Parallel()

	src := `package test

func Modify(cell *borrow.Cell[int]) {
	m1 := cell.BorrowMut()
	m2 := cell.BorrowMut()
	_ = m1
	_ = m2
}`

	rule := &lint.RuleBorrowMultipleMutable{}
	diags := runBorrowRule(t, rule, src)
	require.NotEmpty(t, diags, "must detect multiple mutable borrows")
	require.Equal(t, "B003", diags[0].RuleID)
}

func TestRuleBorrowLinearLeak_CFG(t *testing.T) {
	t.Parallel()

	// Leaks on branch when cond is false
	src := `package test

func LeakBranch(cond bool) int {
	box := borrow.NewBox(42)
	if cond {
		box.Release()
		return 1
	}
	return 0 // leaked on this exit path
}`

	rule := &lint.RuleBorrowLinearLeak{}
	diags := runBorrowRule(t, rule, src)
	require.NotEmpty(t, diags, "must warn on unreleased linear resource on return path")
	require.Equal(t, "B004", diags[0].RuleID)
}

func TestRuleBorrowUnsafeEscape(t *testing.T) {
	t.Parallel()

	src := `package test
import "unsafe"

func Unsafe(b borrow.Bytes) {
	ptr := unsafe.Pointer(&b.AsSlice()[0])
	_ = ptr
}`

	rule := &lint.RuleBorrowUnsafeEscape{}
	diags := runBorrowRule(t, rule, src)
	require.NotEmpty(t, diags, "must detect unsafe pointer conversion")
	require.Equal(t, "B005", diags[0].RuleID)
}

func TestRuleBorrowLoopCarried(t *testing.T) {
	t.Parallel()

	src := `package test

func LoopLeak(items []int) {
	var leaked borrow.Bytes
	for _, item := range items {
		buf := borrow.NewBytes([]byte{byte(item)}, nil)
		leaked = buf
	}
	_ = leaked
}`

	rule := &lint.RuleBorrowLoopCarried{}
	diags := runBorrowRule(t, rule, src)
	require.NotEmpty(t, diags, "must detect loop carried borrow")
	require.Equal(t, "B006", diags[0].RuleID)
}

func TestRuleBorrowReturnEscape(t *testing.T) {
	t.Parallel()

	src := `package test

func SliceEscape(b borrow.Bytes) []byte {
	return b.AsSlice()
}`

	rule := &lint.RuleBorrowReturnEscape{}
	diags := runBorrowRule(t, rule, src)
	require.NotEmpty(t, diags, "must detect return escape of borrowed slice")
	require.Equal(t, "B007", diags[0].RuleID)
}

func TestRuleBorrowAliasInvalidation(t *testing.T) {
	t.Parallel()

	src := `package test

func AliasUse(raw []byte) {
	b := borrow.NewBytes(raw, nil)
	sub := b.AsSlice()[:10]
	b.Release()
	println(sub[0])
}`

	rule := &lint.RuleBorrowAliasInvalidation{}
	diags := runBorrowRule(t, rule, src)
	require.NotEmpty(t, diags, "must detect use of derived sub-slice after parent release")
	require.Equal(t, "B008", diags[0].RuleID)
}

func TestRuleBorrowClosureEscape(t *testing.T) {
	t.Parallel()

	src := `package test

func MakeHandler(b borrow.Bytes) func() {
	return func() {
		println(b.AsSlice())
	}
}`

	rule := &lint.RuleBorrowClosureEscape{}
	diags := runBorrowRule(t, rule, src)
	require.NotEmpty(t, diags, "must detect closure escape of borrowed handle")
	require.Equal(t, "B009", diags[0].RuleID)
}

func TestRuleBorrow_CleanCode(t *testing.T) {
	t.Parallel()

	src := `package test

func Clean(cond bool) int {
	box := borrow.NewBox(42)
	defer box.Release()

	ref := box.Borrow()
	val := *ref.Get()
	return val
}`

	escapeRule := &lint.RuleBorrowEscape{}
	require.Empty(t, runBorrowRule(t, escapeRule, src))

	uafRule := &lint.RuleBorrowUseAfterRelease{}
	require.Empty(t, runBorrowRule(t, uafRule, src))

	leakRule := &lint.RuleBorrowLinearLeak{}
	require.Empty(t, runBorrowRule(t, leakRule, src))
}

func TestRuleBorrowUseAfterRelease_InterproceduralMoves(t *testing.T) {
	t.Parallel()

	src := `package test

// @borrow:moves(buf)
func consumeBuffer(buf borrow.Bytes) {
	buf.Release()
}

func Process(buf borrow.Bytes) {
	consumeBuffer(buf)
	println(string(buf.AsSlice()))
}`

	rule := &lint.RuleBorrowUseAfterRelease{}
	diags := runBorrowRule(t, rule, src)
	require.NotEmpty(t, diags, "must detect use after inter-procedural move")
	require.Equal(t, "B002", diags[0].RuleID)
}

func TestRuleBorrowMultipleMutable_DisjointFields(t *testing.T) {
	t.Parallel()

	// Disjoint field borrowing is allowed (no diagnostic)
	srcAllowed := `package test

type Order struct {
	Header borrow.Cell[string]
	Items  borrow.Cell[[]int]
}

func Process(order *Order) {
	h := order.Header.BorrowMut()
	items := order.Items.BorrowMut()
	_ = h
	_ = items
}`

	rule := &lint.RuleBorrowMultipleMutable{}
	diagsAllowed := runBorrowRule(t, rule, srcAllowed)
	require.Empty(t, diagsAllowed, "disjoint field borrows must be allowed without error")

	// Same field borrowing violates exclusivity (triggers B003)
	srcConflict := `package test

type Order struct {
	Header borrow.Cell[string]
}

func ProcessConflict(order *Order) {
	h1 := order.Header.BorrowMut()
	h2 := order.Header.BorrowMut()
	_ = h1
	_ = h2
}`

	diagsConflict := runBorrowRule(t, rule, srcConflict)
	require.NotEmpty(t, diagsConflict, "must detect multiple mutable borrows on the same struct field")
	require.Equal(t, "B003", diagsConflict[0].RuleID)
}

func TestRuleBorrowMultipleMutable_FreezeUnfreeze(t *testing.T) {
	t.Parallel()

	// Mutation while frozen triggers B003
	srcFrozenMutation := `package test

func HandleFreeze(mutBuf *borrow.Mut[[]byte]) {
	reader := mutBuf.Freeze()
	_ = reader
	mutBuf.Write([]byte("danger"))
}`

	rule := &lint.RuleBorrowMultipleMutable{}
	diags := runBorrowRule(t, rule, srcFrozenMutation)
	require.NotEmpty(t, diags, "must detect mutation of buffer while frozen by active read handle")
	require.Equal(t, "B003", diags[0].RuleID)

	// Clean: Unfreeze restores mutation rights
	srcCleanUnfreeze := `package test

func HandleUnfreeze(mutBuf *borrow.Mut[[]byte]) {
	reader := mutBuf.Freeze()
	_ = reader
	mutBuf.Unfreeze(reader)
	mutBuf.Write([]byte("safe"))
}`

	diagsClean := runBorrowRule(t, rule, srcCleanUnfreeze)
	require.Empty(t, diagsClean, "unfreeze must restore mutation rights without errors")
}

func TestRuleBorrowGlobalEscape(t *testing.T) {
	t.Parallel()

	srcEscape := `package test

var GlobalBuffer []byte

func Process(b borrow.Bytes) {
	GlobalBuffer = b.AsSlice()
}`

	rule := &lint.RuleBorrowGlobalEscape{}
	diags := runBorrowRule(t, rule, srcEscape)
	require.NotEmpty(t, diags, "must detect escape into package-level variable")
	require.Equal(t, "B010", diags[0].RuleID)

	srcClean := `package test

var GlobalBuffer []byte

func ProcessClean(b borrow.Bytes) {
	local := b.AsSlice()
	println(len(local))
}`

	diagsClean := runBorrowRule(t, rule, srcClean)
	require.Empty(t, diagsClean, "local use of borrowed slice must not trigger B010")
}

