// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gen_test

import (
	"strings"
	"testing"

	"github.com/lemon4ksan/aoni/internal/codegen/oracle/gen"
	"github.com/lemon4ksan/aoni/internal/codegen/oracle/spec"
)

func TestCodegenOracleCompilation(t *testing.T) {
	s := &spec.OracleSpec{
		Name:      "aistudio",
		Port:      64055,
		TargetURL: "https://aistudio.google.com/prompts/new_chat?model=gemini-3.7-flash",
		Browser: spec.BrowserConfig{
			Headless:   true,
			AutoDetect: true,
			DismissSelectors: []string{
				"button:has-text('Get started')",
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

	jsBytes, err := gen.GenerateJS(s)
	if err != nil {
		t.Fatalf("GenerateJS failed: %v", err)
	}

	jsStr := string(jsBytes)
	if !strings.Contains(jsStr, "aistudio") {
		t.Errorf("expected generated JS to contain oracle name 'aistudio'")
	}

	goBytes, err := gen.GenerateGo(s, "oracle")
	if err != nil {
		t.Fatalf("GenerateGo failed: %v", err)
	}

	goStr := string(goBytes)
	if !strings.Contains(goStr, "AistudioAPI") {
		t.Errorf("expected generated Go service 'AistudioAPI', got:\n%s", goStr)
	}
}
