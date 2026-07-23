// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license can be found in the LICENSE file.

package telemetry_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/telemetry"
)

func TestCurlFromRequest_CleanURLAndHeaders(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://example.com/api/users", nil)
	require.NoError(t, err)

	req.Header.Set("Accept", "application/json")

	curl := telemetry.CurlFromRequest(req, nil)
	assert.Contains(t, curl, "curl -H 'Accept: application/json' https://example.com/api/users")
	assert.NotContains(t, curl, "'https://example.com/api/users'")
}

func TestCurlFromRequest_Redaction(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://example.com/api/login", nil)
	require.NoError(t, err)

	req.Header.Set("Authorization", "Bearer secretToken123")
	req.Header.Set("X-Api-Key", "super-secret-key")

	curl := telemetry.CurlFromRequest(req, nil)
	assert.NotContains(t, curl, "secretToken123")
	assert.NotContains(t, curl, "super-secret-key")
	assert.Contains(t, curl, "*****REDACTED*****")
}

func TestCurlFromRequest_Multipart(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://example.com/upload", nil)
	require.NoError(t, err)

	req.Header.Set("Content-Type", "multipart/form-data; boundary=---------------------------12345")

	curl := telemetry.CurlFromRequest(req, []byte("raw binary data"))
	assert.Contains(t, curl, "-F '(multipart/form-data payload)'")
	assert.NotContains(t, curl, "raw binary data")
}

func TestCurlFromRequest_CookieExtraction(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://example.com/dashboard", nil)
	require.NoError(t, err)

	req.AddCookie(&http.Cookie{Name: "session", Value: "abc123xyz"})

	curl := telemetry.CurlFromRequest(req, nil)
	assert.Contains(t, curl, "Cookie:")
	assert.Contains(t, curl, "session=")
}
