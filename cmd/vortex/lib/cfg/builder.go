// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cfg

import (
	"go/ast"
	"go/token"
)

// New constructs the CFG for the specified function body.
func New(body *ast.BlockStmt, mayReturn func(*ast.CallExpr) bool) *CFG {
	if mayReturn == nil {
		mayReturn = func(*ast.CallExpr) bool { return true }
	}

	b := &builder{
		mayReturn: mayReturn,
		lblocks:   make(map[string]*lblock),
	}

	b.current = b.newBlock(KindBody, body)
	b.stmt(body)

	// Check if entry block is live
	if len(b.blocks) > 0 {
		b.blocks[0].Live = true
	}

	// Compute reachability (Live flags)
	markLive(b.blocks)

	// Compute Predecessors (Preds)
	for _, block := range b.blocks {
		for _, succ := range block.Succs {
			succ.Preds = append(succ.Preds, block)
		}
	}

	// Index blocks
	for i, block := range b.blocks {
		block.Index = int32(i)
	}

	return &CFG{
		Blocks:   b.blocks,
		NoReturn: !hasReachableReturn(b.blocks),
	}
}

type builder struct {
	blocks    []*Block
	mayReturn func(*ast.CallExpr) bool
	current   *Block
	lblocks   map[string]*lblock
	targets   *targets
}

type lblock struct {
	_goto     *Block
	_break    *Block
	_continue *Block
}

type targets struct {
	tail      *targets
	_break    *Block
	_continue *Block
}

func (b *builder) newBlock(kind BlockKind, stmt ast.Stmt) *Block {
	block := &Block{
		Kind: kind,
		Stmt: stmt,
	}

	b.blocks = append(b.blocks, block)

	return block
}

func (b *builder) add(n ast.Node) {
	b.current.Nodes = append(b.current.Nodes, n)
}

func (b *builder) jump(target *Block) {
	b.current.Succs = append(b.current.Succs, target)
	b.current = b.newBlock(KindUnreachable, nil)
}

func (b *builder) ifelse(then, _else *Block) {
	b.current.succs2[0] = then
	b.current.succs2[1] = _else
	b.current.Succs = b.current.succs2[:2]
	b.current = b.newBlock(KindUnreachable, nil)
}

func (b *builder) stmtList(list []ast.Stmt) {
	for _, s := range list {
		b.stmt(s)
	}
}

func (b *builder) stmt(_s ast.Stmt) {
	var label *lblock

start:
	switch s := _s.(type) {
	case *ast.BadStmt,
		*ast.SendStmt,
		*ast.IncDecStmt,
		*ast.GoStmt,
		*ast.EmptyStmt,
		*ast.AssignStmt:
		b.add(s)

	case *ast.DeferStmt:
		b.add(s)
		b.current.returns = true

	case *ast.ExprStmt:
		b.add(s)

		if call, ok := s.X.(*ast.CallExpr); ok && !b.mayReturn(call) {
			b.current = b.newBlock(KindUnreachable, s)
		}

	case *ast.DeclStmt:
		d, ok := s.Decl.(*ast.GenDecl)
		if ok && d.Tok == token.VAR {
			for _, spec := range d.Specs {
				if vspec, ok := spec.(*ast.ValueSpec); ok {
					b.add(vspec)
				}
			}
		}

	case *ast.LabeledStmt:
		label = b.labeledBlock(s.Label, s)

		b.jump(label._goto)
		b.current = label._goto
		_s = s.Stmt

		goto start

	case *ast.ReturnStmt:
		b.current.returns = true
		b.add(s)
		b.current = b.newBlock(KindUnreachable, s)

	case *ast.BranchStmt:
		b.branchStmt(s)

	case *ast.BlockStmt:
		b.stmtList(s.List)

	case *ast.IfStmt:
		if s.Init != nil {
			b.stmt(s.Init)
		}

		then := b.newBlock(KindIfThen, s)
		done := b.newBlock(KindIfDone, s)

		_else := done
		if s.Else != nil {
			_else = b.newBlock(KindIfElse, s)
		}

		b.add(s.Cond)
		b.ifelse(then, _else)
		b.current = then
		b.stmt(s.Body)
		b.jump(done)

		if s.Else != nil {
			b.current = _else
			b.stmt(s.Else)
			b.jump(done)
		}

		b.current = done

	case *ast.ForStmt:
		b.forStmt(s, label)

	case *ast.RangeStmt:
		b.rangeStmt(s, label)

	case *ast.SwitchStmt:
		b.switchStmt(s, label)

	case *ast.TypeSwitchStmt:
		b.typeSwitchStmt(s, label)

	case *ast.SelectStmt:
		b.selectStmt(s, label)
	}
}

