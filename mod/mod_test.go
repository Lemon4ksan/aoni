// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod_test

import (
	"context"
	"testing"

	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/mod"
)

type dummyUser struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func TestModifierBuilders(t *testing.T) {
	reqAdapter := fast.NewRequest(nil)
	defer reqAdapter.Release()

	mods := []aoni.RequestModifier{
		mod.WithHeader("X-Request-ID", "req-12345"),
		mod.WithBearer("secret-token-xyz"),
		mod.WithQuery("filter=active"),
		mod.WithForceHTTP2(),
	}

	for _, m := range mods {
		if m != nil {
			m(reqAdapter)
		}
	}

	if reqAdapter.Header("X-Request-ID") != "req-12345" {
		t.Errorf("got X-Request-ID %q, want req-12345", reqAdapter.Header("X-Request-ID"))
	}

	if reqAdapter.Header("Authorization") != "Bearer secret-token-xyz" {
		t.Errorf("got Auth %q, want Bearer secret-token-xyz", reqAdapter.Header("Authorization"))
	}

	ctx := reqAdapter.Context()
	reqCfg := aoni.GetRequestConfig(ctx)
	if reqCfg == nil || len(reqCfg.ALPNOverride) == 0 || reqCfg.ALPNOverride[0] != aoni.AlpnH2 {
		t.Errorf("expected ALPN h2 override in request config")
	}
}

func TestWithJSONBody(t *testing.T) {
	reqAdapter := fast.NewRequest(nil)
	defer reqAdapter.Release()

	user := dummyUser{Name: "Alice", Email: "alice@example.com"}
	mod.WithJSONBody(user)(reqAdapter)

	if reqAdapter.Header("Content-Type") != "application/json" {
		t.Errorf("got Content-Type %q, want application/json", reqAdapter.Header("Content-Type"))
	}

	fastReq := reqAdapter.EngineRequest().(*fasthttp.Request)
	if len(fastReq.Body()) == 0 {
		t.Errorf("expected non-empty JSON body")
	}
}

func TestWithForceHTTP3(t *testing.T) {
	ctx := context.Background()
	ctxH3 := aoni.WithContextModifier(ctx, mod.WithForceHTTP3())

	reqCfg := aoni.GetRequestConfig(ctxH3)
	if reqCfg == nil {
		t.Fatalf("expected non-nil RequestConfig in context")
	}

	reqAdapter := fast.NewRequest(nil)
	defer reqAdapter.Release()
	reqAdapter.SetContext(ctxH3)

	for _, m := range reqCfg.Modifiers {
		if m != nil {
			m(reqAdapter)
		}
	}

	activeCfg := aoni.GetRequestConfig(reqAdapter.Context())
	if activeCfg == nil || len(activeCfg.ALPNOverride) == 0 || activeCfg.ALPNOverride[0] != aoni.AlpnH3 {
		t.Errorf("expected ALPN h3 override in request config")
	}
}
