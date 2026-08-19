// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package js_test

import (
	"strings"
	"testing"

	"github.com/lemon4ksan/aoni/ast/js"
)

func TestFluentJSBuilder(t *testing.T) {
	prog := js.NewProgram(
		js.Require("http", "path", "fs", "os"),
		js.RequireFrom("playwright", "chromium"),
		js.Const("PORT", 64055),
		js.Let("browserContext", nil),

		js.Fn("executeFlow").Async().Args("flowName", "content").Body(
			js.Await("initBrowser"),
			js.If("!activePage").Then(
				js.Throw("Error", "browser session not ready"),
			).Else(
				js.Await("dismissDialogs", "activePage"),
			),
			js.Try(
				js.Const("input", js.Await("activePage.locator", "textarea")),
				js.Await("humanType", "activePage", "content"),
			).Catch("err",
				js.Call("console.warn", "err.message"),
			),
			js.Return("capturedTokensPool.pop()"),
		),
	)

	code, err := js.Format(prog)
	if err != nil {
		t.Fatalf("Format failed: %v", err)
	}

	if !strings.Contains(code, "const http = require('http');") {
		t.Errorf("expected require statement, got:\n%s", code)
	}

	if !strings.Contains(code, "const { chromium } = require('playwright');") {
		t.Errorf("expected requireFrom statement, got:\n%s", code)
	}

	if !strings.Contains(code, "async function executeFlow(flowName, content) {") {
		t.Errorf("expected async function declaration, got:\n%s", code)
	}

	if !strings.Contains(code, "await initBrowser();") {
		t.Errorf("expected await statement, got:\n%s", code)
	}

	if !strings.Contains(code, "throw new Error(\"browser session not ready\");") {
		t.Errorf("expected throw statement, got:\n%s", code)
	}

	if !strings.Contains(code, "} catch (err) {") {
		t.Errorf("expected catch block, got:\n%s", code)
	}

	if !strings.Contains(code, "return capturedTokensPool.pop();") {
		t.Errorf("expected return statement, got:\n%s", code)
	}
}
