// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/version"
)

func newTestApp(stdout, stderr *bytes.Buffer) *App {
	commands := []Command{
		&CmdAutoPilot{},
		&CmdStatus{},
		&CmdInit{},
		&CmdConfig{},
		&CmdWork{},
		&CmdGen{},
		&CmdHarness{},
		&CmdMock{},
		&CmdClean{},
		&CmdCheck{},
		&CmdDiff{},
		&CmdReview{},
		&CmdAccept{},
		&CmdCherryPick{},
		&CmdRefactor{},
		&CmdSource{},
		&CmdLog{},
		&CmdTag{},
		&CmdBlame{},
		&CmdOAPI{},
		&CmdProto{},
		&CmdBench{},
		&CmdProf{},
		&CmdCover{},
		&CmdList{},
		&CmdExplain{},
		&CmdExample{},
		&CmdPGO{},
		&CmdCompletion{},
	}

	app := NewApp("vortex", version.Current, "Unified Toolchain", commands...)
	app.Stdout = stdout
	app.Stderr = stderr

	return app
}

func TestApp_HelpAndVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"--help"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Available Commands:")

	stdout.Reset()

	err = app.Run(context.Background(), []string{"--version"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "vortex version "+version.Current)
}

func TestApp_ListAndExplain(t *testing.T) {
	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	// List
	err := app.Run(context.Background(), []string{"list"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "vortex DSL Directives & Pipeline Reference")

	// List JSON
	stdout.Reset()

	err = app.Run(context.Background(), []string{"list", "-json"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), `"name":`)

	// Explain valid
	stdout.Reset()

	err = app.Run(context.Background(), []string{"explain", "status"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Directive: @status")

	// Explain invalid
	stdout.Reset()

	err = app.Run(context.Background(), []string{"explain", "nonexistent_directive_xyz"})
	require.Error(t, err)
}

func TestApp_Example(t *testing.T) {
	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"example", "http"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "package")

	stdout.Reset()

	err = app.Run(context.Background(), []string{"example", "invalid_template_kind"})
	require.Error(t, err)
}

func TestApp_Gen(t *testing.T) {
	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "api.go")
	outFile := filepath.Join(tmpDir, "api.gen.go")

	src := `package testapi

// @aoni:service
// @base_url "https://api.github.com"
type GitHubAPI interface {
	// @get /users/{username}
	GetUser(ctx context.Context, username string) (string, error)
}
`
	require.NoError(t, os.WriteFile(srcFile, []byte(src), 0o600))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"gen", "-file=" + srcFile, "-out=" + outFile})
	require.NoError(t, err)
	require.FileExists(t, outFile)
	require.Contains(t, stdout.String(), "Generated")
}

func TestApp_Check(t *testing.T) {
	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "valid_api.go")

	src := `package testapi

// @aoni:service
// @base_url "https://api.github.com"
type ValidAPI interface {
	// @get /users/{username}
	GetUser(ctx context.Context, username string) (string, error)
}
`
	require.NoError(t, os.WriteFile(srcFile, []byte(src), 0o600))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"check", "-disable=E001,W001", srcFile})
	require.NoError(t, err)
}

func TestApp_Diff(t *testing.T) {
	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "api.go")
	specFile := filepath.Join(tmpDir, "spec.json")

	src := `package testapi

import (
	"context"
	"github.com/lemon4ksan/aoni"
)

// @aoni:service
type API interface {
	// @get "items/{id}"
	GetItem(ctx context.Context, id string, mods ...aoni.RequestModifier) (map[string]any, error)
}
`
	require.NoError(t, os.WriteFile(srcFile, []byte(src), 0o600))

	spec := `{
  "openapi": "3.1.0",
  "info": {"title": "Test", "version": "1.0.0"},
  "paths": {
    "/items/{id}": {
      "get": {
        "operationId": "GetItem",
        "parameters": [{"name": "id", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"200": {"description": "OK"}}
      }
    }
  }
}`
	require.NoError(t, os.WriteFile(specFile, []byte(spec), 0o600))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"diff", specFile, srcFile})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "100% in sync")
}

