// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package challenge

import (
	"errors"
	"net/http"

	"github.com/lemon4ksan/foundation/generic"
)

// ChallengePair associates a [Detector] with its corresponding [Solver].
type ChallengePair struct {
	Detector Detector
	Solver   Solver
}

// ChallengePipeline coordinates a cascade solver pipeline.
// It sequentially tests and solves multi-layered WAF/Anti-Bot protections (Cloudflare, DataDome, Akamai, etc.).
type ChallengePipeline struct {
	pairs []ChallengePair
}

// NewPipeline constructs a [ChallengePipeline] from registered detector-solver pairs.
func NewPipeline(pairs ...ChallengePair) *ChallengePipeline {
	filtered := generic.Filter(pairs, func(p ChallengePair) bool {
		return p.Detector != nil && p.Solver != nil
	})

	return &ChallengePipeline{pairs: filtered}
}

// Add registers a new [ChallengePair] into the cascade.
func (p *ChallengePipeline) Add(detector Detector, solver Solver) *ChallengePipeline {
	if detector != nil && solver != nil {
		p.pairs = append(p.pairs, ChallengePair{Detector: detector, Solver: solver})
	}

	return p
}

// SolveCascading executes the challenge pipeline sequentially against an incoming 403/503 response.
func (p *ChallengePipeline) SolveCascading(req *http.Request, resp *http.Response) (bool, *http.Response, error) {
	if resp == nil || len(p.pairs) == 0 {
		return false, resp, nil
	}

	currentResp := resp
	wasSolved := false

	// Cascade up to the number of pairs to prevent infinite loops.
	for round := 0; round < len(p.pairs); round++ {
		matched := false
		for _, pair := range p.pairs {
			detected, err := pair.Detector(currentResp)
			if !detected {
				continue
			}

			ctx := req.Context()
			oldBody := currentResp.Body

			newResp, solveErr := pair.Solver.Solve(ctx, err, req)
			if solveErr != nil {
				if oldBody != nil {
					_ = oldBody.Close()
				}

				return true, nil, solveErr
			}

			if newResp == nil {
				if oldBody != nil {
					_ = oldBody.Close()
				}

				return true, nil, errors.New("aoni: challenge detected but solver returned nil response")
			}

			if oldBody != nil && oldBody != newResp.Body {
				_ = oldBody.Close()
			}

			currentResp = newResp
			wasSolved = true
			matched = true

			break
		}

		if !matched {
			break
		}
	}

	return wasSolved, currentResp, nil
}
