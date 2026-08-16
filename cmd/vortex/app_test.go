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
)

func newTestApp(stdout, stderr *bytes.Buffer) *App {
	commands := []Command{
		&CmdGen{},
		&CmdCheck{},
		&CmdOAPI{},
		&CmdProto{},
		&CmdBench{},
		&CmdCover{},
		&CmdList{},
		&CmdExplain{},
		&CmdExample{},
	}

	app := NewApp("vortex", "v0.6.0", "Unified Toolchain", commands...)
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
	require.Contains(t, stdout.String(), "vortex v0.6.0")
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

func TestApp_UnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"some_bogus_command"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown command")
}
