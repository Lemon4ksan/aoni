// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package option_test

import (
	"testing"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/option"
)

func TestOptionBuilders(t *testing.T) {
	cfg := &aoni.Config{}

	opts := []aoni.ClientOption{
		option.WithBaseURL("https://api.example.com/v1"),
		option.WithTimeout(10 * time.Second),
		option.WithHeader("X-Custom-Header", "AoniValue"),
		option.WithUserAgent("AoniTestAgent/1.0"),
		option.WithInsecureSkipVerify(),
	}

	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}

	if cfg.Defaults.BaseURLString != "https://api.example.com/v1/" {
		t.Errorf("got BaseURL %q, want https://api.example.com/v1/", cfg.Defaults.BaseURLString)
	}

	if cfg.Engine.Timeout != 10*time.Second {
		t.Errorf("got Timeout %v, want 10s", cfg.Engine.Timeout)
	}

	if cfg.Defaults.Headers.Get("X-Custom-Header") != "AoniValue" {
		t.Errorf("got Header %q, want AoniValue", cfg.Defaults.Headers.Get("X-Custom-Header"))
	}

	if cfg.Defaults.Headers.Get("User-Agent") != "AoniTestAgent/1.0" {
		t.Errorf("got User-Agent %q, want AoniTestAgent/1.0", cfg.Defaults.Headers.Get("User-Agent"))
	}

	if !cfg.Engine.InsecureSkipVerify {
		t.Errorf("expected InsecureSkipVerify true, got false")
	}
}

func TestWithHeadersAndBearer(t *testing.T) {
	cfg := &aoni.Config{}

	hdr := map[string]string{
		"Authorization": "Bearer my-secret-token",
		"Accept":        "application/json",
	}

	option.WithHeaders(hdr)(cfg)

	if cfg.Defaults.Headers.Get("Authorization") != "Bearer my-secret-token" {
		t.Errorf("got Auth header %q", cfg.Defaults.Headers.Get("Authorization"))
	}

	if cfg.Defaults.Headers.Get("Accept") != "application/json" {
		t.Errorf("got Accept header %q", cfg.Defaults.Headers.Get("Accept"))
	}
}