func TestApp_Log(t *testing.T) {
	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "api.go")

	src := `package testapi

import (
	"context"
	"github.com/lemon4ksan/aoni"
)

// @aoni:service
// @version "v1.4.0"
// @source "test_openapi.json"
type API interface {
	// @since "v1.0.0"
	// @get "items/{id}"
	GetItem(ctx context.Context, id string, mods ...aoni.RequestModifier) (map[string]any, error)

	// @since "v1.4.0"
	// @get "orders"
	GetOrders(ctx context.Context, mods ...aoni.RequestModifier) (map[string]any, error)
}
`
	require.NoError(t, os.WriteFile(srcFile, []byte(src), 0o600))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"log", srcFile})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Vortex API Contract Timeline")
	require.Contains(t, stdout.String(), "v1.4.0")
	require.Contains(t, stdout.String(), "test_openapi.json")

	// JSON format
	stdout.Reset()

	err = app.Run(context.Background(), []string{"log", "-json", srcFile})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), `"version": "v1.4.0"`)
}

func TestApp_OAPI_Merge(t *testing.T) {
	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "api.go")
	specFile := filepath.Join(tmpDir, "spec.json")

	src := `package testapi

import (
	"context"
	"github.com/lemon4ksan/aoni"
)

// @aoni:service
type API interface {
	// @get "items/{id}"
	// @unwrap "data"
	GetItem(ctx context.Context, id string, mods ...aoni.RequestModifier) (map[string]any, error)
}
`
	require.NoError(t, os.WriteFile(srcFile, []byte(src), 0o600))

	spec := `{
  "openapi": "3.1.0",
  "info": {"title": "Test", "version": "v1.5.0"},
  "paths": {
    "/items/{id}": {
      "get": {
        "operationId": "api_v1_items_id_get",
        "parameters": [{"name": "id", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"200": {"description": "OK"}}
      }
    },
    "/new/route": {
      "post": {
        "operationId": "CreateSomething",
        "responses": {"200": {"description": "OK"}}
      }
    }
  }
}`
	require.NoError(t, os.WriteFile(specFile, []byte(spec), 0o600))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"oapi", "import", "-spec=" + specFile, "-out=" + srcFile})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "vortex merge")

	// Read merged file
	content, err := os.ReadFile(srcFile)
	require.NoError(t, err)

	contentStr := string(content)

	// Directives preserved
	require.Contains(t, contentStr, `// @unwrap "data"`)
	// New endpoint added
	require.Contains(t, contentStr, "CreateSomething")
	// No DO NOT EDIT
	require.NotContains(t, contentStr, "DO NOT EDIT")
}

func TestApp_Status(t *testing.T) {
	tempDir := t.TempDir()

	serviceDir := filepath.Join(tempDir, "pkg", "statusdemo")
	require.NoError(t, os.MkdirAll(serviceDir, 0o750))

	apiSrc := `package statusdemo

import (
	"context"
	"github.com/lemon4ksan/aoni"
)

// @aoni:service
type StatusDemoAPI interface {
	// @get "ping"
	Ping(ctx context.Context, mods ...aoni.RequestModifier) (map[string]any, error)
}
`
	require.NoError(t, os.WriteFile(filepath.Join(serviceDir, "api.go"), []byte(apiSrc), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(serviceDir, "api.gen.go"), []byte("package statusdemo\n"), 0o600))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	// Run status with JSON
	err := app.Run(context.Background(), []string{"status", "-json", "-all"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), `"total_methods"`)
}

func TestApp_Init(t *testing.T) {
	tempDir := t.TempDir()

	serviceDir := filepath.Join(tempDir, "pkg", "initdemo")
	require.NoError(t, os.MkdirAll(serviceDir, 0o750))

	apiSrc := `package initdemo

import (
	"context"
	"github.com/lemon4ksan/aoni"
)

// @aoni:service
type InitDemoAPI interface {
	// @get "hello"
	Hello(ctx context.Context, mods ...aoni.RequestModifier) (map[string]any, error)
}
`
	require.NoError(t, os.WriteFile(filepath.Join(serviceDir, "api.go"), []byte(apiSrc), 0o600))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"init", tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Created")
	require.FileExists(t, filepath.Join(tempDir, ".vortex.yml"))
}

