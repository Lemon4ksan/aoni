// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package profiles_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/profiles"
	"github.com/lemon4ksan/aoni/profiles/chrome"
	"github.com/lemon4ksan/aoni/profiles/firefox"
)

func TestOSKeyIsMobile(t *testing.T) {
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
		if got := tt.os.IsMobile(); got != tt.expected {
			t.Errorf("OSKey(%d).IsMobile() = %v, want %v", tt.os, got, tt.expected)
		}
	}
}

func TestOSKeyMobile(t *testing.T) {
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
		if got := tt.os.Mobile(); got != tt.expected {
			t.Errorf("OSKey(%d).Mobile() = %q, want %q", tt.os, got, tt.expected)
		}
	}
}

func TestChromeVariantNotNil(t *testing.T) {
	if chrome.Desktop == nil {
		t.Error("chrome.Desktop is nil")
	}

	if chrome.Mobile == nil {
		t.Error("chrome.Mobile is nil")
	}

	if chrome.Desktop.HelloSpec == nil {
		t.Error("chrome.Desktop.HelloSpec is nil")
	}
}

func TestFirefoxVariantNotNil(t *testing.T) {
	if firefox.Desktop == nil {
		t.Error("firefox.Desktop is nil")
	}

	if firefox.Mobile == nil {
		t.Error("firefox.Mobile is nil")
	}
}

func TestChromeBoundary(t *testing.T) {
	b := chrome.Boundary()
	if len(b) == 0 {
		t.Error("Boundary() returned empty string")
	}

	if len(b) < 20 {
		t.Errorf("Boundary() too short: %q", b)
	}
}

func TestFirefoxBoundary(t *testing.T) {
	b := firefox.Boundary()
	if len(b) == 0 {
		t.Error("Boundary() returned empty string")
	}

	if len(b) < 20 {
		t.Errorf("Boundary() too short: %q", b)
	}
}

func TestHeaderCacheEnums(t *testing.T) {
	cache := profiles.NewHeaderCache(
		map[string][]string{"GET": {"accept", "user-agent"}},
		map[string][]string{"GET": {"user-agent", "accept"}},
	)

	desktopEnums := cache.Enums(false)
	if len(desktopEnums) == 0 {
		t.Error("desktop enums is empty")
	}

	mobileEnums := cache.Enums(true)
	if len(mobileEnums) == 0 {
		t.Error("mobile enums is empty")
	}
}

func TestChromeBuildHeaders(t *testing.T) {
	headers := chrome.Desktop.BuildHeaders(profiles.Windows)
	if len(headers) == 0 {
		t.Error("BuildHeaders returned empty")
	}

	foundUA := false
	for _, h := range headers {
		if h.Name == profiles.USER_AGENT {
			foundUA = true

			if h.Value == "" {
				t.Error("User-Agent value is empty")
			}
		}
	}

	if !foundUA {
		t.Error("User-Agent header not found")
	}
}

func TestFirefoxBuildHeaders(t *testing.T) {
	headers := firefox.Desktop.BuildHeaders(profiles.Windows)
	if len(headers) == 0 {
		t.Error("BuildHeaders returned empty")
	}

	foundUA := false
	for _, h := range headers {
		if h.Name == profiles.USER_AGENT {
			foundUA = true

			if h.Value == "" {
				t.Error("User-Agent value is empty")
			}
		}
	}

	if !foundUA {
		t.Error("User-Agent header not found")
	}
}

func TestChromeConfigureH2(t *testing.T) {
	var s profiles.H2Settings
	chrome.Desktop.ConfigureH2(&s)

	if s.HeaderTableSize != 65536 {
		t.Errorf("HeaderTableSize = %d, want 65536", s.HeaderTableSize)
	}

	if s.EnablePush != 0 {
		t.Errorf("EnablePush = %d, want 0", s.EnablePush)
	}

	if s.InitialWindowSize != 6291456 {
		t.Errorf("InitialWindowSize = %d, want 6291456", s.InitialWindowSize)
	}
}

func TestFirefoxConfigureH2(t *testing.T) {
	var s profiles.H2Settings
	firefox.Desktop.ConfigureH2(&s)

	if s.InitialStreamID != 3 {
		t.Errorf("InitialStreamID = %d, want 3", s.InitialStreamID)
	}

	if s.HeaderTableSize != 65536 {
		t.Errorf("HeaderTableSize = %d, want 65536", s.HeaderTableSize)
	}
}

func TestChromeConfigureH3(t *testing.T) {
	var s profiles.H3Settings
	chrome.Desktop.ConfigureH3(&s)

	if s.QpackMaxTableCapacity != 65536 {
		t.Errorf("QpackMaxTableCapacity = %d, want 65536", s.QpackMaxTableCapacity)
	}

	if s.MaxFieldSectionSize != 262144 {
		t.Errorf("MaxFieldSectionSize = %d, want 262144", s.MaxFieldSectionSize)
	}
}

func TestFirefoxConfigureH3(t *testing.T) {
	var s profiles.H3Settings
	firefox.Desktop.ConfigureH3(&s)

	if s.QpackMaxTableCapacity != 65536 {
		t.Errorf("QpackMaxTableCapacity = %d, want 65536", s.QpackMaxTableCapacity)
	}

	if s.EnableConnectProtocol != 1 {
		t.Errorf("EnableConnectProtocol = %d, want 1", s.EnableConnectProtocol)
	}
}
