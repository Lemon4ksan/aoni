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
	app := NewApp("vortex", version.Current, "Unified Toolchain")
	app.Stdout = stdout
	app.Stderr = stderr

	return app
}

func TestApp_HelpAndVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"--help"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Usage:")

	stdout.Reset()

	err = app.Run(context.Background(), []string{"--version"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), version.Current)
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

	err := app.Run(context.Background(), []string{"spec", "diff", specFile, srcFile})
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

	err := app.Run(context.Background(), []string{"ast", "log", srcFile})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Vortex API Contract Timeline")
	require.Contains(t, stdout.String(), "v1.4.0")
	require.Contains(t, stdout.String(), "test_openapi.json")

	// JSON format
	stdout.Reset()

	err = app.Run(context.Background(), []string{"ast", "log", "-json", srcFile})
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

	err := app.Run(context.Background(), []string{"spec", "import", "-spec=" + specFile, "-out=" + srcFile})
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
	err := app.Run(context.Background(), []string{"ast", "review"})
	require.Error(t, err)

	// Accept missing arg
	err = app.Run(context.Background(), []string{"ast", "accept"})
	require.Error(t, err)
}

func TestApp_Log_Git_Flag(t *testing.T) {
	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	// Log with --git flag on local file
	err := app.Run(context.Background(), []string{"ast", "log", "--git", "-n=3", "main.go"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "⚡ Vortex API Git History")
}

func TestApp_Diff_Against_HEAD(t *testing.T) {
	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	// Diff with --against=HEAD on local file
	err := app.Run(context.Background(), []string{"traffic", "diff", "--against=HEAD", "main.go"})
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

	err := app.Run(context.Background(), []string{"perf", "harness", apiFile})
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
	require.Contains(t, stdout.String(), "Mock Server: api_mock.gen.go")

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

	err = app.Run(context.Background(), []string{"spec", "source", "list", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "(none)")

	// 3. Test source set
	stdout.Reset()

	err = app.Run(context.Background(), []string{"spec", "source", "set", "PriceDBAPI", specFile, "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Successfully bound upstream source")

	// 4. Test source list (now bound)
	stdout.Reset()

	err = app.Run(context.Background(), []string{"spec", "source", "list", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "PriceDBAPI")
	require.Contains(t, stdout.String(), "openapi")

	// 5. Test source ping (local file check)
	stdout.Reset()

	err = app.Run(context.Background(), []string{"spec", "source", "ping", "PriceDBAPI", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "[LOCAL]")

	// 6. Test source diff
	stdout.Reset()

	err = app.Run(context.Background(), []string{"spec", "source", "diff", "PriceDBAPI", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "100% in sync")

	// 7. Test source rm
	stdout.Reset()

	err = app.Run(context.Background(), []string{"spec", "source", "rm", "PriceDBAPI", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Removed upstream source binding")

	// 8. Verify list is back to none
	stdout.Reset()

	err = app.Run(context.Background(), []string{"spec", "source", "list", "-dir=" + tempDir})
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
	err := app.Run(context.Background(), []string{"ast", "blame", apiFile, "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "GetUser")
	require.Contains(t, stdout.String(), "UserDTO")
	require.Contains(t, stdout.String(), "Vortex Contract Provenance")

	// 2. Run blame JSON
	stdout.Reset()

	err = app.Run(context.Background(), []string{"ast", "blame", apiFile, "-json", "-dir=" + tempDir})
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
		[]string{
			"ast",
			"tag",
			"add",
			"v1.0.0",
			"Market",
			"-m",
			"Initial market API release",
			"-git=false",
			"-dir=" + tempDir,
		},
	)
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Created API release tag v1.0.0")

	// 3. List tags
	stdout.Reset()

	err = app.Run(context.Background(), []string{"ast", "tag", "list", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "v1.0.0")
	require.Contains(t, stdout.String(), "Initial market API release")

	// 4. Show tag
	stdout.Reset()

	err = app.Run(context.Background(), []string{"ast", "tag", "show", "v1.0.0", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "API Release Snapshot: v1.0.0")
	require.Contains(t, stdout.String(), "ListItems")

	// 5. Remove tag
	stdout.Reset()

	err = app.Run(context.Background(), []string{"ast", "tag", "rm", "v1.0.0", "-dir=" + tempDir})
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
		[]string{"ast", "pick", srcFile + ":GetItemPrice", "--to=" + destFile, "-dry-run", "-dir=" + tempDir},
	)
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Dry-Run AST Transplant")

	// Real cherry-pick
	stdout.Reset()

	err = app.Run(
		context.Background(),
		[]string{"ast", "pick", srcFile + ":GetItemPrice", "--to=" + destFile, "-dir=" + tempDir},
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

func TestApp_HistoryAndUndo(t *testing.T) {
	tempDir := t.TempDir()

	serviceDir := filepath.Join(tempDir, "pkg", "market")
	require.NoError(t, os.MkdirAll(serviceDir, 0o750))
	apiFile := filepath.Join(serviceDir, "api.go")
	initialContent := `package market

import (
	"context"
	"github.com/lemon4ksan/aoni"
)

// @aoni:service
type MarketAPI interface {
	// @get "items"
	FetchItems(ctx context.Context, mods ...aoni.RequestModifier) (map[string]any, error)
}
`
	require.NoError(t, os.WriteFile(apiFile, []byte(initialContent), 0o600))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	// 1. Perform rename (FetchItems -> GetItems)
	err := app.Run(
		context.Background(),
		[]string{"ast", "rename", "--match=Fetch(.*)", "--replace=Get$1", apiFile, "-dir=" + tempDir},
	)
	require.NoError(t, err)

	renamedContent, err := os.ReadFile(apiFile)
	require.NoError(t, err)
	require.Contains(t, string(renamedContent), "GetItems")

	// 2. View History
	stdout.Reset()

	err = app.Run(context.Background(), []string{"ast", "history", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "vortex refactor rename")
	require.Contains(t, stdout.String(), "api.go")

	// 3. Undo Operation (Ctrl+Z)
	stdout.Reset()

	err = app.Run(context.Background(), []string{"ast", "undo", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Successfully reverted")

	// 4. Verify file restored to FetchItems
	restoredContent, err := os.ReadFile(apiFile)
	require.NoError(t, err)
	require.Contains(t, string(restoredContent), "FetchItems")
	require.NotContains(t, string(restoredContent), "GetItems")
}

func TestApp_CheckIncrementalCache(t *testing.T) {
	tempDir := t.TempDir()

	serviceDir := filepath.Join(tempDir, "pkg", "market")
	require.NoError(t, os.MkdirAll(serviceDir, 0o750))
	apiFile := filepath.Join(serviceDir, "api.go")
	initialContent := `package market

import (
	"context"
	"github.com/lemon4ksan/aoni"
)

// @aoni:service
type MarketAPI interface {
	// @get "items"
	GetItems(ctx context.Context, mods ...aoni.RequestModifier) (map[string]any, error)
}
`
	require.NoError(t, os.WriteFile(apiFile, []byte(initialContent), 0o600))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	// 1. Generate client first so stale-codegen passes
	err := app.Run(context.Background(), []string{"gen", apiFile})
	require.NoError(t, err)

	// 2. Initial check (populates cache)
	stdout.Reset()

	err = app.Run(context.Background(), []string{"check", apiFile})
	require.NoError(t, err)

	// 3. Second check (hits cache)
	stdout.Reset()

	err = app.Run(context.Background(), []string{"check", apiFile})
	require.NoError(t, err)
}

func TestApp_CheckMirrorRules(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Create legacy backend root source (Untagged pure Go)
	legacyDir := filepath.Join(tempDir, "internal", "legacy", "steam")
	require.NoError(t, os.MkdirAll(legacyDir, 0o750))
	legacyFile := filepath.Join(legacyDir, "inventory.go")
	require.NoError(t, os.WriteFile(legacyFile, []byte(`package steam

import "context"

type LegacyItem struct {
	AssetID uint64
}

type LegacyInventoryService interface {
	GetInventory(ctx context.Context, steamID uint64) ([]*LegacyItem, error)
	BatchTransfer(ctx context.Context, assetIDs []uint64) error
}
`), 0o600))

	// 2. Create Aoni wrapper with intentional signature drift (steamID string instead of uint64) and missing BatchTransfer
	wrapperDir := filepath.Join(tempDir, "pkg", "steam", "inventory")
	require.NoError(t, os.MkdirAll(wrapperDir, 0o750))
	wrapperFile := filepath.Join(wrapperDir, "api.go")
	require.NoError(t, os.WriteFile(wrapperFile, []byte(`package inventory

import (
	"context"
	"github.com/lemon4ksan/aoni"
)

type Item struct {
	AssetID uint64
}

// @aoni:service
// @aoni:mirror "internal/legacy/steam/inventory.go:LegacyInventoryService"
type InventoryWrapperAPI interface {
	// @get "inventory"
	GetInventory(ctx context.Context, steamID string, mods ...aoni.RequestModifier) ([]*Item, error)
}
`), 0o600))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	// Run gen so stale-codegen doesn't fire
	_ = app.Run(context.Background(), []string{"gen", wrapperFile})

	// Run check
	stdout.Reset()

	_ = app.Run(context.Background(), []string{"check", wrapperFile, "-dir=" + tempDir})

	outStr := stdout.String()
	// E016: mirror-signature-drift should be reported
	require.Contains(t, outStr, "E016")
	require.Contains(t, outStr, "mirror-signature-drift")
	// W012: mirror-ghost-method should be reported for BatchTransfer
	require.Contains(t, outStr, "W012")
	require.Contains(t, outStr, "mirror-ghost-method")
}

func TestApp_Init_PackageScaffolding(t *testing.T) {
	tempDir := t.TempDir()

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	// 1. Test init package with default REST template
	err := app.Run(context.Background(), []string{"init", "billing", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Successfully initialized package billing")

	billingFile := filepath.Join(tempDir, "pkg", "billing", "api.go")
	require.FileExists(t, billingFile)

	content, err := os.ReadFile(billingFile)
	require.NoError(t, err)
	require.Contains(t, string(content), "package billing")
	require.Contains(t, string(content), "type BillingAPI interface")
	require.Contains(t, string(content), "ListItems")

	// Verify .vortex.yml auto-registration
	vortexYml := filepath.Join(tempDir, ".vortex.yml")
	require.FileExists(t, vortexYml)
	ymlBytes, err := os.ReadFile(vortexYml)
	require.NoError(t, err)
	require.Contains(t, string(ymlBytes), "BillingAPI")
	require.Contains(t, string(ymlBytes), "pkg/billing/api.go")

	// 2. Test init package with WebSocket template
	stdout.Reset()

	err = app.Run(context.Background(), []string{"init", "chat", "-tpl=ws", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Successfully initialized package chat")

	chatFile := filepath.Join(tempDir, "pkg", "chat", "api.go")
	require.FileExists(t, chatFile)
	chatContent, err := os.ReadFile(chatFile)
	require.NoError(t, err)
	require.Contains(t, string(chatContent), "package chat")
	require.Contains(t, string(chatContent), "type ChatAPI interface")
	require.Contains(t, string(chatContent), `// @aoni:service protocol=ws`)

	// 3. Test init with list templates
	stdout.Reset()

	err = app.Run(context.Background(), []string{"init", "-tpl=list"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Vortex Ready-Made Contract Templates")
}

func TestApp_Diff_SpecToSpec(t *testing.T) {
	tempDir := t.TempDir()

	specA := filepath.Join(tempDir, "specA.json")
	specB := filepath.Join(tempDir, "specB.json")

	specAContent := `{"openapi": "3.0.0", "info": {"title": "A", "version": "1.0"}, "paths": {"/orders": {"get": {"responses": {"200": {"description": "OK"}}}}}}`
	specBContent := `{"openapi": "3.0.0", "info": {"title": "B", "version": "2.0"}, "paths": {"/orders": {"get": {"parameters": [{"name": "key", "in": "query", "required": true, "schema": {"type": "string"}}], "responses": {"200": {"description": "OK"}}}}, "/invoices": {"get": {"responses": {"200": {"description": "OK"}}}}}}`

	require.NoError(t, os.WriteFile(specA, []byte(specAContent), 0o600))
	require.NoError(t, os.WriteFile(specB, []byte(specBContent), 0o600))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"spec", "diff", specA, specB})
	require.NoError(t, err)

	outStr := stdout.String()
	require.Contains(t, outStr, "Vortex Schema Drift Inspector")
	require.Contains(t, outStr, "added required query parameter \"key\"")
	require.Contains(t, outStr, "Endpoint GET /invoices was added")
}

func TestApp_Cache_Workflow(t *testing.T) {
	tempDir := t.TempDir()
	cwd, err := os.Getwd()
	require.NoError(t, err)

	defer func() { _ = os.Chdir(cwd) }()

	require.NoError(t, os.Chdir(tempDir))

	harData := []byte(`{
		"log": {
			"version": "1.2",
			"entries": [
				{
					"request": {
						"method": "GET",
						"url": "https://api.example.com/v1/users",
						"headers": [
							{"name": "Authorization", "value": "Bearer sample_secret_token_123"},
							{"name": "x-goog-api-key", "value": "AIzaSySecretApiKey"}
						]
					},
					"response": {
						"status": 200,
						"headers": [{"name": "Content-Type", "value": "application/json"}]
					}
				}
			]
		}
	}`)

	harFile := filepath.Join(tempDir, "traffic.har")
	require.NoError(t, os.WriteFile(harFile, harData, 0o600))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	// 1. vortex traffic cache store --move
	err = app.Run(context.Background(), []string{"traffic", "cache", "store", "--move", harFile})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Moved")
	require.NoFileExists(t, harFile)

	// 2. vortex traffic cache list
	stdout.Reset()

	err = app.Run(context.Background(), []string{"traffic", "cache", "list"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "traffic")
	require.Contains(t, stdout.String(), "api.example.com")

	// 3. vortex traffic cache show
	stdout.Reset()

	err = app.Run(context.Background(), []string{"traffic", "cache", "show", "traffic"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "traffic")
	require.Contains(t, stdout.String(), "1 total entries")

	// 4. vortex traffic cache secrets list & get
	stdout.Reset()

	err = app.Run(context.Background(), []string{"traffic", "cache", "secrets", "list"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "AUTH_TOKEN")
	require.Contains(t, stdout.String(), "GOOGLE_API_KEY")

	stdout.Reset()

	err = app.Run(context.Background(), []string{"traffic", "cache", "secrets", "get", "AUTH_TOKEN"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "sample_secret_token_123")

	// 5. vortex traffic cache export
	stdout.Reset()

	restoredFile := filepath.Join(tempDir, "restored.har")
	err = app.Run(context.Background(), []string{"traffic", "cache", "export", "traffic", "-out=" + restoredFile})
	require.NoError(t, err)
	require.FileExists(t, restoredFile)

	// 6. vortex traffic cache prune
	stdout.Reset()

	err = app.Run(context.Background(), []string{"traffic", "cache", "prune", "--all"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Removed 1 cached traffic session(s)")
}

func TestApp_Config_Secrets(t *testing.T) {
	tempDir := t.TempDir()
	cwd, err := os.Getwd()
	require.NoError(t, err)

	defer func() { _ = os.Chdir(cwd) }()

	require.NoError(t, os.Chdir(tempDir))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	// 1. vortex init
	err = app.Run(context.Background(), []string{"init", "-f"})
	require.NoError(t, err)

	// 2. vortex config secret add
	stdout.Reset()

	err = app.Run(context.Background(), []string{"config", "secret", "header", "x-custom-key", "CUSTOM_KEY"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "x-custom-key -> ${CUSTOM_KEY}")

	stdout.Reset()

	err = app.Run(context.Background(), []string{"config", "secret", "query", "api_key", "API_KEY"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "?api_key -> ${API_KEY}")

	stdout.Reset()

	err = app.Run(context.Background(), []string{"config", "secret", "cookie", "session_id", "SESSION_ID"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "session_id -> ${SESSION_ID}")

	// 3. vortex config secret list
	stdout.Reset()

	err = app.Run(context.Background(), []string{"config", "secret", "list"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "x-custom-key")
	require.Contains(t, stdout.String(), "api_key")
	require.Contains(t, stdout.String(), "session_id")

	// 4. vortex config secret rm
	stdout.Reset()

	err = app.Run(context.Background(), []string{"config", "secret", "rm", "header", "x-custom-key"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Removed secret header rule")

	stdout.Reset()

	err = app.Run(context.Background(), []string{"config", "secret", "list"})
	require.NoError(t, err)
	require.NotContains(t, stdout.String(), "x-custom-key")
}

func TestApp_HARDifferential_Diff(t *testing.T) {
	tempDir := t.TempDir()
	cwd, err := os.Getwd()
	require.NoError(t, err)

	defer func() { _ = os.Chdir(cwd) }()

	require.NoError(t, os.Chdir(tempDir))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	// Create sample contract
	contractGo := `package testapi

// TestAPI provides contract definition.
//
// @aoni:service
// @base_url "https://api.test.com"
type TestAPI interface {
	// @post "/$rpc/google.test.Service/GenerateContent"
	GenerateContent(req GenerateContentRequest) (any, error)
}

// @aoni:tuple
type GenerateContentRequest struct {
	Field0 string ` + "`" + `aoni:"0"` + "`" + `
	Field4 int    ` + "`" + `aoni:"4"` + "`" + `
	Field5 float64 ` + "`" + `aoni:"5"` + "`" + `
}
`
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "api.go"), []byte(contractGo), 0o600))

	// HAR 1: output length 65536
	harA := `{
  "log": {
    "entries": [
      {
        "request": {
          "method": "POST",
          "url": "https://api.test.com/$rpc/google.test.Service/GenerateContent",
          "postData": {
            "text": "[\"gemini-1.5\", 0, 0, 0, 65536, 0.7]"
          }
        },
        "response": {
          "status": 200,
          "content": {
            "text": "[\"result\"]"
          }
        }
      }
    ]
  }
}`

	// HAR 2: output length 8192 and temp 1.0
	harB := `{
  "log": {
    "entries": [
      {
        "request": {
          "method": "POST",
          "url": "https://api.test.com/$rpc/google.test.Service/GenerateContent",
          "postData": {
            "text": "[\"gemini-1.5\", 0, 0, 0, 8192, 1.0]"
          }
        },
        "response": {
          "status": 200,
          "content": {
            "text": "[\"result\"]"
          }
        }
      }
    ]
  }
}`

	fileA := filepath.Join(tempDir, "session_a.har")
	fileB := filepath.Join(tempDir, "session_b.har")

	require.NoError(t, os.WriteFile(fileA, []byte(harA), 0o600))
	require.NoError(t, os.WriteFile(fileB, []byte(harB), 0o600))

	// Run vortex traffic diff session_a.har session_b.har
	stdout.Reset()

	err = app.Run(context.Background(), []string{"traffic", "diff", fileA, fileB})
	require.NoError(t, err)

	out := stdout.String()
	require.Contains(t, out, "Traffic Diff")
	require.Contains(t, out, "GenerateContentRequest")
	require.Contains(t, out, "Field4")
	require.Contains(t, out, "65536 ➔ 8192")
	require.Contains(t, out, "vortex ast rename --type=GenerateContentRequest --field=Field4 --to=<NAME>")
	require.Contains(t, out, "Field5")
	require.Contains(t, out, "0.7 ➔ 1")
}

func TestApp_AST_FieldRename(t *testing.T) {
	tempDir := t.TempDir()
	cwd, err := os.Getwd()
	require.NoError(t, err)

	defer func() { _ = os.Chdir(cwd) }()

	require.NoError(t, os.Chdir(tempDir))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	contractGo := `package testapi

// @aoni:tuple
type GenerateContentRequest struct {
	Field0 string ` + "`" + `aoni:"0"` + "`" + `
	Field4 int    ` + "`" + `aoni:"4"` + "`" + `
}
`
	apiPath := filepath.Join(tempDir, "api.go")
	require.NoError(t, os.WriteFile(apiPath, []byte(contractGo), 0o600))

	// Run vortex ast rename --type=GenerateContentRequest --field=Field4 --to=MaxOutputTokens
	err = app.Run(
		context.Background(),
		[]string{
			"ast",
			"rename",
			"--type=GenerateContentRequest",
			"--field=Field4",
			"--to=MaxOutputTokens",
			"-gen=false",
		},
	)
	require.NoError(t, err)

	data, err := os.ReadFile(apiPath)
	require.NoError(t, err)

	content := string(data)
	require.Contains(t, content, "MaxOutputTokens int")
	require.Contains(t, content, "`aoni:\"4\"`")
	require.NotContains(t, content, "Field4")
}

func TestApp_JSImport_PreservesExistingTypes(t *testing.T) {
	tempDir := t.TempDir()
	cwd, err := os.Getwd()
	require.NoError(t, err)

	defer func() { _ = os.Chdir(cwd) }()

	require.NoError(t, os.Chdir(tempDir))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	contractGo := `package testapi

import (
	"context"

	"github.com/lemon4ksan/aoni"
)

// API provides client operations.
//
// @aoni:service casing=snake_case
// @base_url "https://api.example.com"
type API interface {
	// @post "rpc/GenerateContent"
	GenerateContent(ctx context.Context, req *GenerateContentRequest, mods ...aoni.RequestModifier) (*GenerateContentResponse, error)
}

// @aoni:tuple
type GenerateContentRequest struct {
	Model  string ` + "`" + `aoni:"0"` + "`" + `
	Prompt string ` + "`" + `aoni:"1"` + "`" + `
}

// @aoni:tuple
type GenerateContentResponse struct {
	Text string ` + "`" + `aoni:"0"` + "`" + `
}
`
	apiPath := filepath.Join(tempDir, "api.go")
	require.NoError(t, os.WriteFile(apiPath, []byte(contractGo), 0o600))

	jsBundle := `
const ep1 = "/rpc/GenerateContent";
const ep2 = "/$rpc/google.internal/ListModels";
`
	jsPath := filepath.Join(tempDir, "bundle.js")
	require.NoError(t, os.WriteFile(jsPath, []byte(jsBundle), 0o600))

	// Run vortex spec import -js=bundle.js -out=api.go -pkg=testapi -service=API
	err = app.Run(
		context.Background(),
		[]string{"spec", "import", "-js=" + jsPath, "-out=" + apiPath, "-pkg=testapi", "-service=API"},
	)
	require.NoError(t, err)

	data, err := os.ReadFile(apiPath)
	require.NoError(t, err)

	content := string(data)

	// 1. Existing typed request & response must NOT be downgraded to any
	require.Contains(
		t,
		content,
		"GenerateContent(ctx context.Context, req *GenerateContentRequest, mods ...aoni.RequestModifier) (*GenerateContentResponse, error)",
	)
	require.NotContains(t, content, "GenerateContent(ctx context.Context, req any")

	// 2. Existing custom struct declarations must be preserved
	require.Contains(t, content, "type GenerateContentRequest struct")
	require.Contains(t, content, "Model")
	require.Contains(t, content, "`aoni:\"0\"`")
	require.Contains(t, content, "Prompt")
	require.Contains(t, content, "`aoni:\"1\"`")
	require.Contains(t, content, "type GenerateContentResponse struct")
	require.Contains(t, content, "Text")

	// 3. New endpoint from JS bundle must be added
	require.Contains(t, content, "ListModels")
}

func TestApp_Stack_LifecycleAndDiff(t *testing.T) {
	tempDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)

	defer func() { _ = os.Chdir(origDir) }()

	require.NoError(t, os.Chdir(tempDir))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	apiPath := filepath.Join(tempDir, "api.go")

	// 1. Initial Frame #0
	v0 := `package testapi
// @aoni:service
type API interface {
	// @post "rpc/Generate"
	RPCMethod1(ctx context.Context, req *Req) (*Resp, error)
}
// @aoni:tuple
type Req struct {
	Field0 string ` + "`" + `aoni:"0"` + "`" + `
	Field4 int    ` + "`" + `aoni:"4"` + "`" + `
}
`
	require.NoError(t, os.WriteFile(apiPath, []byte(v0), 0o600))

	// ast stack push -label="base-scan"
	err = app.Run(context.Background(), []string{"ast", "stack", "push", "-label=base-scan", apiPath})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Captured stack snapshot frame #0 [base-scan]")

	// 2. Frame #1: rename Field0 -> ModelName
	v1 := `package testapi
// @aoni:service
type API interface {
	// @post "rpc/Generate"
	RPCMethod1(ctx context.Context, req *Req) (*Resp, error)
}
// @aoni:tuple
type Req struct {
	ModelName string ` + "`" + `aoni:"0"` + "`" + `
	Field4    int    ` + "`" + `aoni:"4"` + "`" + `
}
`
	require.NoError(t, os.WriteFile(apiPath, []byte(v1), 0o600))
	stdout.Reset()

	err = app.Run(context.Background(), []string{"ast", "stack", "push", "-label=deobf-model", apiPath})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Captured stack snapshot frame #1 [deobf-model]")

	// 3. Frame #2: rename Field4 -> MaxTokens, RPCMethod1 -> GenerateContent
	v2 := `package testapi
// @aoni:service
type API interface {
	// @post "rpc/Generate"
	GenerateContent(ctx context.Context, req *Req) (*Resp, error)
}
// @aoni:tuple
type Req struct {
	ModelName string ` + "`" + `aoni:"0"` + "`" + `
	MaxTokens int    ` + "`" + `aoni:"4"` + "`" + `
}
`
	require.NoError(t, os.WriteFile(apiPath, []byte(v2), 0o600))
	stdout.Reset()

	err = app.Run(context.Background(), []string{"ast", "stack", "push", "-label=full-polish", apiPath})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Captured stack snapshot frame #2 [full-polish]")

	// 4. Test stack list
	stdout.Reset()

	err = app.Run(context.Background(), []string{"ast", "stack", "list"})
	require.NoError(t, err)

	outList := stdout.String()
	require.Contains(t, outList, "[#2] full-polish")
	require.Contains(t, outList, "[#1] deobf-model")
	require.Contains(t, outList, "[#0] base-scan")
	require.Contains(t, outList, "[CURRENT HEAD]")
	require.Contains(t, outList, "[BASE]")

	// 5. Test stack diff (Adjacent Frame 2 vs Frame 1)
	stdout.Reset()

	err = app.Run(context.Background(), []string{"ast", "stack", "diff"})
	require.NoError(t, err)

	adjDiff := stdout.String()
	require.Contains(t, adjDiff, "Field4 ➔ MaxTokens")
	require.Contains(t, adjDiff, "RPCMethod1 ➔ GenerateContent")
	require.NotContains(t, adjDiff, "Field0 ➔ ModelName") // Happened in Frame 1, not Frame 2

	// 6. Test stack diff --cumulative (Frame 2 vs Frame 0)
	stdout.Reset()

	err = app.Run(context.Background(), []string{"ast", "stack", "diff", "--cumulative"})
	require.NoError(t, err)

	cumDiff := stdout.String()
	require.Contains(t, cumDiff, "Field0 ➔ ModelName")
	require.Contains(t, cumDiff, "Field4 ➔ MaxTokens")
	require.Contains(t, cumDiff, "RPCMethod1 ➔ GenerateContent")

	// 7. Test stack restore base
	stdout.Reset()

	err = app.Run(context.Background(), []string{"ast", "stack", "restore", "base"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Restored workspace state to frame \"base\"")

	// Verify file content on disk rolled back to v0
	restored, err := os.ReadFile(apiPath)
	require.NoError(t, err)
	require.Contains(t, string(restored), "Field0 string")
	require.Contains(t, string(restored), "Field4 int")
	require.Contains(t, string(restored), "RPCMethod1")
}

func TestApp_Doctor(t *testing.T) {
	tempDir := t.TempDir()
	pkgDir := filepath.Join(tempDir, "pkg", "docdemo")
	require.NoError(t, os.MkdirAll(pkgDir, 0o750))

	apiSrc := `package docdemo

import (
	"context"
	"github.com/lemon4ksan/aoni"
)

// @aoni:service
type DocDemoAPI interface {
	// @get "check"
	Check(ctx context.Context, mods ...aoni.RequestModifier) (map[string]any, error)
}
`
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "api.go"), []byte(apiSrc), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "api.gen.go"), []byte("package docdemo\n"), 0o600))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	// 1. Run doctor on clean workspace
	err := app.Run(context.Background(), []string{"doctor", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Vortex Doctor")
	require.Contains(t, stdout.String(), "Go Toolchain")

	// 2. Run doctor with --json
	stdout.Reset()

	err = app.Run(context.Background(), []string{"doctor", "-json", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), `"workspace_root"`)
	require.Contains(t, stdout.String(), `"go_version"`)

	// 3. Run doctor --fix
	stdout.Reset()

	err = app.Run(context.Background(), []string{"doctor", "--fix", "-dir=" + tempDir})
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(tempDir, ".gitignore"))
	require.FileExists(t, filepath.Join(tempDir, ".gitattributes"))
}

func TestApp_Init_Git(t *testing.T) {
	tempDir := t.TempDir()

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"init", "--git", "-dir=" + tempDir})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Updated .gitignore")
	require.Contains(t, stdout.String(), "Updated .gitattributes")
	require.FileExists(t, filepath.Join(tempDir, ".gitignore"))
	require.FileExists(t, filepath.Join(tempDir, ".gitattributes"))
}