func TestApp_UnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"some_bogus_command"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown command")
}

func TestApp_Review_And_Accept_MissingArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	// Review missing arg
	err := app.Run(context.Background(), []string{"review"})
	require.Error(t, err)

	// Accept missing arg
	err = app.Run(context.Background(), []string{"accept"})
	require.Error(t, err)
}

func TestApp_Log_Git_Flag(t *testing.T) {
	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	// Log with --git flag on local file
	err := app.Run(context.Background(), []string{"log", "--git", "-n=3", "main.go"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "⚡ Vortex API Git History")
}

func TestApp_Diff_Against_HEAD(t *testing.T) {
	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	// Diff with --against=HEAD on local file
	err := app.Run(context.Background(), []string{"diff", "--against=HEAD", "main.go"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "⚡ [vortex diff]")
}

func TestApp_AutoPilot_EmptyWorkspace(t *testing.T) {
	tempDir := t.TempDir()
	cwd, err := os.Getwd()
	require.NoError(t, err)

	defer func() { _ = os.Chdir(cwd) }()

	require.NoError(t, os.Chdir(tempDir))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	// Run with no args (default auto-pilot) in empty dir
	err = app.Run(context.Background(), nil)
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "No @aoni contracts or .vortex.yml found")
}

func TestApp_AutoPilot_ActiveWorkspace(t *testing.T) {
	tempDir := t.TempDir()
	cwd, err := os.Getwd()
	require.NoError(t, err)

	defer func() { _ = os.Chdir(cwd) }()

	// Create contract file
	pkgDir := filepath.Join(tempDir, "pkg", "autopilotdemo")
	require.NoError(t, os.MkdirAll(pkgDir, 0o750))

	apiSrc := `package autopilotdemo

import (
	"context"
	"github.com/lemon4ksan/aoni"
)

// @aoni:service
type DemoAPI interface {
	// @get "/hello"
	Hello(ctx context.Context, mods ...aoni.RequestModifier) (map[string]any, error)
}
`
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "api.go"), []byte(apiSrc), 0o600))
	require.NoError(t, os.Chdir(tempDir))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	// Run with no args (default auto-pilot) in active workspace
	err = app.Run(context.Background(), nil)
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "⚡ Vortex Auto-Pilot: Audit & Build Pipeline")
	require.Contains(t, stdout.String(), "Pre-flight Contract Audit")
	require.Contains(t, stdout.String(), "Code Generation & Polyglot Targets")
	require.FileExists(t, filepath.Join(pkgDir, "api.gen.go"))
}

func TestApp_Harness_Command(t *testing.T) {
	tempDir := t.TempDir()
	pkgDir := filepath.Join(tempDir, "pkg", "harnessdemo")
	require.NoError(t, os.MkdirAll(pkgDir, 0o750))

	apiSrc := `package harnessdemo

import (
	"context"
	"github.com/lemon4ksan/aoni"
)

// @aoni:dto casing=snake_case
type UserRequest struct {
	Name string ` + "`" + `json:"name"` + "`" + `
}

// @aoni:service
type UserAPI interface {
	// @post "/v1/users"
	// @bench weight=70
	// @budget client_allocs=0 max_client_time="200ns"
	ListUsers(ctx context.Context, req UserRequest, mods ...aoni.RequestModifier) (map[string]any, error)
}
`
	apiFile := filepath.Join(pkgDir, "api.go")
	require.NoError(t, os.WriteFile(apiFile, []byte(apiSrc), 0o600))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"harness", apiFile})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "✔ Generated Harness")
	require.FileExists(t, filepath.Join(pkgDir, "api_harness.gen.go"))

	// Test gen --harness flag
	stdout.Reset()

	err = app.Run(context.Background(), []string{"gen", "-harness", apiFile})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "✔ Generated")
	require.FileExists(t, filepath.Join(pkgDir, "api.gen.go"))
	require.FileExists(t, filepath.Join(pkgDir, "api_harness.gen.go"))
}

