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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCmdWork_InitAndStatus(t *testing.T) {
	tempDir := t.TempDir()

	// Create 2 mock workspaces with .vortex.yml
	ws1 := filepath.Join(tempDir, "service-a")
	require.NoError(t, os.MkdirAll(ws1, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(ws1, ".vortex.yml"), []byte("version: 1\ncontracts: []\n"), 0o600))

	ws2 := filepath.Join(tempDir, "service-b")
	require.NoError(t, os.MkdirAll(ws2, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(ws2, ".vortex.yml"), []byte("version: 1\ncontracts: []\n"), 0o600))

	origWd, _ := os.Getwd()
	defer func() { _ = os.Chdir(origWd) }()

	require.NoError(t, os.Chdir(tempDir))

	var stdout, stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr)

	// 1. vortex work init
	err := app.Run(context.Background(), []string{"work", "init"})
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "Created multi-repo workspace configuration")
	assert.Contains(t, stdout.String(), "./service-a")
	assert.Contains(t, stdout.String(), "./service-b")

	// 2. vortex work status
	stdout.Reset()

	err = app.Run(context.Background(), []string{"work", "status"})
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "Vortex Workspace Orchestrator")
	assert.Contains(t, stdout.String(), "./service-a")
	assert.Contains(t, stdout.String(), "./service-b")
}
