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
		&CmdStatus{},
		&CmdInit{},
		&CmdGen{},
		&CmdCheck{},
		&CmdDiff{},
		&CmdLog{},
		&CmdOAPI{},
		&CmdProto{},
		&CmdBench{},
		&CmdCover{},
		&CmdList{},
		&CmdExplain{},
		&CmdExample{},
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
	require.Contains(t, stdout.String(), "vortex "+version.Current)
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
