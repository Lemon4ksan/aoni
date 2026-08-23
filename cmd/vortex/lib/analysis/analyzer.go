// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package analysis

import (
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
)

// Analyzer checks the semantic validity of a parsed [ir.RootIR] using the default pipeline.
type Analyzer struct {
	engine *Engine
}

// NewAnalyzer creates a new Analyzer instance configured with standard semantic rules.
func NewAnalyzer() *Analyzer {
	return &Analyzer{
		engine: DefaultEngine(),
	}
}

// NewCustomAnalyzer creates an Analyzer instance wrapping a custom [Engine] configuration.
func NewCustomAnalyzer(engine *Engine) *Analyzer {
	if engine == nil {
		engine = DefaultEngine()
	}

	return &Analyzer{engine: engine}
}

// Analyze validates the RootIR and returns a slice of diagnostics.
// If any diagnostic has SeverityError, the IR is invalid.
func (a *Analyzer) Analyze(root *ir.RootIR) []Diagnostic {
	if a.engine == nil {
		a.engine = DefaultEngine()
	}

	return a.engine.Run(root)
}
