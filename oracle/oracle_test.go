// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package oracle_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/oracle"
)

func TestOracleClientDefaults(t *testing.T) {
	c := oracle.NewClient("")
	if c.BaseURL() != oracle.DefaultBaseURL {
		t.Errorf("expected default base URL %s, got %s", oracle.DefaultBaseURL, c.BaseURL())
	}
}

func TestOracleSupervisorCreation(t *testing.T) {
	c := oracle.NewClient("http://127.0.0.1:64055")

	s := oracle.NewSupervisor(c, "sidecar/server.js")
	if s == nil {
		t.Fatal("expected supervisor instance, got nil")
	}
}
