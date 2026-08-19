// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package builder_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/codegen/builder"
)

func TestBuilder_BuildFile(t *testing.T) {
	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "service.go")
	outFile := filepath.Join(tmpDir, "service.gen.go")

	src := `package testapi

// @aoni:service
// @base_url "https://api.example.com"
type UserService interface {
	// @get /users/{id}
	GetUser(ctx context.Context, id string) (string, error)
}
`
	err := os.WriteFile(srcFile, []byte(src), 0o600)
	require.NoError(t, err)

	b := builder.New(builder.Config{})
	res, err := b.BuildFile(context.Background(), srcFile, outFile)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.False(t, res.Skipped)
	require.Equal(t, 1, res.ServicesCount)
	require.FileExists(t, outFile)

	content, err := os.ReadFile(outFile)
	require.NoError(t, err)
	require.Contains(t, string(content), "type userServiceClient struct")
}

func TestBuilder_BuildFile_SkipsNonContract(t *testing.T) {
	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "plain.go")

	src := `package testapi

type PlainModel struct {
	Name string
}
`
	err := os.WriteFile(srcFile, []byte(src), 0o600)
	require.NoError(t, err)

	b := builder.New(builder.Config{})
	res, err := b.BuildFile(context.Background(), srcFile, "")
	require.NoError(t, err)
	require.NotNil(t, res)
	require.True(t, res.Skipped)
}

func TestBuilder_CollectInputFiles(t *testing.T) {
	tmpDir := t.TempDir()
	f1 := filepath.Join(tmpDir, "api.go")
	f2 := filepath.Join(tmpDir, "api_test.go")
	f3 := filepath.Join(tmpDir, "api.gen.go")

	require.NoError(t, os.WriteFile(f1, []byte("package api"), 0o600))
	require.NoError(t, os.WriteFile(f2, []byte("package api"), 0o600))
	require.NoError(t, os.WriteFile(f3, []byte("package api"), 0o600))

	files := builder.CollectInputFiles("", []string{tmpDir + "/..."})
	require.Len(t, files, 1)
	require.Equal(t, f1, files[0])
}

func TestBuilder_CollectInputFiles_DepthLimit(t *testing.T) {
	tmpDir := t.TempDir()

	// Level 1
	l1File := filepath.Join(tmpDir, "level1.go")
	require.NoError(t, os.WriteFile(l1File, []byte("package l1"), 0o600))

	// Level 3: a/b/c/level3.go
	l3Dir := filepath.Join(tmpDir, "a", "b", "c")
	require.NoError(t, os.MkdirAll(l3Dir, 0o750))
	l3File := filepath.Join(l3Dir, "level3.go")
	require.NoError(t, os.WriteFile(l3File, []byte("package l3"), 0o600))

	// Collect with MaxDepth = 1 (should only find level1.go)
	files := builder.CollectInputFiles("", []string{tmpDir + "/..."}, builder.CollectOptions{
		MaxDepth: 1,
	})
	require.Len(t, files, 1)
	require.Equal(t, l1File, files[0])

	// Collect with MaxDepth = 5 (should find both)
	filesAll := builder.CollectInputFiles("", []string{tmpDir + "/..."}, builder.CollectOptions{
		MaxDepth: 5,
	})
	require.Len(t, filesAll, 2)
}

func TestBuilder_QuickCheckCandidate(t *testing.T) {
	tmpDir := t.TempDir()

	plainFile := filepath.Join(tmpDir, "plain.go")
	require.NoError(t, os.WriteFile(plainFile, []byte("package test\ntype User struct{}\n"), 0o600))
	require.False(t, builder.QuickCheckCandidate(plainFile))

	serviceFile := filepath.Join(tmpDir, "svc.go")
	require.NoError(
		t,
		os.WriteFile(serviceFile, []byte("package test\n// @aoni:service\ntype Svc interface{}\n"), 0o600),
	)
	require.True(t, builder.QuickCheckCandidate(serviceFile))

	mirrorFile := filepath.Join(tmpDir, "mirror.go")
	require.NoError(
		t,
		os.WriteFile(mirrorFile, []byte("package test\n// @mirror \"api.go\"\ntype Svc interface{}\n"), 0o600),
	)
	require.True(t, builder.QuickCheckCandidate(mirrorFile))
}
