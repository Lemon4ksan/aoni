// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package profiles_test

import (
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/fingerprint/profiles"
	"github.com/lemon4ksan/aoni/fingerprint/profiles/chrome"
	"github.com/lemon4ksan/aoni/fingerprint/profiles/firefox"
	"github.com/lemon4ksan/aoni/fingerprint/profiles/safari"
)

func TestOSKeyIsMobile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		os       profiles.OSKey
		expected bool
	}{
		{profiles.Windows, false},
		{profiles.MacOS, false},
		{profiles.Linux, false},
		{profiles.Android, true},
		{profiles.IOS, true},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, tt.os.IsMobile())
	}
}

func TestOSKeyMobile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		os       profiles.OSKey
		expected string
	}{
		{profiles.Windows, "?0"},
		{profiles.MacOS, "?0"},
		{profiles.Linux, "?0"},
		{profiles.Android, "?1"},
		{profiles.IOS, "?1"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, tt.os.Mobile())
	}
}

func TestVariantNotNil(t *testing.T) {
	t.Parallel()

	require.NotNil(t, chrome.Desktop)
	require.NotNil(t, chrome.Mobile)
	require.NotNil(t, firefox.Desktop)
	require.NotNil(t, firefox.Mobile)
	require.NotNil(t, safari.Desktop)
}

func TestBoundaries(t *testing.T) {
	t.Parallel()

	t.Run("chrome_boundary", func(t *testing.T) {
		t.Parallel()

		b := chrome.Boundary()
		assert.NotEmpty(t, b)
		assert.GreaterOrEqual(t, len(b), 20)
		assert.Contains(t, b, "----WebKitFormBoundary")
	})

	t.Run("firefox_boundary", func(t *testing.T) {
		t.Parallel()

		b := firefox.Boundary()
		assert.NotEmpty(t, b)
		assert.GreaterOrEqual(t, len(b), 20)
		assert.Contains(t, b, "---------------------------")
	})

	t.Run("safari_boundary", func(t *testing.T) {
		t.Parallel()

		b := safari.Boundary()
		assert.NotEmpty(t, b)
		assert.GreaterOrEqual(t, len(b), 20)
		assert.Contains(t, b, "----WebKitFormBoundary")
	})
}

func TestHeaderCacheEnumsAndSort(t *testing.T) {
	t.Parallel()

	cache := profiles.NewHeaderCache(
		map[string][]string{"GET": {"accept", "user-agent", "cookie"}},
		map[string][]string{"GET": {"cookie", "user-agent", "accept"}},
	)

	desktopEnums := cache.Enums(false)
	require.NotEmpty(t, desktopEnums)

	mobileEnums := cache.Enums(true)
	require.NotEmpty(t, mobileEnums)

	// Test SortByOrder
	inputHeaders := []profiles.HeaderEntry{
		{Name: "cookie", Value: "sess=1"},
		{Name: "accept", Value: "*/*"},
		{Name: "user-agent", Value: "Go"},
	}

	sortedDesktop := cache.SortByOrder(inputHeaders, "GET", false)
	require.Len(t, sortedDesktop, 3)
	assert.Equal(t, "accept", sortedDesktop[0].Name)
	assert.Equal(t, "user-agent", sortedDesktop[1].Name)
	assert.Equal(t, "cookie", sortedDesktop[2].Name)

	sortedMobile := cache.SortByOrder(inputHeaders, "GET", true)
	require.Len(t, sortedMobile, 3)
	assert.Equal(t, "cookie", sortedMobile[0].Name)
	assert.Equal(t, "user-agent", sortedMobile[1].Name)
	assert.Equal(t, "accept", sortedMobile[2].Name)
}

func TestBuildHeaders(t *testing.T) {
	t.Parallel()

	t.Run("chrome_build_headers", func(t *testing.T) {
		t.Parallel()

		headers := chrome.Desktop.BuildHeaders(profiles.Windows)
		require.NotEmpty(t, headers)

		foundUA := false
		for _, h := range headers {
			if h.Name == profiles.USER_AGENT {
				foundUA = true

				assert.NotEmpty(t, h.Value)
			}
		}

		assert.True(t, foundUA, "User-Agent header should be present")
	})

	t.Run("firefox_build_headers", func(t *testing.T) {
		t.Parallel()

		headers := firefox.Desktop.BuildHeaders(profiles.Windows)
		require.NotEmpty(t, headers)

		foundUA := false
		for _, h := range headers {
			if h.Name == profiles.USER_AGENT {
				foundUA = true

				assert.NotEmpty(t, h.Value)
			}
		}

		assert.True(t, foundUA, "User-Agent header should be present")
	})
}

func TestConfigureH2AndH3(t *testing.T) {
	t.Parallel()

	t.Run("chrome_h2_h3", func(t *testing.T) {
		t.Parallel()

		var h2s profiles.H2Settings
		chrome.Desktop.ConfigureH2(&h2s)
		assert.Equal(t, uint32(65536), h2s.HeaderTableSize)
		assert.Equal(t, uint32(0), h2s.EnablePush)
		assert.Equal(t, uint32(6291456), h2s.InitialWindowSize)

		var h3s profiles.H3Settings
		chrome.Desktop.ConfigureH3(&h3s)
		assert.Equal(t, uint64(65536), h3s.QpackMaxTableCapacity)
		assert.Equal(t, uint64(262144), h3s.MaxFieldSectionSize)
	})

	t.Run("firefox_h2_h3", func(t *testing.T) {
		t.Parallel()

		var h2s profiles.H2Settings
		firefox.Desktop.ConfigureH2(&h2s)
		assert.Equal(t, uint32(3), h2s.InitialStreamID)
		assert.Equal(t, uint32(65536), h2s.HeaderTableSize)

		var h3s profiles.H3Settings
		firefox.Desktop.ConfigureH3(&h3s)
		assert.Equal(t, uint64(65536), h3s.QpackMaxTableCapacity)
		assert.Equal(t, uint64(1), h3s.EnableConnectProtocol)
	})
}

func TestInsertHeaders(t *testing.T) {
	t.Parallel()

	headers := make(map[string]string)
	chrome.Desktop.InsertHeaders(headers, "GET")

	assert.Equal(t, "document", headers[profiles.SEC_FETCH_DEST])
	assert.Equal(t, "navigate", headers[profiles.SEC_FETCH_MODE])
	assert.Equal(t, "1", headers[profiles.UPGRADE_INSECURE_REQUESTS])
}
