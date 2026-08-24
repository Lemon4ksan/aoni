// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package cfg constructs an optimized, lightweight Control Flow Graph (CFG) for Go AST.
package cfg

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
)

// CFG represents the control-flow graph of a single function or block.
type CFG struct {
	Blocks   []*Block // blocks[0] is entry; order otherwise topological/defined
	NoReturn bool     // true if the function lacks a reachable return statement
}

// Entry returns the entry basic block of the CFG.
func (g *CFG) Entry() *Block {
	if g == nil || len(g.Blocks) == 0 {
		return nil
	}

	return g.Blocks[0]
}

// ReturnBlocks returns all reachable basic blocks that terminate in a return statement.
func (g *CFG) ReturnBlocks() []*Block {
	if g == nil {
		return nil
	}

	var ret []*Block

	for _, b := range g.Blocks {
		if b.Live && b.IsReturn() {
			ret = append(ret, b)
		}
	}

	return ret
}

// Block represents a basic block: a sequence of statements and expressions evaluated sequentially.
type Block struct {
	Nodes []ast.Node // statements, expressions, or declarations
	Succs []*Block   // successor blocks
	Preds []*Block   // predecessor blocks
	Index int32      // index within CFG.Blocks
	Live  bool       // block is reachable from entry
	Kind  BlockKind  // semantic block purpose
	Stmt  ast.Stmt   // statement that generated this block

	returns bool
	succs2  [2]*Block
}

// IsReturn reports whether this block terminates with a return statement.
func (b *Block) IsReturn() bool {
	if b == nil {
		return false
	}

	if b.returns {
		return true
	}

	if len(b.Nodes) > 0 {
		if _, ok := b.Nodes[len(b.Nodes)-1].(*ast.ReturnStmt); ok {
			return true
		}
	}

	return false
}

// BlockKind identifies the role and origin of a basic block.
type BlockKind uint8

const (
	KindInvalid BlockKind = iota
	KindUnreachable
	KindBody
	KindForBody
	KindForDone
	KindForLoop
	KindForPost
	KindIfDone
	KindIfElse
	KindIfThen
	KindRangeBody
	KindRangeDone
	KindRangeIter
	KindSelectBody
	KindSelectCase
	KindSelectDone
	KindSwitchCaseClause
	KindSwitchDone
)

var blockKindNames = [...]string{
	KindInvalid:          "Invalid",
	KindUnreachable:      "Unreachable",
	KindBody:             "Body",
	KindForBody:          "ForBody",
	KindForDone:          "ForDone",
	KindForLoop:          "ForLoop",
	KindForPost:          "ForPost",
	KindIfDone:           "IfDone",
	KindIfElse:           "IfElse",
	KindIfThen:           "IfThen",
	KindRangeBody:        "RangeBody",
	KindRangeDone:        "RangeDone",
	KindRangeIter:        "RangeIter",
	KindSelectBody:       "SelectBody",
	KindSelectCase:       "SelectCase",
	KindSelectDone:       "SelectDone",
	KindSwitchCaseClause: "SwitchCaseClause",
	KindSwitchDone:       "SwitchDone",
}

func (k BlockKind) String() string {
	if int(k) < len(blockKindNames) {
		return blockKindNames[k]
	}

	return fmt.Sprintf("BlockKind(%d)", k)
}

// Format returns a formatted string representation of the CFG for debugging.
func (g *CFG) Format(fset *token.FileSet) string {
	if g == nil {
		return "<nil CFG>"
	}

	var buf bytes.Buffer

	for i, b := range g.Blocks {
		fmt.Fprintf(&buf, ".%d (%s):", i, b.Kind)

		if !b.Live {
			buf.WriteString(" (unreachable)")
		}

		buf.WriteString("\n")

		for _, n := range b.Nodes {
			fmt.Fprintf(&buf, "\t%s\n", formatNode(fset, n))
		}

		if len(b.Succs) > 0 {
			buf.WriteString("\tsuccs:")

			for _, succ := range b.Succs {
				fmt.Fprintf(&buf, " .%d", succ.Index)
			}

			buf.WriteString("\n")
		}

		if len(b.Preds) > 0 {
			buf.WriteString("\tpreds:")

			for _, pred := range b.Preds {
				fmt.Fprintf(&buf, " .%d", pred.Index)
			}

			buf.WriteString("\n")
		}
	}

	return buf.String()
}

func formatNode(fset *token.FileSet, n ast.Node) string {
	var buf bytes.Buffer

	if err := format.Node(&buf, fset, n); err != nil {
		return fmt.Sprintf("%T", n)
	}

	return buf.String()
}