func TestApp_Completion(t *testing.T) {
	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"completion", "bash"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "complete -F _vortex_completions vortex")

	stdout.Reset()

	err = app.Run(context.Background(), []string{"completion", "zsh"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "#compdef vortex")

	stdout.Reset()

	err = app.Run(context.Background(), []string{"completion", "powershell"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Register-ArgumentCompleter")
}

func TestApp_CheckFormats(t *testing.T) {
	tempDir := t.TempDir()
	apiFile := filepath.Join(tempDir, "api.go")
	apiSrc := `package test
// @aoni:service
type API interface {
	// @get "/item"
	GetItem()
}
`
	require.NoError(t, os.WriteFile(apiFile, []byte(apiSrc), 0o600))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	// 1. GitHub format
	err := app.Run(context.Background(), []string{"check", "-format=github", apiFile})
	require.Error(t, err)
	require.Contains(t, stdout.String(), "::error")
	require.Contains(t, stdout.String(), "missing-context")

	// 2. SARIF format
	stdout.Reset()

	err = app.Run(context.Background(), []string{"check", "-format=sarif", apiFile})
	require.Error(t, err)
	require.Contains(t, stdout.String(), "\"$schema\"")
	require.Contains(t, stdout.String(), "sarif-spec")
	require.Contains(t, stdout.String(), "\"missing-context\"")
}

func TestApp_Mock(t *testing.T) {
	tempDir := t.TempDir()
	apiFile := filepath.Join(tempDir, "api.go")
	apiSrc := `package test

import (
	"context"
	"github.com/lemon4ksan/aoni"
)

// @aoni:service
type API interface {
	// @get "/item"
	GetItem(ctx context.Context, id string, mods ...aoni.RequestModifier) (map[string]any, error)
}
`
	require.NoError(t, os.WriteFile(apiFile, []byte(apiSrc), 0o600))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"mock", apiFile})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "✔ Generated Mock Server")

	mockFile := filepath.Join(tempDir, "api_mock.gen.go")
	require.FileExists(t, mockFile)

	content, err := os.ReadFile(mockFile)
	require.NoError(t, err)
	require.Contains(t, string(content), "type APIMockServer struct")
	require.Contains(t, string(content), "NewAPIMockServer(t testing.TB)")
	require.Contains(t, string(content), "OnGetItem")
}

func TestApp_Init_FromOpenAPI(t *testing.T) {
	tempDir := t.TempDir()
	specFile := filepath.Join(tempDir, "swagger.json")
	swaggerJSON := `{
  "swagger": "2.0",
  "info": { "title": "Petstore", "version": "1.0.0" },
  "paths": {
    "/pets/{id}": {
      "get": {
        "operationId": "getPet",
        "parameters": [{ "name": "id", "in": "path", "required": true, "type": "string" }],
        "responses": { "200": { "description": "OK" } }
      }
    }
  }
}`
	require.NoError(t, os.WriteFile(specFile, []byte(swaggerJSON), 0o600))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{
		"init",
		"-dir=" + tempDir,
		"-from-openapi=" + specFile,
		"-pkg=petstore",
		"-service=PetStoreAPI",
	})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Successfully imported OpenAPI contract")

	contractFile := filepath.Join(tempDir, "api.go")
	require.FileExists(t, contractFile)

	content, err := os.ReadFile(contractFile)
	require.NoError(t, err)
	require.Contains(t, string(content), "package petstore")
	require.Contains(t, string(content), "type PetStoreAPI interface")
	require.Contains(t, string(content), "GetPet")

	vortexYml := filepath.Join(tempDir, ".vortex.yml")
	require.FileExists(t, vortexYml)
}

