// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package middleware_test

import (
	"errors"
	"testing"

	"github.com/lemon4ksan/foundation/testing/assert"
	"github.com/lemon4ksan/foundation/testing/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/middleware"
)

func TestIntercept_Basic(t *testing.T) {
	t.Parallel()

	called := false
	mw := middleware.Intercept(func(req aoni.Request, next aoni.RequestDoer) (aoni.Response, error) {
		called = true
		return next.Do(req)
	})

	mockDoer := aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
		return fast.NewResponse(nil), nil
	})

	wrapped := mw(mockDoer)

	req := fast.NewRequest(nil)
	defer req.Release()

	resp, err := wrapped.Do(req)
	require.NoError(t, err)

	defer resp.Close()

	assert.True(t, called)
}

func TestIntercept_ShortCircuit(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("blocked by interceptor")
	mw := middleware.Intercept(func(req aoni.Request, next aoni.RequestDoer) (aoni.Response, error) {
		return nil, expectedErr
	})

	mockCalled := false
	mockDoer := aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
		mockCalled = true
		return fast.NewResponse(nil), nil
	})

	wrapped := mw(mockDoer)

	req := fast.NewRequest(nil)
	defer req.Release()

	resp, err := wrapped.Do(req)
	require.ErrorIs(t, err, expectedErr)
	assert.Nil(t, resp)
	assert.False(t, mockCalled)
}

func TestIntercept_Unwrap(t *testing.T) {
	t.Parallel()

	mockDoer := aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
		return nil, nil
	})

	mw := middleware.Intercept(func(req aoni.Request, next aoni.RequestDoer) (aoni.Response, error) {
		return next.Do(req)
	})

	wrapped := mw(mockDoer)
	unwrapped, ok := aoni.UnwrapAs[aoni.RequestDoer](wrapped)
	assert.True(t, ok)
	assert.NotNil(t, unwrapped)
}
