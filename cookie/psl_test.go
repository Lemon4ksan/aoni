// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cookie

import (
	"context"
	"net/http"
	"net/url"
	"testing"
)

// fakeSuffixList treats a fixed set of domains as public suffixes.
type fakeSuffixList map[string]bool

func (f fakeSuffixList) PublicSuffix(domain string) string {
	if f[domain] {
		return domain
	}

	return "com"
}

func (fakeSuffixList) String() string { return "fake" }

func setSupercookie(t *testing.T, jar CookieJar) []*http.Cookie {
	t.Helper()

	u, _ := url.Parse("https://shop.co.uk")
	jar.SetCookies(context.Background(), u, []*http.Cookie{
		{Name: "super", Value: "1", Domain: "co.uk", Path: "/"},
	})

	return jar.Cookies(context.Background(), u)
}

func TestMemoryJar_PublicSuffixList_RejectsSuffixDomain(t *testing.T) {
	jar := NewMemoryJar(WithPublicSuffixList(fakeSuffixList{"co.uk": true}))

	if got := setSupercookie(t, jar); len(got) != 0 {
		t.Fatalf("cookie scoped to a public suffix must be rejected, got %v", got)
	}
}

func TestMemoryJar_NoPublicSuffixList_FallsBackToDomainMatch(t *testing.T) {
	jar := NewMemoryJar()

	// Without a list the core cannot know co.uk is a suffix; DomainMatch alone accepts it.
	if got := setSupercookie(t, jar); len(got) != 1 {
		t.Fatalf("expected cookie accepted without a suffix list, got %v", got)
	}
}

func TestMemoryJar_SetPublicSuffixList_NilDisablesChecks(t *testing.T) {
	jar := NewMemoryJar(WithPublicSuffixList(fakeSuffixList{"co.uk": true}))
	jar.SetPublicSuffixList(nil)

	if got := setSupercookie(t, jar); len(got) != 1 {
		t.Fatalf("expected nil list to disable suffix checks, got %v", got)
	}
}

func TestProxyIsolatedJar_PublicSuffixListPropagates(t *testing.T) {
	jar := NewProxyIsolatedJar().WithPublicSuffixList(fakeSuffixList{"co.uk": true})

	if got := setSupercookie(t, jar); len(got) != 0 {
		t.Fatalf("per-proxy jar must inherit the suffix list, got %v", got)
	}
}

func TestApplyPublicSuffixList(t *testing.T) {
	list := fakeSuffixList{"co.uk": true}

	if !ApplyPublicSuffixList(NewMemoryJar(), list) {
		t.Error("MemoryJar should accept a suffix list")
	}

	if !ApplyPublicSuffixList(NewProxyIsolatedJar(), list) {
		t.Error("ProxyIsolatedJar should accept a suffix list")
	}

	if ApplyPublicSuffixList(struct{}{}, list) {
		t.Error("unsupported jar types must report false")
	}
}