func TestApp_Init_FromAsyncAPI(t *testing.T) {
	tempDir := t.TempDir()
	specFile := filepath.Join(tempDir, "asyncapi.yaml")
	asyncapiYAML := `
asyncapi: 3.1.0
info:
  title: Market Data
  version: 1.0.0
channels:
  marketStream:
    address: market/stream
    messages:
      Ticker:
        $ref: '#/components/messages/Ticker'
operations:
  onTicker:
    action: send
    channel:
      $ref: '#/channels/marketStream'
    messages:
      - $ref: '#/channels/marketStream/messages/Ticker'
components:
  messages:
    Ticker:
      payload:
        type: object
        properties:
          symbol:
            type: string
          price:
            type: number
`
	require.NoError(t, os.WriteFile(specFile, []byte(asyncapiYAML), 0o600))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{
		"init",
		"-dir=" + tempDir,
		"-from-asyncapi=" + specFile,
		"-pkg=market",
		"-service=MarketStreamAPI",
	})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Successfully imported AsyncAPI contract")

	contractFile := filepath.Join(tempDir, "api.go")
	require.FileExists(t, contractFile)

	content, err := os.ReadFile(contractFile)
	require.NoError(t, err)
	require.Contains(t, string(content), "package market")
	require.Contains(t, string(content), "type MarketStreamAPI interface")
	require.Contains(t, string(content), "OnTicker")
	require.Contains(t, string(content), `// @event "onTicker"`)

	vortexYml := filepath.Join(tempDir, ".vortex.yml")
	require.FileExists(t, vortexYml)
}

func TestApp_Init_Filters(t *testing.T) {
	tempDir := t.TempDir()

	// pkg1 (to include)
	pkg1Dir := filepath.Join(tempDir, "pkg", "steam", "market")
	require.NoError(t, os.MkdirAll(pkg1Dir, 0o750))

	src1 := `package market
import (
	"context"
	"github.com/lemon4ksan/aoni"
)
// @aoni:service
type MarketAPI interface {
	// @get "price"
	GetPrice(ctx context.Context, mods ...aoni.RequestModifier) (map[string]any, error)
}
`
	require.NoError(t, os.WriteFile(filepath.Join(pkg1Dir, "api.go"), []byte(src1), 0o600))

	// pkg2 (to exclude)
	pkg2Dir := filepath.Join(tempDir, "pkg", "legacy", "oldapi")
	require.NoError(t, os.MkdirAll(pkg2Dir, 0o750))

	src2 := `package oldapi
import (
	"context"
	"github.com/lemon4ksan/aoni"
)
// @aoni:service
type OldAPI interface {
	// @get "old"
	GetOld(ctx context.Context, mods ...aoni.RequestModifier) (map[string]any, error)
}
`
	require.NoError(t, os.WriteFile(filepath.Join(pkg2Dir, "api.go"), []byte(src2), 0o600))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{
		"init",
		"-dir=" + tempDir,
		"-exclude=pkg/legacy/**",
	})
	require.NoError(t, err)

	vortexYml := filepath.Join(tempDir, ".vortex.yml")
	require.FileExists(t, vortexYml)
	content, err := os.ReadFile(vortexYml)
	require.NoError(t, err)
	require.Contains(t, string(content), "MarketAPI")
	require.NotContains(t, string(content), "OldAPI")
}

