// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package optimizer

import (
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
)

// Optimizer orchestrates compiler optimization passes over the Intermediate Representation (IR).
type Optimizer struct {
	passes []Pass
}

// Pass defines a discrete compiler transformation pass on a ServiceIR.
type Pass func(svc *ir.ServiceIR)

// NewOptimizer creates a new Optimizer configured with default optimization passes.
func NewOptimizer() *Optimizer {
	return &Optimizer{
		passes: []Pass{
			clusterSubRequesters,
			deduplicateHeaders,
			canonicalizeQueryParams,
			estimateStackAllocations,
		},
	}
}

// Optimize runs all registered optimization passes sequentially over root IR in-place.
func (opt *Optimizer) Optimize(root *ir.RootIR) {
	if root == nil {
		return
	}

	for _, svc := range root.Services {
		if svc == nil {
			continue
		}
		for _, pass := range opt.passes {
			pass(svc)
		}
	}
}
