// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cookie_test

import (
	"net/http"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/cookie"
)

func TestJSONFileCookieStorage_Persistence(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "cookies.json")

	backend1 := cookie.NewJSONFileStorage(filePath)
	jar1 := cookie.NewProxyIsolatedJar().WithStorageBackend(backend1)

	u, err := url.Parse("https://pyaterochka.ru")
	require.NoError(t, err)

	c := &http.Cookie{
		Name:  "session_token",
		Value: "valid_session_12345",
	}

	jar1.SetCookiesForProxy("http://proxy.test:8080", u, []*http.Cookie{c})

	// Verify the file was created on disk
	assert.FileExists(t, filePath)

	// Instantiate a completely new jar loading from the same file (simulating app restart)
	backend2 := cookie.NewJSONFileStorage(filePath)
	jar2 := cookie.NewProxyIsolatedJar().WithStorageBackend(backend2)

	// Retrieve cookies for the same proxy/domain - should be restored from file
	cookies := jar2.CookiesForProxy("http://proxy.test:8080", u)
	require.Len(t, cookies, 1)
	assert.Equal(t, "session_token", cookies[0].Name)
	assert.Equal(t, "valid_session_12345", cookies[0].Value)
}

func TestJSONFileCookieStorage_EmptyLoad(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "empty_cookies.json")

	backend := cookie.NewJSONFileStorage(filePath)

	cookies, err := backend.Load("nonexistent_proxy_key")
	require.NoError(t, err)
	assert.Nil(t, cookies)
}
