// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/request"
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
	defer server.Close()

	solver := &MockChallengeSolver{
		solveFunc: func(ctx context.Context, err error, req *http.Request) (*http.Response, error) {
			assert.ErrorIs(t, err, aoni.ErrCloudflareChallenge)

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
		option.WithChallengeSolver(solver),
	)

	type Response struct {
		Success bool `json:"success"`
	}

	res, err := request.GetTo[Response](context.Background(), client, "/")
	assert.NoError(t, err)
	assert.True(t, res.Success)
	assert.Equal(t, 1, solver.solveCount)
	assert.Equal(t, 2, requestCount)
}

func TestChallengeSolver_CustomDetector(t *testing.T) {
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
	defer server.Close()

	customErr := errors.New("custom WAF error")

	detector := func(resp *http.Response) (bool, error) {
		// Custom detection: detect WAF blocks by status code
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

	res, err := request.GetTo[Response](context.Background(), client, "/")
	assert.NoError(t, err)
	assert.True(t, res.Success)
	assert.Equal(t, 1, solver.solveCount)
	assert.Equal(t, 2, requestCount)
}
