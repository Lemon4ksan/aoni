// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
)

func TestResolveTargetURL(t *testing.T) {
	c := NewClient(option.WithBaseURL("https://api.example.com/v1/"))

	reqAdapter := NewRequest(nil)

	if err := c.resolveTargetURL(reqAdapter, "/users"); err != nil {
		t.Fatalf("resolveTargetURL failed: %v", err)
	}

	if reqAdapter.URL() != "https://api.example.com/v1/users" {
		t.Errorf("got URL %q, want https://api.example.com/v1/users", reqAdapter.URL())
	}

	if err := c.resolveTargetURL(reqAdapter, "https://other.com/data"); err != nil {
		t.Fatalf("resolveTargetURL absolute override failed: %v", err)
	}

	if reqAdapter.URL() != "https://other.com/data" {
		t.Errorf("got URL %q, want https://other.com/data", reqAdapter.URL())
	}

	emptyClient := NewClient()
	emptyReq := NewRequest(nil)

	if err := emptyClient.resolveTargetURL(emptyReq, ""); err != ErrTargetURLEmpty {
		t.Fatalf("expected ErrTargetURLEmpty, got %v", err)
	}
}

func TestResolveALPNMode(t *testing.T) {
	ctx := context.Background()
	cfg := &aoni.Config{}

	if mode := resolveALPNMode(ctx, cfg); mode != aoni.AlpnHTTP {
		t.Errorf("got ALPN mode %q, want %q", mode, aoni.AlpnHTTP)
	}

	cfgH2 := &aoni.Config{
		Fingerprint: aoni.FingerprintConfig{
			HeaderOrder: []string{":method", ":path", "user-agent"},
		},
	}

	if mode := resolveALPNMode(ctx, cfgH2); mode != aoni.AlpnH2 {
		t.Errorf("got ALPN mode %q, want %q", mode, aoni.AlpnH2)
	}

	ctxH3 := aoni.WithContextModifier(ctx, mod.WithForceHTTP3())
	if mode := resolveALPNMode(ctxH3, cfg); mode != aoni.AlpnH3 {
		t.Errorf("got context ALPN mode %q, want %q", mode, aoni.AlpnH3)
	}
}

func TestClientHTTP1Execution(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Aoni-Test") != "active" {
			http.Error(w, "missing test header", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)

		_, _ = fmt.Fprint(w, "fast engine ok")
	}))

	defer ts.Close()

	client := NewClient(option.WithBaseURL(ts.URL))

	resp, err := client.Request(context.Background(), "GET", "/",
		mod.WithHeader("X-Aoni-Test", "active"),
	)
	if err != nil {
		t.Fatalf("client.Request failed: %v", err)
	}

	defer resp.Close()

	if resp.StatusCode() != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode())
	}

	if string(resp.BodyBytes()) != "fast engine ok" {
		t.Fatalf("body mismatch: got %q, want %q", resp.BodyBytes(), "fast engine ok")
	}
}

func TestPooledResponseSafety(t *testing.T) {
	fastReq := NewRequest(nil).FastHTTPRequest()
	fastResp := NewResponse(nil).FastHTTPResponse()

	pooled := NewPooledResponse(fastReq, fastResp)

	if err := pooled.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}

	if err := pooled.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
}