func (b *builder) forStmt(s *ast.ForStmt, label *lblock) {
	if s.Init != nil {
		b.stmt(s.Init)
	}

	loop := b.newBlock(KindForLoop, s)
	body := b.newBlock(KindForBody, s)
	post := b.newBlock(KindForPost, s)
	done := b.newBlock(KindForDone, s)

	if label != nil {
		label._break = done
		label._continue = post
	}

	b.jump(loop)

	b.current = loop
	if s.Cond != nil {
		b.add(s.Cond)
		b.ifelse(body, done)
	} else {
		b.jump(body)
	}

	b.targets = &targets{
		tail:      b.targets,
		_break:    done,
		_continue: post,
	}

	b.current = body
	b.stmt(s.Body)
	b.jump(post)

	b.targets = b.targets.tail

	b.current = post
	if s.Post != nil {
		b.stmt(s.Post)
	}

	b.jump(loop)

	b.current = done
}

func (b *builder) rangeStmt(s *ast.RangeStmt, label *lblock) {
	iter := b.newBlock(KindRangeIter, s)
	body := b.newBlock(KindRangeBody, s)
	done := b.newBlock(KindRangeDone, s)

	if label != nil {
		label._break = done
		label._continue = iter
	}

	b.add(s.X)
	b.jump(iter)

	b.current = iter
	if s.Key != nil {
		b.add(s.Key)
	}

	if s.Value != nil {
		b.add(s.Value)
	}

	b.ifelse(body, done)

	b.targets = &targets{
		tail:      b.targets,
		_break:    done,
		_continue: iter,
	}

	b.current = body
	b.stmt(s.Body)
	b.jump(iter)

	b.targets = b.targets.tail

	b.current = done
}

func (b *builder) switchStmt(s *ast.SwitchStmt, label *lblock) {
	if s.Init != nil {
		b.stmt(s.Init)
	}

	if s.Tag != nil {
		b.add(s.Tag)
	}

	done := b.newBlock(KindSwitchDone, s)
	if label != nil {
		label._break = done
	}

	var defaultClause *ast.CaseClause
	for _, clause := range s.Body.List {
		cc := clause.(*ast.CaseClause)
		if cc.List == nil {
			defaultClause = cc
			continue
		}

		body := b.newBlock(KindSwitchCaseClause, cc)
		next := b.newBlock(KindSwitchDone, cc)

		for _, expr := range cc.List {
			b.add(expr)
		}

		b.ifelse(body, next)

		b.current = body
		b.targets = &targets{
			tail:   b.targets,
			_break: done,
		}
		b.stmtList(cc.Body)
		b.targets = b.targets.tail
		b.jump(done)

		b.current = next
	}

	if defaultClause != nil {
		body := b.newBlock(KindSwitchCaseClause, defaultClause)
		b.jump(body)
		b.current = body
		b.targets = &targets{
			tail:   b.targets,
			_break: done,
		}
		b.stmtList(defaultClause.Body)
		b.targets = b.targets.tail
		b.jump(done)
	} else {
		b.jump(done)
	}

	b.current = done
}

