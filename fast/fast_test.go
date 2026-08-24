// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/fast/h1engine"
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

	if err := emptyClient.resolveTargetURL(emptyReq, ""); !errors.Is(err, ErrTargetURLEmpty) {
		t.Fatalf("expected ErrTargetURLEmpty, got %v", err)
	}
}

func TestH2_ALPNResolutionAndHeaderOrdering(t *testing.T) {
	cfg := &aoni.Config{
		Fingerprint: aoni.FingerprintConfig{
			HeaderOrder: []string{":method", ":path", ":authority", ":scheme", "user-agent"},
		},
	}

	fastReq := h1engine.AcquireRequest()
	defer h1engine.ReleaseRequest(fastReq)

	mode := resolveALPNMode(context.Background(), cfg, fastReq, nil)
	assert.Equal(t, aoni.AlpnH2, mode)
}

func TestH3_ForceHTTP3ContextModifier(t *testing.T) {
	ctx := context.Background()
	ctxH3 := aoni.WithContextModifier(ctx, mod.WithForceHTTP3())

	fastReq := h1engine.AcquireRequest()
	defer h1engine.ReleaseRequest(fastReq)

	cfg := &aoni.Config{}
	mode := resolveALPNMode(ctxH3, cfg, fastReq, nil)
	assert.Equal(t, aoni.AlpnH3, mode)
}

func TestResolveALPNMode(t *testing.T) {
	ctx := context.Background()
	cfg := &aoni.Config{}

	if mode := resolveALPNMode(ctx, cfg, &h1engine.Request{}, nil); mode != aoni.AlpnHTTP {
		t.Errorf("got ALPN mode %q, want %q", mode, aoni.AlpnHTTP)
	}

	cfgH2 := &aoni.Config{
		Fingerprint: aoni.FingerprintConfig{
			HeaderOrder: []string{":method", ":path", "user-agent"},
		},
	}

	if mode := resolveALPNMode(ctx, cfgH2, &h1engine.Request{}, nil); mode != aoni.AlpnH2 {
		t.Errorf("got ALPN mode %q, want %q", mode, aoni.AlpnH2)
	}

	ctxH3 := aoni.WithContextModifier(ctx, mod.WithForceHTTP3())
	if mode := resolveALPNMode(ctxH3, cfg, &h1engine.Request{}, nil); mode != aoni.AlpnH3 {
		t.Errorf("got context ALPN mode %q, want %q", mode, aoni.AlpnH3)
	}
}

func TestResponse_JSON_And_String(t *testing.T) {
	t.Parallel()

	fastResp := h1engine.AcquireResponse()
	defer h1engine.ReleaseResponse(fastResp)

	fastResp.SetBodyString(`{"name":"aoni-fast","rps":1870000}`)

	resp := NewResponse(fastResp)
	defer resp.Release()

	assert.Equal(t, `{"name":"aoni-fast","rps":1870000}`, resp.String())

	var data struct {
		Name string `json:"name"`
		RPS  int    `json:"rps"`
	}

	err := resp.JSON(&data)
	assert.NoError(t, err)
	assert.Equal(t, "aoni-fast", data.Name)
	assert.Equal(t, 1870000, data.RPS)

	var dataNoCopy struct {
		Name string `json:"name"`
		RPS  int    `json:"rps"`
	}

	errNoCopy := resp.JSONNoCopy(&dataNoCopy)
	assert.NoError(t, errNoCopy)
	assert.Equal(t, "aoni-fast", dataNoCopy.Name)
	assert.Equal(t, 1870000, dataNoCopy.RPS)
}

func BenchmarkResponse_JSON(b *testing.B) {
	fastResp := h1engine.AcquireResponse()
	defer h1engine.ReleaseResponse(fastResp)

	fastResp.SetBodyString(`{"name":"aoni-fast","rps":1870000,"status":"active"}`)

	resp := NewResponse(fastResp)
	defer resp.Release()

	var data struct {
		Name   string `json:"name"`
		RPS    int    `json:"rps"`
		Status string `json:"status"`
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = resp.JSON(&data)
	}
}

func BenchmarkResponse_JSONNoCopy(b *testing.B) {
	fastResp := h1engine.AcquireResponse()
	defer h1engine.ReleaseResponse(fastResp)

	fastResp.SetBodyString(`{"name":"aoni-fast","rps":1870000,"status":"active"}`)

	resp := NewResponse(fastResp)
	defer resp.Release()

	var data struct {
		Name   string `json:"name"`
		RPS    int    `json:"rps"`
		Status string `json:"status"`
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = resp.JSONNoCopy(&data)
	}
}
