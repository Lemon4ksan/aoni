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

func TestMemoryJar_SetAndGet(t *testing.T) {
	jar := NewMemoryJar()
	u, _ := url.Parse("https://example.com")

	cookies := []*http.Cookie{
		{Name: "test", Value: "123", Domain: "example.com", Path: "/"},
	}

	jar.SetCookies(context.Background(), u, cookies)

	got := jar.Cookies(context.Background(), u)
	if len(got) != 1 || got[0].Value != "123" {
		t.Errorf("expected cookie test=123, got %v", got)
	}
}

func TestMemoryJar_CHIPS(t *testing.T) {
	jar := NewMemoryJar()
	u, _ := url.Parse("https://tracker.com")

	cookies := []*http.Cookie{
		{Name: "tracker_id", Value: "abc", Domain: "tracker.com", Path: "/", Partitioned: true},
		{Name: "shared_id", Value: "xyz", Domain: "tracker.com", Path: "/", Partitioned: false},
	}

	ctxA := WithPartitionKey(context.Background(), "siteA.com")
	jar.SetCookies(ctxA, u, cookies)

	ctxB := WithPartitionKey(context.Background(), "siteB.com")
	gotB := jar.Cookies(ctxB, u)

	if len(gotB) != 1 || gotB[0].Name != "shared_id" {
		t.Errorf("siteB should only see unpartitioned shared_id, got %v", gotB)
	}

	gotA := jar.Cookies(ctxA, u)
	if len(gotA) != 2 {
		t.Errorf("siteA should see both cookies, got %d", len(gotA))
	}
}
