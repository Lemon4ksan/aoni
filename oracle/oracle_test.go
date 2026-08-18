// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package oracle_test

import (
	"strings"
	"testing"

	"github.com/lemon4ksan/aoni/oracle"
	"github.com/lemon4ksan/aoni/oracle/generator"
	"github.com/lemon4ksan/aoni/oracle/spec"
)

func TestOracleASTCompilation(t *testing.T) {
	s := &spec.OracleSpec{
		Name:      "aistudio",
		Port:      64055,
		TargetURL: "https://aistudio.google.com/prompts/new_chat?model=gemini-3.7-flash",
		Browser: spec.BrowserConfig{
			Headless:   true,
			AutoDetect: true,
			DismissSelectors: []string{
				"button:has-text('Get started')",
				"button:has-text('Dismiss')",
			},
		},
		Flows: []spec.FlowSpec{
			{
				Name:             "generate_token",
				InputSelector:    "textarea",
				SubmitSelector:   "button.run-button",
				FallbackShortcut: "Control+Enter",
				HumanKinetics:    true,
				Intercept: spec.InterceptRule{
					URLPattern:     "/GenerateContent",
					TokenIndex:     4,
					CaptureCookies: true,
				},
			},
		},
	}

	jsBytes, err := generator.GenerateJS(s)
	if err != nil {
		t.Fatalf("GenerateJS failed: %v", err)
	}

	jsStr := string(jsBytes)
	if !strings.Contains(jsStr, "aistudio") {
		t.Errorf("expected generated JS to contain oracle name 'aistudio'")
	}

	if !strings.Contains(jsStr, "findSystemBrowser") {
		t.Errorf("expected generated JS to contain browser discovery")
	}

	if !strings.Contains(jsStr, "humanClick") {
		t.Errorf("expected generated JS to contain kinetics helper")
	}

	goBytes, err := generator.GenerateGo(s, "oracle")
	if err != nil {
		t.Fatalf("GenerateGo failed: %v", err)
	}

	goStr := string(goBytes)
	if !strings.Contains(goStr, "AistudioAPI") {
		t.Errorf("expected generated Go service interface 'AistudioAPI', got:\n%s", goStr)
	}

	if !strings.Contains(goStr, "Generate_token") && !strings.Contains(goStr, "GenerateToken") {
		t.Errorf("expected generated flow method in Go client, got:\n%s", goStr)
	}
}

func TestOracleClientDefaults(t *testing.T) {
	c := oracle.NewClient("")
	if c.BaseURL() != oracle.DefaultBaseURL {
		t.Errorf("expected default base URL %s, got %s", oracle.DefaultBaseURL, c.BaseURL())
	}
}
