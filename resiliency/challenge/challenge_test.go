// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package challenge_test

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/request"
	"github.com/lemon4ksan/aoni/resiliency/challenge"
)

type MockChallengeSolver struct {
	solveCount int
	solveFunc  func(ctx context.Context, err error, req *http.Request) (*http.Response, error)
}

func (m *MockChallengeSolver) Solve(ctx context.Context, err error, req *http.Request) (*http.Response, error) {
	m.solveCount++

	return m.solveFunc(ctx, err, req)
}

func TestChallengeSolver_BypassesChallenge(t *testing.T) {
	t.Parallel()

	requestCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("<html><body>cf-challenge error cloudflare ray id 12345</body></html>"))

			return
		}

		assert.Equal(t, "solved-cookie-val", r.Header.Get("X-Solved-Cookie"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success": true}`))
	}))
	t.Cleanup(server.Close)

	solver := &MockChallengeSolver{
		solveFunc: func(ctx context.Context, err error, req *http.Request) (*http.Response, error) {
			assert.ErrorIs(t, err, challenge.ErrCloudflareDetected)

			client := &http.Client{}

			retryReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL.String(), nil)
			if err != nil {
				return nil, err
			}

			maps.Copy(retryReq.Header, req.Header)
			retryReq.Header.Set("X-Solved-Cookie", "solved-cookie-val")

			return client.Do(retryReq)
		},
	}

	client := aoni.NewClient(nil,
		option.WithBaseURL(server.URL),
		option.WithChallengeDetector(challenge.DefaultDetector),
		option.WithChallengeSolver(solver),
	)

	type Response struct {
		Success bool `json:"success"`
	}

	res, err := request.GetTo[Response](t.Context(), client, "/")
	require.NoError(t, err)
	assert.True(t, res.Success)
	assert.Equal(t, 1, solver.solveCount)
	assert.Equal(t, 2, requestCount)
}

func TestChallengeSolver_CustomDetector(t *testing.T) {
	t.Parallel()

	requestCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error": "custom_waf_block"}`))

			return
		}

		assert.Equal(t, "solved-val", r.Header.Get("X-Solved"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success": true}`))
	}))
	t.Cleanup(server.Close)

	customErr := errors.New("custom WAF error")

	detector := func(resp *http.Response) (bool, error) {
		if resp.StatusCode == http.StatusForbidden {
			return true, customErr
		}

		return false, nil
	}

	solver := &MockChallengeSolver{
		solveFunc: func(ctx context.Context, err error, req *http.Request) (*http.Response, error) {
			assert.ErrorIs(t, err, customErr)

			client := &http.Client{}

			retryReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL.String(), nil)
			if err != nil {
				return nil, err
			}

			maps.Copy(retryReq.Header, req.Header)
			retryReq.Header.Set("X-Solved", "solved-val")

			return client.Do(retryReq)
		},
	}

	client := aoni.NewClient(nil,
		option.WithBaseURL(server.URL),
		option.WithChallengeDetector(detector),
		option.WithChallengeSolver(solver),
	)

	type Response struct {
		Success bool `json:"success"`
	}

	res, err := request.GetTo[Response](t.Context(), client, "/")
	require.NoError(t, err)
	assert.True(t, res.Success)
	assert.Equal(t, 1, solver.solveCount)
	assert.Equal(t, 2, requestCount)
}

func TestChallengeDetector_NoChallenge(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "ok"}`))
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(nil, option.WithBaseURL(server.URL))

	resp, err := client.Request(t.Context(), http.MethodGet, "/")
	require.NoError(t, err)

	defer aoni.CloseResponse(resp)

	detected, err := challenge.DetectCloudflareChallenge(resp)
	assert.NoError(t, err)
	assert.False(t, detected)
}

func TestChallengeSolver_SolverError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<html><body>cf-challenge error cloudflare ray id 123</body></html>"))
	}))
	t.Cleanup(server.Close)

	solverErr := errors.New("failed to solve captcha")
	solver := &MockChallengeSolver{
		solveFunc: func(_ context.Context, _ error, _ *http.Request) (*http.Response, error) {
			return nil, solverErr
		},
	}

	client := aoni.NewClient(nil,
		option.WithBaseURL(server.URL),
		option.WithChallengeDetector(challenge.DefaultDetector),
		option.WithChallengeSolver(solver),
	)

	_, err := client.Request(t.Context(), http.MethodGet, "/")
	require.Error(t, err)
	assert.ErrorIs(t, err, solverErr)
}

func TestChallengePipeline_Cascading(t *testing.T) {
	t.Parallel()

	detector1 := func(resp *http.Response) (bool, error) {
		return resp != nil && resp.StatusCode == 403, nil
	}

	solvedResp1 := &http.Response{StatusCode: 200}
	solver1 := &MockChallengeSolver{
		solveFunc: func(_ context.Context, _ error, _ *http.Request) (*http.Response, error) {
			return solvedResp1, nil
		},
	}

	pipeline := challenge.NewPipeline(challenge.ChallengePair{
		Detector: detector1,
		Solver:   solver1,
	})

	req, _ := http.NewRequestWithContext(t.Context(), "GET", "http://test", nil)
	resp403 := &http.Response{StatusCode: 403}

	solved, finalResp, err := pipeline.SolveCascading(req, resp403)
	require.NoError(t, err)
	assert.True(t, solved)
	assert.Equal(t, 200, finalResp.StatusCode)
}
