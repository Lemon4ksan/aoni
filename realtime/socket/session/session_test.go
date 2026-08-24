// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package session_test

import (
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/realtime/socket/session"
)

type userCreds struct {
	UserID uint64
	Token  string
}

func TestSession_State(t *testing.T) {
	t.Parallel()

	st := session.NewState(&userCreds{UserID: 123, Token: "token_1"})
	require.NotNil(t, st)

	current := st.Load()
	require.NotNil(t, current)
	assert.Equal(t, uint64(123), current.UserID)

	st.Store(&userCreds{UserID: 456, Token: "token_2"})
	assert.Equal(t, uint64(456), st.Load().UserID)

	old := st.Swap(&userCreds{UserID: 789, Token: "token_3"})
	assert.Equal(t, uint64(456), old.UserID)
	assert.Equal(t, uint64(789), st.Load().UserID)
}

func TestSession_BasicSession(t *testing.T) {
	t.Parallel()

	bs := &session.BasicSession{}
	assert.Equal(t, uint64(0), bs.SessionID())
	assert.Equal(t, "", bs.Token())

	bs.SetSessionID(999)
	bs.SetToken("secret_token")

	assert.Equal(t, uint64(999), bs.SessionID())
	assert.Equal(t, "secret_token", bs.Token())
}
