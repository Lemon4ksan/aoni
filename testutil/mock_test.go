// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testutil_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/request"
	"github.com/lemon4ksan/aoni/testutil"
)

type userResponse struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func TestMockEngine(t *testing.T) {
	t.Parallel()

	mock := testutil.NewMockEngine()
	mock.On("GET", "/api/user/1").
		Reply(http.StatusOK).
		Header("X-Mock", "true").
		JSON(userResponse{ID: 1, Name: "Steve"})

	mock.On("POST", "/api/user").
		Reply(http.StatusCreated).
		JSON(userResponse{ID: 2, Name: "Woz"})

	client := aoni.NewClient(mock)

	t.Run("GetTo_Mock", func(t *testing.T) {
		t.Parallel()

		user, err := request.GetTo[userResponse](context.Background(), client, "/api/user/1")
		require.NoError(t, err)
		require.NotNil(t, user)
		assert.Equal(t, 1, user.ID)
		assert.Equal(t, "Steve", user.Name)
	})

	t.Run("PostTo_Mock", func(t *testing.T) {
		t.Parallel()

		created, err := request.PostTo[userResponse](
			context.Background(),
			client,
			"/api/user",
			userResponse{Name: "Woz"},
		)
		require.NoError(t, err)
		require.NotNil(t, created)
		assert.Equal(t, 2, created.ID)
		assert.Equal(t, "Woz", created.Name)
	})

	t.Run("Unmatched_Route", func(t *testing.T) {
		t.Parallel()

		_, err := request.GetTo[userResponse](context.Background(), client, "/unmatched")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected request")
	})
}
