// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package nik_test

import (
	"context"
	"testing"

	"github.com/lemon4ksan/aoni/netutil/nik"
)

func TestNetworkIsolationKey_Basic(t *testing.T) {
	k := nik.New("https://example.com", "https://sub.example.com")
	if k.IsEmpty() {
		t.Fatal("expected non-empty NIK")
	}

	if k.TopFrameSite() != "https://example.com" {
		t.Fatalf("unexpected topFrameSite: %s", k.TopFrameSite())
	}

	if k.FrameSite() != "https://sub.example.com" {
		t.Fatalf("unexpected frameSite: %s", k.FrameSite())
	}

	if !k.IsCrossSite() {
		t.Fatal("expected cross site to be true")
	}

	if k.KeyString() != "https://example.com|https://sub.example.com" {
		t.Fatalf("unexpected KeyString: %s", k.KeyString())
	}
}

func TestNetworkIsolationKey_SameSite(t *testing.T) {
	k := nik.NewSameSite("example.com")
	if k.IsEmpty() {
		t.Fatal("expected non-empty NIK")
	}

	if k.IsCrossSite() {
		t.Fatal("expected same-site to be false for cross-site")
	}

	if k.KeyString() != "https://example.com" {
		t.Fatalf("unexpected KeyString: %s", k.KeyString())
	}
}

func TestNetworkIsolationKey_Transient(t *testing.T) {
	t1 := nik.NewTransient()
	t2 := nik.NewTransient()

	if !t1.IsTransient() || !t2.IsTransient() {
		t.Fatal("expected transient keys")
	}

	if t1.KeyString() == t2.KeyString() {
		t.Fatal("expected unique transient IDs")
	}
}

func TestNetworkIsolationKey_Context(t *testing.T) {
	ctx := context.Background()

	if _, ok := nik.FromContext(ctx); ok {
		t.Fatal("expected no NIK in background context")
	}

	key := nik.New("https://site.org", "")
	ctxWithKey := nik.WithNIK(ctx, key)

	extracted, ok := nik.FromContext(ctxWithKey)
	if !ok {
		t.Fatal("expected NIK in context")
	}

	if extracted.TopFrameSite() != "https://site.org" {
		t.Fatalf("unexpected extracted top site: %s", extracted.TopFrameSite())
	}
}
