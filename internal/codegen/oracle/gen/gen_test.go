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

func TestCodegenUniversalOracleCompilation(t *testing.T) {
	s := &spec.OracleSpec{
		Name:      "cloudflare_turnstile_and_google",
		Port:      64055,
		TargetURL: "https://target.com/login",
		Browser: spec.BrowserConfig{
			Headless:   true,
			AutoDetect: true,
			Proxy:      "socks5://127.0.0.1:1080",
			PoolSize:   4,
			DismissSelectors: []string{
				"button:has-text('Accept')",
			},
		},
		Flows: []spec.FlowSpec{
			{
				Name: "solve_and_capture",
				Steps: []spec.FlowStep{
					{
						Action:   spec.ActionWaitHidden,
						Selector: ".spinner",
						Timeout:  5000,
					},
					{
						Action:   spec.ActionType,
						Selector: "#email",
						Value:    "{content}",
						Kinetics: true,
					},
					{
						Action:   spec.ActionClick,
						Selector: "#submit-btn",
						Kinetics: true,
					},
				},
				Intercept: spec.InterceptRule{
					Source:     spec.SourceResponseBody,
					URLPattern: "/api/auth",
					JSONPath:   "data.session.token",
				},
			},
			{
				Name: "extract_local_storage",
				Intercept: spec.InterceptRule{
					Source: spec.SourceLocalStorage,
					Key:    "jwt_token",
				},
			},
		},
	}

	jsBytes, err := gen.GenerateJS(s)
	if err != nil {
		t.Fatalf("GenerateJS failed: %v", err)
	}

	jsStr := string(jsBytes)
	if !strings.Contains(jsStr, "cloudflare_turnstile_and_google") {
		t.Errorf("expected generated JS to contain oracle name")
	}

	if !strings.Contains(jsStr, "PROXY_URL") {
		t.Errorf("expected generated JS to contain PROXY_URL")
	}

	if !strings.Contains(jsStr, "acquirePage") || !strings.Contains(jsStr, "releasePage") {
		t.Errorf("expected generated JS to contain Page Pool manager")
	}

	if !strings.Contains(jsStr, "localStorage.getItem") {
		t.Errorf("expected generated JS to contain localStorage extraction")
	}

	if !strings.Contains(jsStr, "text/event-stream") {
		t.Errorf("expected generated JS to contain /stream SSE endpoint")
	}

	goBytes, err := gen.GenerateGo(s, "oracle")
	if err != nil {
		t.Fatalf("GenerateGo failed: %v", err)
	}

	goStr := string(goBytes)
	if !strings.Contains(goStr, "Cloudflare_turnstile_and_googleAPI") {
		t.Errorf("expected generated Go service interface, got:\n%s", goStr)
	}
}
