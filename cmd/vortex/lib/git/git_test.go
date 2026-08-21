// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package git_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/git"
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
	assert.NotEmpty(t, commits[0].Hash)
	assert.NotEmpty(t, commits[0].Author)
	assert.NotEmpty(t, commits[0].Date)
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

func TestGit_ShowFile_InvalidRef(t *testing.T) {
	t.Parallel()

	cwd, err := os.Getwd()
	require.NoError(t, err)

	ctx := context.Background()
	rootDir, err := git.RootDir(ctx, cwd)
	require.NoError(t, err)

	_, err = git.ShowFile(ctx, rootDir, "invalid_ref_non_existent_commit_12345", "go.mod")
	assert.Error(t, err)
}

func TestGit_RootDir_NonGitDirectory(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	ctx := context.Background()

	_, err := git.RootDir(ctx, tempDir)
	assert.Error(t, err)
}

func TestGit_ListProposalBranches(t *testing.T) {
	t.Parallel()

	cwd, err := os.Getwd()
	require.NoError(t, err)

	ctx := context.Background()
	rootDir, err := git.RootDir(ctx, cwd)
	require.NoError(t, err)

	// List with custom and default prefixes
	proposals, err := git.ListProposalBranches(ctx, rootDir, []string{"feat/", "chore/", "fix/", "main"})
	require.NoError(t, err)

	_ = proposals
}

func TestGit_LogCommits_LimitZero(t *testing.T) {
	t.Parallel()

	cwd, err := os.Getwd()
	require.NoError(t, err)

	ctx := context.Background()
	rootDir, err := git.RootDir(ctx, cwd)
	require.NoError(t, err)

	commits, err := git.LogCommits(ctx, rootDir, "go.mod", 0)
	require.NoError(t, err)
	assert.NotEmpty(t, commits)
}

func TestGit_IsClean(t *testing.T) {
	t.Parallel()

	cwd, err := os.Getwd()
	require.NoError(t, err)

	ctx := context.Background()
	rootDir, err := git.RootDir(ctx, cwd)
	require.NoError(t, err)

	clean, err := git.IsClean(ctx, rootDir, "go.mod")
	require.NoError(t, err)

	_ = clean
}

func TestGit_ContextTimeout(t *testing.T) {
	t.Parallel()

	cwd, err := os.Getwd()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	time.Sleep(2 * time.Millisecond) // Ensure context is expired

	_, err = git.RootDir(ctx, cwd)
	assert.Error(t, err)
}

func TestGit_NormalizePaths(t *testing.T) {
	t.Parallel()

	cwd, err := os.Getwd()
	require.NoError(t, err)

	ctx := context.Background()
	rootDir, err := git.RootDir(ctx, cwd)
	require.NoError(t, err)

	// Nested path with forward slashes and backslashes
	rel := filepath.Join("internal", "version", "version.go")
	data, err := git.ShowFile(ctx, rootDir, "HEAD", rel)
	require.NoError(t, err)
	assert.Contains(t, string(data), "package version")
}