func TestApp_Clean(t *testing.T) {
	tempDir := t.TempDir()

	// Create test artifacts
	mockFile := filepath.Join(tempDir, "api_mock.gen.go")
	harnessFile := filepath.Join(tempDir, "api_harness.gen.go")
	profFile := filepath.Join(tempDir, "cpu.prof")
	covFile := filepath.Join(tempDir, "coverage.out")
	genFile := filepath.Join(tempDir, "api.gen.go")
	cacheDir := filepath.Join(tempDir, ".vortex")
	require.NoError(t, os.MkdirAll(cacheDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "cache.db"), []byte("cache data"), 0o600))

	require.NoError(t, os.WriteFile(mockFile, []byte("// mock"), 0o600))
	require.NoError(t, os.WriteFile(harnessFile, []byte("// harness"), 0o600))
	require.NoError(t, os.WriteFile(profFile, []byte("prof data"), 0o600))
	require.NoError(t, os.WriteFile(covFile, []byte("cov data"), 0o600))
	require.NoError(t, os.WriteFile(genFile, []byte("// gen client"), 0o600))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	// Dry run first
	err := app.Run(context.Background(), []string{"clean", "-dry-run", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Dry-run")
	require.FileExists(t, mockFile) // Should not be deleted yet

	// Real clean
	stdout.Reset()

	err = app.Run(context.Background(), []string{"clean", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Successfully removed")

	require.NoFileExists(t, mockFile)
	require.NoFileExists(t, harnessFile)
	require.NoFileExists(t, profFile)
	require.NoFileExists(t, covFile)
	require.NoDirExists(t, cacheDir)
	// Normal clean preserves api.gen.go
	require.FileExists(t, genFile)

	// Clean --all removes api.gen.go as well
	stdout.Reset()

	err = app.Run(context.Background(), []string{"clean", "--all", "-dir=" + tempDir})
	require.NoError(t, err)
	require.NoFileExists(t, genFile)
}

func TestApp_Source(t *testing.T) {
	tempDir := t.TempDir()
	serviceDir := filepath.Join(tempDir, "pkg", "pricedb")
	require.NoError(t, os.MkdirAll(serviceDir, 0o750))

	apiFile := filepath.Join(serviceDir, "api.go")
	apiSrc := `package pricedb

import (
	"context"
	"github.com/lemon4ksan/aoni"
)

// @aoni:service
type PriceDBAPI interface {
	// @get "prices/{sku}"
	GetPrice(ctx context.Context, sku string, mods ...aoni.RequestModifier) (map[string]any, error)
}
`
	require.NoError(t, os.WriteFile(apiFile, []byte(apiSrc), 0o600))

	specFile := filepath.Join(tempDir, "pricedb.json")
	specContent := `{
  "openapi": "3.1.0",
  "info": {"title": "PriceDB", "version": "1.0.0"},
  "paths": {
    "/prices/{sku}": {
      "get": {
        "operationId": "GetPrice",
        "parameters": [{"name": "sku", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"200": {"description": "OK"}}
      }
    }
  }
}`
	require.NoError(t, os.WriteFile(specFile, []byte(specContent), 0o600))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	// 1. Initialize workspace
	err := app.Run(context.Background(), []string{"init", "-dir=" + tempDir})
	require.NoError(t, err)

	// 2. Test source list (initially none)
	stdout.Reset()

	err = app.Run(context.Background(), []string{"source", "list", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "(none)")

	// 3. Test source set
	stdout.Reset()

	err = app.Run(context.Background(), []string{"source", "set", "PriceDBAPI", specFile, "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Successfully bound upstream source")

	// 4. Test source list (now bound)
	stdout.Reset()

	err = app.Run(context.Background(), []string{"source", "list", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "PriceDBAPI")
	require.Contains(t, stdout.String(), "openapi")

	// 5. Test source ping (local file check)
	stdout.Reset()

	err = app.Run(context.Background(), []string{"source", "ping", "PriceDBAPI", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "[LOCAL]")

	// 6. Test source diff
	stdout.Reset()

	err = app.Run(context.Background(), []string{"source", "diff", "PriceDBAPI", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "100% in sync")

	// 7. Test source rm
	stdout.Reset()

	err = app.Run(context.Background(), []string{"source", "rm", "PriceDBAPI", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Removed upstream source binding")

	// 8. Verify list is back to none
	stdout.Reset()

	err = app.Run(context.Background(), []string{"source", "list", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "(none)")
}

func TestApp_Config(t *testing.T) {
	tempDir := t.TempDir()

	serviceDir := filepath.Join(tempDir, "pkg", "user")
	require.NoError(t, os.MkdirAll(serviceDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(serviceDir, "api.go"), []byte(`package user
import (
	"context"
	"github.com/lemon4ksan/aoni"
)
// @aoni:service
type UserAPI interface {
	// @get "users/{id}"
	GetUser(ctx context.Context, id string, mods ...aoni.RequestModifier) (map[string]any, error)
}
`), 0o600))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	// 1. Init workspace
	err := app.Run(context.Background(), []string{"init", "-dir=" + tempDir})
	require.NoError(t, err)

	// 2. Config list
	stdout.Reset()

	err = app.Run(context.Background(), []string{"config", "list", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Vortex Workspace Configuration")

	// 3. Config set defaults
	stdout.Reset()

	err = app.Run(context.Background(), []string{"config", "set", "defaults.casing", "kebab-case", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Set defaults.casing = kebab-case")

	// 4. Config get
	stdout.Reset()

	err = app.Run(context.Background(), []string{"config", "get", "defaults.casing", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Equal(t, "kebab-case\n", stdout.String())

	// 5. Config lint disable & enable
	stdout.Reset()

	err = app.Run(context.Background(), []string{"config", "lint", "disable", "S001", "W002", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Disabled lint rules: S001, W002")

	stdout.Reset()

	err = app.Run(context.Background(), []string{"config", "lint", "enable", "S001", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Enabled lint rules: S001")

	// 6. Config unset
	stdout.Reset()

	err = app.Run(context.Background(), []string{"config", "unset", "defaults.casing", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Unset defaults.casing")
}

func TestApp_Blame(t *testing.T) {
	tempDir := t.TempDir()

	serviceDir := filepath.Join(tempDir, "pkg", "user")
	require.NoError(t, os.MkdirAll(serviceDir, 0o750))
	apiFile := filepath.Join(serviceDir, "api.go")
	require.NoError(t, os.WriteFile(apiFile, []byte(`package user

import (
	"context"
	"github.com/lemon4ksan/aoni"
)

// UserDTO represents a user object
type UserDTO struct {
	ID string
	Name string
}

// @aoni:service
type UserAPI interface {
	// @get "users/{id}"
	// @retry 2
	GetUser(ctx context.Context, id string, mods ...aoni.RequestModifier) (*UserDTO, error)
}
`), 0o600))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	// 1. Run blame on file
	err := app.Run(context.Background(), []string{"blame", apiFile, "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "GetUser")
	require.Contains(t, stdout.String(), "UserDTO")
	require.Contains(t, stdout.String(), "Vortex Contract Provenance")

	// 2. Run blame JSON
	stdout.Reset()

	err = app.Run(context.Background(), []string{"blame", apiFile, "-json", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), `"name": "GetUser"`)
	require.Contains(t, stdout.String(), `"name": "UserDTO"`)
}

func TestApp_Tag(t *testing.T) {
	tempDir := t.TempDir()

	serviceDir := filepath.Join(tempDir, "pkg", "market")
	require.NoError(t, os.MkdirAll(serviceDir, 0o750))
	apiFile := filepath.Join(serviceDir, "api.go")
	require.NoError(t, os.WriteFile(apiFile, []byte(`package market

import (
	"context"
	"github.com/lemon4ksan/aoni"
)

// @aoni:service
type MarketAPI interface {
	// @get "items"
	ListItems(ctx context.Context, mods ...aoni.RequestModifier) (map[string]any, error)
}
`), 0o600))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	// 1. Init workspace
	err := app.Run(context.Background(), []string{"init", "-dir=" + tempDir})
	require.NoError(t, err)

	// 2. Add tag
	stdout.Reset()

	err = app.Run(
		context.Background(),
		[]string{"tag", "add", "v1.0.0", "Market", "-m", "Initial market API release", "-git=false", "-dir=" + tempDir},
	)
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Created API release tag v1.0.0")

	// 3. List tags
	stdout.Reset()

	err = app.Run(context.Background(), []string{"tag", "list", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "v1.0.0")
	require.Contains(t, stdout.String(), "Initial market API release")

	// 4. Show tag
	stdout.Reset()

	err = app.Run(context.Background(), []string{"tag", "show", "v1.0.0", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "API Release Snapshot: v1.0.0")
	require.Contains(t, stdout.String(), "ListItems")

	// 5. Remove tag
	stdout.Reset()

	err = app.Run(context.Background(), []string{"tag", "rm", "v1.0.0", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Removed API release tag v1.0.0")
}

func TestApp_CherryPick(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Source Service (Mannco)
	srcDir := filepath.Join(tempDir, "pkg", "mannco")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))
	srcFile := filepath.Join(srcDir, "api.go")
	require.NoError(t, os.WriteFile(srcFile, []byte(`package mannco

import (
	"context"
	"github.com/lemon4ksan/aoni"
)

type ItemDetails struct {
	Name string
	SKU  string
}

type ItemPriceResponse struct {
	Price float64
	Item  *ItemDetails
}

// @aoni:service
type ManncoAPI interface {
	// @get "items/{sku}/price"
	GetItemPrice(ctx context.Context, sku string, mods ...aoni.RequestModifier) (*ItemPriceResponse, error)
}
`), 0o600))

	// 2. Dest Service (Bptf)
	destDir := filepath.Join(tempDir, "pkg", "bptf")
	require.NoError(t, os.MkdirAll(destDir, 0o750))
	destFile := filepath.Join(destDir, "api.go")
	require.NoError(t, os.WriteFile(destFile, []byte(`package bptf

import (
	"context"
	"github.com/lemon4ksan/aoni"
)

// @aoni:service
type BptfAPI interface {
	// @get "heartbeat"
	Ping(ctx context.Context, mods ...aoni.RequestModifier) (map[string]any, error)
}
`), 0o600))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	// Dry run first
	err := app.Run(
		context.Background(),
		[]string{"cherry-pick", srcFile + ":GetItemPrice", "--to=" + destFile, "-dry-run", "-dir=" + tempDir},
	)
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Dry-Run AST Transplant")

	// Real cherry-pick
	stdout.Reset()

	err = app.Run(
		context.Background(),
		[]string{"cherry-pick", srcFile + ":GetItemPrice", "--to=" + destFile, "-dir=" + tempDir},
	)
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Successfully cherry-picked GetItemPrice")
	require.Contains(t, stdout.String(), "2 dependent DTO struct(s) copied")

	// Verify dest file now contains GetItemPrice, ItemPriceResponse, and nested ItemDetails
	content, err := os.ReadFile(destFile)
	require.NoError(t, err)
	require.Contains(t, string(content), "GetItemPrice(ctx context.Context")
	require.Contains(t, string(content), "type ItemPriceResponse struct")
	require.Contains(t, string(content), "type ItemDetails struct")
}

func TestApp_Refactor(t *testing.T) {
	tempDir := t.TempDir()

	serviceDir := filepath.Join(tempDir, "pkg", "market")
	require.NoError(t, os.MkdirAll(serviceDir, 0o750))
	apiFile := filepath.Join(serviceDir, "api.go")
	require.NoError(t, os.WriteFile(apiFile, []byte(`package market

import (
	"context"
	"github.com/lemon4ksan/aoni"
)

// @aoni:service
type MarketAPI interface {
	// @get "items"
	FetchItems(ctx context.Context, mods ...aoni.RequestModifier) (map[string]any, error)

	// @get "items/{id}"
	FetchItemByID(ctx context.Context, id string, mods ...aoni.RequestModifier) (map[string]any, error)

	// @post "items"
	CreateListing(ctx context.Context, mods ...aoni.RequestModifier) (map[string]any, error)
}
`), 0o600))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	// 1. Test Refactor Rename (Fetch* -> Get*)
	err := app.Run(
		context.Background(),
		[]string{"refactor", "rename", "--match=Fetch(.*)", "--replace=Get$1", apiFile, "-dir=" + tempDir},
	)
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Successfully renamed 2 method(s)")

	content, err := os.ReadFile(apiFile)
	require.NoError(t, err)
	require.Contains(t, string(content), "GetItems(ctx context.Context")
	require.Contains(t, string(content), "GetItemByID(ctx context.Context")
	require.NotContains(t, string(content), "FetchItems")

	// 2. Test Refactor Split (Get* -> MarketReaderAPI in same file)
	stdout.Reset()

	err = app.Run(
		context.Background(),
		[]string{
			"refactor",
			"split",
			"--from=MarketAPI",
			"--methods=Get*",
			"--to=MarketReaderAPI",
			apiFile,
			"-dir=" + tempDir,
		},
	)
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Successfully split")

	content2, err := os.ReadFile(apiFile)
	require.NoError(t, err)
	require.Contains(t, string(content2), "type MarketReaderAPI interface")
	require.Contains(t, string(content2), "type MarketAPI interface")
	require.Contains(t, string(content2), "CreateListing")
}
