// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package challenge

import (
	"errors"
	"net/http"

	"github.com/lemon4ksan/aoni"
)

// ChallengePair associates a [aoni.ChallengeDetector] with its corresponding [aoni.ChallengeSolver].
type ChallengePair struct {
	Detector aoni.ChallengeDetector
	Solver   aoni.ChallengeSolver
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
func (p *ChallengePipeline) Add(detector aoni.ChallengeDetector, solver aoni.ChallengeSolver) *ChallengePipeline {
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
	solvedAny := false

	for _, pair := range p.pairs {
		detected, err := pair.Detector(currentResp)
		if err != nil && !errors.Is(err, ErrCloudflareDetected) {
			return false, currentResp, err
		}

		if detected || errors.Is(err, ErrCloudflareDetected) {
			solvedResp, solveErr := pair.Solver.Solve(req.Context(), err, req)
			if solveErr != nil {
				return false, currentResp, solveErr
			}

			if currentResp != nil && currentResp.Body != nil && currentResp != solvedResp {
				_ = currentResp.Body.Close()
			}

			currentResp = solvedResp
			solvedAny = true
		}
	}

	return solvedAny, currentResp, nil
}
