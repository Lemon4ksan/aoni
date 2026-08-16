// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package git_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/codegen/git"
)

func TestGit_ShowFile_And_RootDir(t *testing.T) {
	t.Parallel()

	cwd, err := os.Getwd()
	require.NoError(t, err)

	ctx := context.Background()
	rootDir, err := git.RootDir(ctx, cwd)
	require.NoError(t, err)
	assert.NotEmpty(t, rootDir)

	// Read go.mod from HEAD
	data, err := git.ShowFile(ctx, rootDir, "HEAD", "go.mod")
	require.NoError(t, err)
	assert.Contains(t, string(data), "module github.com/lemon4ksan/aoni")

	// Current branch
	branch, err := git.CurrentBranch(ctx, rootDir)
	require.NoError(t, err)
	assert.NotEmpty(t, branch)

	// Log commits for go.mod
	commits, err := git.LogCommits(ctx, rootDir, "go.mod", 5)
	require.NoError(t, err)
	assert.NotEmpty(t, commits)
}

func TestGit_ShowFile_NotFound(t *testing.T) {
	t.Parallel()

	cwd, err := os.Getwd()
	require.NoError(t, err)

	ctx := context.Background()
	rootDir, err := git.RootDir(ctx, cwd)
	require.NoError(t, err)

	_, err = git.ShowFile(ctx, rootDir, "HEAD", "non_existent_file_xyz.go")
	assert.Error(t, err)
}
