// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"
)

type sampleUser struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func TestFetchTyped_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"Alice","age":30}`))
	}))
	defer ts.Close()

	res, resp := FetchTyped[sampleUser](context.Background(), ts.URL)

	require.NotNil(t, resp)
	defer resp.Body.Close()

	require.True(t, res.IsSuccess())

	user, err := res.Unwrap()
	assert.Nil(t, err)
	assert.Equal(t, "Alice", user.Name)
	assert.Equal(t, 30, user.Age)
}

func TestFetchTyped_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`resource not found`))
	}))
	defer ts.Close()

	res, resp := FetchTyped[sampleUser](context.Background(), ts.URL)

	require.NotNil(t, resp)
	defer resp.Body.Close()

	require.False(t, res.IsSuccess())

	_, apiErr := res.Unwrap()
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	assert.Equal(t, CategoryClientError, apiErr.Category())
}

func TestScoped(t *testing.T) {
	called := false

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true

		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	status, err := Scoped(nil, func(c *Client) (int, error) {
		resp, reqErr := c.Raw().Get(context.Background(), ts.URL)
		if reqErr != nil {
			return 0, reqErr
		}
		defer resp.Body.Close()

		return resp.StatusCode, nil
	}, WithClientTimeout(5*time.Second))

	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, http.StatusOK, status)
}

func TestDeepCloning_Isolation(t *testing.T) {
	cfg := Config{
		Defaults: ClientDefaults{
			UARotationProfiles: []BrowserProfile{
				{
					UserAgent: "Profile-1",
					ClientHints: map[string]string{
						"sec-ch-ua": `"Chrome";v="120"`,
					},
				},
			},
			Pipeline: PipelineConfig{
				Cache: &CacheConfig{
					DefaultTTL:    10 * time.Minute,
					CookieIndices: []string{"session_id"},
					NoVarySearch: &NoVarySearchConfig{
						VaryByHeaders: []string{"Accept-Language"},
						IgnoreParams:  []string{"utm_source"},
					},
				},
			},
		},
	}

	cloned := cfg.Clone()

	// Mutate original and verify clone remains isolated
	cfg.Defaults.UARotationProfiles[0].ClientHints["sec-ch-ua"] = "MUTATED"
	cfg.Defaults.Pipeline.Cache.CookieIndices[0] = "MUTATED"
	cfg.Defaults.Pipeline.Cache.NoVarySearch.IgnoreParams[0] = "MUTATED"

	assert.Equal(t, `"Chrome";v="120"`, cloned.Defaults.UARotationProfiles[0].ClientHints["sec-ch-ua"])
	assert.Equal(t, "session_id", cloned.Defaults.Pipeline.Cache.CookieIndices[0])
	assert.Equal(t, "utm_source", cloned.Defaults.Pipeline.Cache.NoVarySearch.IgnoreParams[0])
}

func TestFacade_OptionsAndIntoEx(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodOptions:
			w.Header().Set("Allow", "GET, POST, OPTIONS")
			w.WriteHeader(http.StatusNoContent)
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"name":"Bob","age":25}`))
		case http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"name":"Charlie","age":35}`))
		}
	}))
	defer ts.Close()

	ctx := context.Background()

	// 1. Test Options
	resp, err := Options(ctx, ts.URL)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, "GET, POST, OPTIONS", resp.Header.Get("Allow"))

	// 2. Test GetInto
	var getUser sampleUser

	err = GetInto(ctx, ts.URL, &getUser)
	require.NoError(t, err)
	assert.Equal(t, "Bob", getUser.Name)
	assert.Equal(t, 25, getUser.Age)

	// 3. Test GetEx
	exUser, exResp, err := GetEx[sampleUser](ctx, ts.URL)
	require.NoError(t, err)

	if exResp != nil && exResp.Body != nil {
		_ = exResp.Body.Close()
	}

	assert.Equal(t, "Bob", exUser.Name)

	// 4. Test PostInto
	var postUser sampleUser

	err = PostInto(ctx, ts.URL, map[string]string{"dummy": "data"}, &postUser)
	require.NoError(t, err)
	assert.Equal(t, "Charlie", postUser.Name)
	assert.Equal(t, 35, postUser.Age)
}
