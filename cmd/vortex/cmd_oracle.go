// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/lemon4ksan/aoni/internal/codegen/oracle/gen"
	"github.com/lemon4ksan/aoni/internal/codegen/oracle/spec"
)

// CmdOracle compiles declarative browser attestation oracle specifications into JS sidecars and Go contracts.
type CmdOracle struct{}

func (c *CmdOracle) Name() string      { return "oracle" }
func (c *CmdOracle) Aliases() []string { return []string{"sidecar", "attest"} }
func (c *CmdOracle) Synopsis() string {
	return "Compile browser attestation oracle sidecars and Go contracts"
}
func (c *CmdOracle) Usage() string { return "vortex oracle [flags] [target-url / name]" }

func (c *CmdOracle) Run(_ context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("oracle", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		jsOut      string
		goOut      string
		pkgName    string
		nameFlag   string
		proxyFlag  string
		poolFlag   int
		sourceFlag string
	)

	StringVar(fs, &jsOut, "js", "j", "sidecar/server.js", "Path to output generated JavaScript sidecar")
	StringVar(fs, &goOut, "go", "g", "pkg/oracle/oracle.gen.go", "Path to output generated Go contract")
	StringVar(fs, &pkgName, "pkg", "p", "oracle", "Go package name for generated contract")
	StringVar(fs, &nameFlag, "name", "n", "oracle", "Name of the oracle service")
	StringVar(fs, &proxyFlag, "proxy", "", "", "Outbound proxy for browser context (e.g. socks5://127.0.0.1:1080)")
	IntVar(fs, &poolFlag, "pool", "", 3, "Number of concurrent isolated browser tabs in page pool")
	StringVar(
		fs,
		&sourceFlag,
		"source",
		"s",
		"request_body",
		"Token extraction source (request_body, response_body, local_storage, global_js, dom_attr)",
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex oracle — Compile Universal Browser Attestation Sidecars & Go Contracts\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex oracle [flags] [target-url]\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(stderr, "\nExamples:\n")
		fmt.Fprintf(stderr, "  vortex oracle -name aistudio https://aistudio.google.com/prompts/new_chat\n")
		fmt.Fprintf(stderr, "  vortex oracle -proxy socks5://127.0.0.1:1080 -pool 4 -source request_body\n")
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	targetURL := "https://aistudio.google.com/prompts/new_chat"
	if fs.NArg() > 0 {
		targetURL = fs.Arg(0)
	}

	oracleSpec := &spec.OracleSpec{
		Name:      nameFlag,
		Port:      64055,
		TargetURL: targetURL,
		Browser: spec.BrowserConfig{
			Headless:   true,
			AutoDetect: true,
			Proxy:      proxyFlag,
			PoolSize:   poolFlag,
			DismissSelectors: []string{
				"button:has-text('Get started')",
				"button:has-text('Accept')",
				"button:has-text('Dismiss')",
			},
		},
		Flows: []spec.FlowSpec{
			{
				Name: "generate_token",
				Steps: []spec.FlowStep{
					{
						Action:   spec.ActionClick,
						Selector: "textarea, [contenteditable='true']",
						Kinetics: true,
					},
					{
						Action:   spec.ActionType,
						Selector: "textarea, [contenteditable='true']",
						Value:    "{content}",
						Kinetics: true,
					},
					{
						Action:   spec.ActionClick,
						Selector: "button:has-text('Run'), button[type='submit']",
						Kinetics: true,
					},
				},
				InputSelector:    "textarea, [contenteditable='true']",
				SubmitSelector:   "button:has-text('Run'), button[type='submit']",
				FallbackShortcut: "Control+Enter",
				HumanKinetics:    true,
				Intercept: spec.InterceptRule{
					Source:         spec.InterceptSource(sourceFlag),
					URLPattern:     "/GenerateContent",
					TokenIndex:     4,
					CaptureCookies: true,
				},
			},
		},
	}

	fmt.Fprintf(stdout, "⚡ Compiling Vortex Oracle [%s] -> %s\n", oracleSpec.Name, oracleSpec.TargetURL)

	// 1. Generate JS Sidecar
	jsBytes, err := gen.GenerateJS(oracleSpec)
	if err != nil {
		return fmt.Errorf("generating JS sidecar: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(jsOut), 0o755); err != nil {
		return fmt.Errorf("creating directory for JS sidecar: %w", err)
	}

	if err := os.WriteFile(jsOut, jsBytes, 0o600); err != nil {
		return fmt.Errorf("writing JS sidecar to %s: %w", jsOut, err)
	}

	fmt.Fprintf(stdout, "✔ Generated JS sidecar: %s (%d bytes)\n", jsOut, len(jsBytes))

	// 2. Generate Go Contract
	goBytes, err := gen.GenerateGo(oracleSpec, pkgName)
	if err != nil {
		return fmt.Errorf("generating Go contract: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(goOut), 0o755); err != nil {
		return fmt.Errorf("creating directory for Go contract: %w", err)
	}

	if err := os.WriteFile(goOut, goBytes, 0o600); err != nil {
		return fmt.Errorf("writing Go contract to %s: %w", goOut, err)
	}

	fmt.Fprintf(stdout, "✔ Generated Go contract: %s (%d bytes)\n", goOut, len(goBytes))
	fmt.Fprintf(stdout, "✨ Vortex Oracle compilation finished successfully!\n")

	return nil
}
