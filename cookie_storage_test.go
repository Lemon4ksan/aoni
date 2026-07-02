// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni"
)

func TestJSONFileCookieStorage_Persistence(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "aoni-cookie-test-*")
	require.NoError(t, err)

	defer os.RemoveAll(tempDir)

	filePath := filepath.Join(tempDir, "cookies.json")

	backend1 := aoni.NewJSONFileCookieStorage(filePath)
	jar1 := aoni.NewProxyIsolatedCookieJar().WithStorageBackend(backend1)

	u, err := url.Parse("https://pyaterochka.ru")
	require.NoError(t, err)

	cookie := &http.Cookie{
		Name:  "session_token",
		Value: "valid_session_12345",
	}

	jar1.SetCookiesForProxy("http://proxy.test:8080", u, []*http.Cookie{cookie})

	// Verify the file was created on disk
	assert.FileExists(t, filePath)

	// Instantiate a completely new jar loading from the same file (simulating app restart)
	backend2 := aoni.NewJSONFileCookieStorage(filePath)
	jar2 := aoni.NewProxyIsolatedCookieJar().WithStorageBackend(backend2)

	// Retrieve cookies for the same proxy/domain - should be restored from file
	cookies := jar2.CookiesForProxy("http://proxy.test:8080", u)
	require.Len(t, cookies, 1)
	assert.Equal(t, "session_token", cookies[0].Name)
	assert.Equal(t, "valid_session_12345", cookies[0].Value)
}
