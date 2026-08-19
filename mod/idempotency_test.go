// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/mod"
)

func TestWithIdempotencyKey(t *testing.T) {
	t.Parallel()

	req := newDummyRequest()
	m := mod.WithIdempotencyKey()
	m.Fn(req)

	key := req.Header(mod.HeaderIdempotencyKey)
	require.NotEmpty(t, key)
	require.Len(t, key, 36)
	require.Equal(t, byte('7'), key[14], "version should be 7")

	// If already set, do not overwrite
	req.SetHeader(mod.HeaderIdempotencyKey, "custom-idempotent-key")
	m.Fn(req)
	require.Equal(t, "custom-idempotent-key", req.Header(mod.HeaderIdempotencyKey))
}

func TestWithRequestID(t *testing.T) {
	t.Parallel()

	req := newDummyRequest()
	m := mod.WithRequestID()
	m.Fn(req)

	reqID := req.Header(mod.HeaderRequestID)
	require.NotEmpty(t, reqID)
	require.Len(t, reqID, 36)
}