func (b *builder) typeSwitchStmt(s *ast.TypeSwitchStmt, label *lblock) {
	if s.Init != nil {
		b.stmt(s.Init)
	}

	if s.Assign != nil {
		b.add(s.Assign)
	}

	done := b.newBlock(KindSwitchDone, s)
	if label != nil {
		label._break = done
	}

	var defaultClause *ast.CaseClause
	for _, clause := range s.Body.List {
		cc := clause.(*ast.CaseClause)
		if cc.List == nil {
			defaultClause = cc
			continue
		}

		body := b.newBlock(KindSwitchCaseClause, cc)
		next := b.newBlock(KindSwitchDone, cc)

		for _, expr := range cc.List {
			b.add(expr)
		}

		b.ifelse(body, next)

		b.current = body
		b.targets = &targets{
			tail:   b.targets,
			_break: done,
		}
		b.stmtList(cc.Body)
		b.targets = b.targets.tail
		b.jump(done)

		b.current = next
	}

	if defaultClause != nil {
		body := b.newBlock(KindSwitchCaseClause, defaultClause)
		b.jump(body)
		b.current = body
		b.targets = &targets{
			tail:   b.targets,
			_break: done,
		}
		b.stmtList(defaultClause.Body)
		b.targets = b.targets.tail
		b.jump(done)
	} else {
		b.jump(done)
	}

	b.current = done
}

func (b *builder) selectStmt(s *ast.SelectStmt, label *lblock) {
	body := b.newBlock(KindSelectBody, s)
	done := b.newBlock(KindSelectDone, s)

	if label != nil {
		label._break = done
	}

	b.jump(body)
	b.current = body

	b.targets = &targets{
		tail:   b.targets,
		_break: done,
	}

	for _, comm := range s.Body.List {
		c := comm.(*ast.CommClause)
		caseBlock := b.newBlock(KindSelectCase, c)

		if c.Comm != nil {
			b.add(c.Comm)
		}

		b.jump(caseBlock)
		b.current = caseBlock
		b.stmtList(c.Body)
		b.jump(done)
	}

	b.targets = b.targets.tail
	b.jump(done)
	b.current = done
}

func (b *builder) branchStmt(s *ast.BranchStmt) {
	switch s.Tok {
	case token.BREAK:
		if s.Label != nil {
			if lb, ok := b.lblocks[s.Label.Name]; ok && lb._break != nil {
				b.jump(lb._break)
				return
			}
		} else if b.targets != nil && b.targets._break != nil {
			b.jump(b.targets._break)
			return
		}

	case token.CONTINUE:
		if s.Label != nil {
			if lb, ok := b.lblocks[s.Label.Name]; ok && lb._continue != nil {
				b.jump(lb._continue)
				return
			}
		} else if b.targets != nil && b.targets._continue != nil {
			b.jump(b.targets._continue)
			return
		}

	case token.GOTO:
		if s.Label != nil {
			lb := b.labeledBlock(s.Label, s)
			b.jump(lb._goto)
			return
		}
	}

	b.current = b.newBlock(KindUnreachable, s)
}

func (b *builder) labeledBlock(label *ast.Ident, stmt ast.Stmt) *lblock {
	lb, ok := b.lblocks[label.Name]
	if !ok {
		lb = &lblock{
			_goto: b.newBlock(KindBody, stmt),
		}
		b.lblocks[label.Name] = lb
	}

	return lb
}

func markLive(blocks []*Block) {
	if len(blocks) == 0 {
		return
	}

	queue := []*Block{blocks[0]}
	blocks[0].Live = true

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		for _, succ := range curr.Succs {
			if !succ.Live {
				succ.Live = true
				queue = append(queue, succ)
			}
		}
	}
}

func hasReachableReturn(blocks []*Block) bool {
	for _, b := range blocks {
		if b.Live && b.IsReturn() {
			return true
		}
	}

	return false
}
