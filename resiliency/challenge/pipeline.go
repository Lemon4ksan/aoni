// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package challenge

import (
	"errors"
	"net/http"
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
	filtered := make([]ChallengePair, 0, len(pairs))
	for _, p := range pairs {
		if p.Detector != nil && p.Solver != nil {
			filtered = append(filtered, p)
		}
	}

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
			if detected {
				ctx := req.Context()

				newResp, solveErr := pair.Solver.Solve(ctx, err, req)
				if solveErr != nil {
					return true, nil, solveErr
				}

				if newResp != nil {
					currentResp = newResp
					wasSolved = true
					matched = true

					break
				}

				return true, nil, errors.New("aoni: challenge detected but solver returned nil response")
			}
		}

		if !matched {
			break
		}
	}

	return wasSolved, currentResp, nil
}
