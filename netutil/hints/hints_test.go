// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package hints_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/aoni/netutil/hints"
)

type mockPreconnector struct {
	mu           sync.Mutex
	preconnects  []string
	preresolves  []string
	preconnectCh chan string
	preresolveCh chan string
}

func newMockPreconnector() *mockPreconnector {
	return &mockPreconnector{
		preconnectCh: make(chan string, 10),
		preresolveCh: make(chan string, 10),
	}
}

func (m *mockPreconnector) Preconnect(ctx context.Context, targetURL string) error {
	m.mu.Lock()
	m.preconnects = append(m.preconnects, targetURL)
	m.mu.Unlock()

	m.preconnectCh <- targetURL

	return nil
}

func (m *mockPreconnector) Preresolve(ctx context.Context, host string) error {
	m.mu.Lock()
	m.preresolves = append(m.preresolves, host)
	m.mu.Unlock()

	m.preresolveCh <- host

	return nil
}

func TestParseLinkHeader(t *testing.T) {
	raw := `<https://cdn.example.com/style.css>; rel=preload; as=style, <https://api.example.com>; rel=preconnect; crossorigin`

	links := hints.ParseLinkHeader(raw)
	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}

	if links[0].URI != "https://cdn.example.com/style.css" || links[0].Rel != "preload" || links[0].As != "style" {
		t.Errorf("unexpected link 0: %+v", links[0])
	}

	if links[1].URI != "https://api.example.com" || links[1].Rel != "preconnect" {
		t.Errorf("unexpected link 1: %+v", links[1])
	}
}

func TestProcessEarlyHints(t *testing.T) {
	mock := newMockPreconnector()

	headers := http.Header{}
	headers.Add("Link", "<https://static.cdn.com>; rel=preconnect")
	headers.Add("Link", "<https://dns.example.org>; rel=dns-prefetch")

	hints.ProcessEarlyHints(context.Background(), mock, headers)

	select {
	case url := <-mock.preconnectCh:
		if url != "https://static.cdn.com" {
			t.Errorf("unexpected preconnect URL: %s", url)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for preconnect")
	}

	select {
	case host := <-mock.preresolveCh:
		if host != "dns.example.org" {
			t.Errorf("unexpected preresolve host: %s", host)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for preresolve")
	}
}
