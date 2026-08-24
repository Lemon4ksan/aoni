// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package challenge_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/resiliency/challenge"
)

func TestChallengePipeline_IsolatedCascade(t *testing.T) {
	t.Parallel()

	t.Run("empty_pipeline_returns_unmodified", func(t *testing.T) {
		t.Parallel()

		pipeline := challenge.NewPipeline()
		req, _ := http.NewRequestWithContext(t.Context(), "GET", "http://test", nil)
		resp := &http.Response{StatusCode: 200}

		solved, finalResp, err := pipeline.SolveCascading(req, resp)
		require.NoError(t, err)
		assert.False(t, solved)
		assert.Equal(t, 200, finalResp.StatusCode)
	})

	t.Run("multi_tier_cascading_solvers", func(t *testing.T) {
		t.Parallel()

		detector1 := func(resp *http.Response) (bool, error) {
			return resp != nil && resp.StatusCode == 403, nil
		}
		solver1 := &MockChallengeSolver{
			solveFunc: func(_ context.Context, _ error, _ *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 503, Header: http.Header{"X-Challenge": []string{"datadome"}}}, nil
			},
		}

		detector2 := func(resp *http.Response) (bool, error) {
			return resp != nil && resp.Header.Get("X-Challenge") == "datadome", nil
		}
		solver2 := &MockChallengeSolver{
			solveFunc: func(_ context.Context, _ error, _ *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200}, nil
			},
		}

		pipeline := challenge.NewPipeline().
			Add(detector1, solver1).
			Add(detector2, solver2)

		req, _ := http.NewRequestWithContext(t.Context(), "GET", "http://test", nil)
		resp403 := &http.Response{StatusCode: 403}

		solved, finalResp, err := pipeline.SolveCascading(req, resp403)
		require.NoError(t, err)
		assert.True(t, solved)
		assert.Equal(t, 200, finalResp.StatusCode)
		assert.Equal(t, 1, solver1.solveCount)
		assert.Equal(t, 1, solver2.solveCount)
	})
}
