// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cfg

import (
	"go/ast"
	"go/token"
)

// PathVisitor is invoked for each complete execution path in the CFG.
type PathVisitor func(path []*Block)

// WalkPaths traverses all acyclic execution paths from the entry block to all return/exit blocks.
func (g *CFG) WalkPaths(visitor PathVisitor) {
	if g == nil || len(g.Blocks) == 0 || visitor == nil {
		return
	}

	var (
		visited = make(map[*Block]bool)
		current []*Block
		dfs     func(b *Block)
	)

	dfs = func(b *Block) {
		if b == nil || !b.Live || visited[b] {
			return
		}

		visited[b] = true
		current = append(current, b)

		isExit := len(b.Succs) == 0 || b.IsReturn()

		if isExit {
			pathCopy := make([]*Block, len(current))
			copy(pathCopy, current)
			visitor(pathCopy)
		} else {
			for _, succ := range b.Succs {
				dfs(succ)
			}
		}

		current = current[:len(current)-1]
		visited[b] = false
	}

	dfs(g.Blocks[0])
}

// FindLoopBlocks returns all basic blocks that belong to a loop body.
func (g *CFG) FindLoopBlocks() map[*Block]bool {
	loopBlocks := make(map[*Block]bool)
	if g == nil {
		return loopBlocks
	}

	for _, b := range g.Blocks {
		isLoop := b.Kind == KindForBody ||
			b.Kind == KindForLoop ||
			b.Kind == KindRangeBody ||
			b.Kind == KindRangeIter

		if b.Live && isLoop {
			loopBlocks[b] = true
		}
	}

	return loopBlocks
}

// FindStatementPosition returns token position of a node within the CFG.
func FindStatementPosition(fset *token.FileSet, n ast.Node) (int, int) {
	if fset == nil || n == nil {
		return 1, 1
	}

	pos := fset.Position(n.Pos())

	return pos.Line, pos.Column
}
