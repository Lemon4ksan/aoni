// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"errors"
	"net/http"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"
)

func TestHTTPStatusCategory(t *testing.T) {
	tests := []struct {
		statusCode int
		expected   HTTPStatusCategory
		str        string
	}{
		{statusCode: 101, expected: CategoryInformational, str: "1xx Informational"},
		{statusCode: 200, expected: CategorySuccess, str: "2xx Success"},
		{statusCode: 301, expected: CategoryRedirection, str: "3xx Redirection"},
		{statusCode: 404, expected: CategoryClientError, str: "4xx Client Error"},
		{statusCode: 500, expected: CategoryServerError, str: "5xx Server Error"},
		{statusCode: 99, expected: CategoryUnknown, str: "Unknown"},
		{statusCode: 600, expected: CategoryUnknown, str: "Unknown"},
	}

	for _, tt := range tests {
		apiErr := &APIError{StatusCode: tt.statusCode}
		assert.Equal(t, tt.expected, apiErr.Category())
		assert.Equal(t, tt.str, tt.expected.String())
	}

	var nilErr *APIError
	assert.Equal(t, CategoryUnknown, nilErr.Category())
}

func TestAsTypedResult(t *testing.T) {
	// Success case
	resSuccess := AsTypedResult("payload", nil)
	require.True(t, resSuccess.IsSuccess())
	val, err := resSuccess.Unwrap()
	assert.Equal(t, "payload", val)
	assert.Nil(t, err)

	// APIError failure case
	apiErr := &APIError{StatusCode: http.StatusNotFound, Body: []byte("not found")}
	resAPIErr := AsTypedResult("", apiErr)
	require.False(t, resAPIErr.IsSuccess())
	_, unwrappedErr := resAPIErr.Unwrap()
	require.NotNil(t, unwrappedErr)
	assert.Equal(t, http.StatusNotFound, unwrappedErr.StatusCode)
	assert.Equal(t, CategoryClientError, unwrappedErr.Category())

	// Generic error fallback case
	plainErr := errors.New("underlying network failure")
	resPlain := AsTypedResult("", plainErr)
	require.False(t, resPlain.IsSuccess())
	_, unwrappedPlainErr := resPlain.Unwrap()
	require.NotNil(t, unwrappedPlainErr)
	assert.Equal(t, http.StatusInternalServerError, unwrappedPlainErr.StatusCode)
	assert.Equal(t, "underlying network failure", unwrappedPlainErr.BodyString())
}
