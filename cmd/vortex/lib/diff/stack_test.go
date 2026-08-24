// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package diff

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/require"
)

func TestDiffStack_Lifecycle(t *testing.T) {
	tempDir := t.TempDir()

	file1 := filepath.Join(tempDir, "api.go")
	specFile := filepath.Join(tempDir, "spec.json")

	v1Go := `package testapi
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
	v1Spec := `{"openapi": "3.0.0", "info": {"title": "Raw API"}}`

	require.NoError(t, os.WriteFile(file1, []byte(v1Go), 0o600))
	require.NoError(t, os.WriteFile(specFile, []byte(v1Spec), 0o600))

	// 1. Initialize & Push Frame 0 (Base)
	stack, err := LoadStack(tempDir)
	require.NoError(t, err)
	require.Empty(t, stack.Frames)

	frame0, err := stack.Push("initial-scan", []string{"api.go", "spec.json"}, []string{"base", "raw"}, nil)
	require.NoError(t, err)
	require.Equal(t, 0, frame0.Index)
	require.Equal(t, "initial-scan", frame0.Label)
	require.Len(t, stack.List(), 1)

	// 2. Modify files -> Push Frame 1 (Renamed Field0)
	v2Go := `package testapi
// @aoni:service
type API interface {
	// @post "rpc/Generate"
	RPCMethod1(ctx context.Context, req *Req) (*Resp, error)
}
// @aoni:tuple
type Req struct {
	Model  string ` + "`" + `aoni:"0"` + "`" + `
	Field4 int    ` + "`" + `aoni:"4"` + "`" + `
}
`
	require.NoError(t, os.WriteFile(file1, []byte(v2Go), 0o600))

	frame1, err := stack.Push("renamed-model", []string{"api.go"}, []string{"tuple"}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, frame1.Index)
	require.Equal(t, "renamed-model", frame1.Label)
	require.Len(t, stack.List(), 2)

	// 3. Modify files -> Push Frame 2 (Renamed Field4 and RPCMethod1)
	v3Go := `package testapi
// @aoni:service
type API interface {
	// @post "rpc/Generate"
	GenerateContent(ctx context.Context, req *Req) (*Resp, error)
}
// @aoni:tuple
type Req struct {
	Model           string ` + "`" + `aoni:"0"` + "`" + `
	MaxOutputTokens int    ` + "`" + `aoni:"4"` + "`" + `
}
`
	require.NoError(t, os.WriteFile(file1, []byte(v3Go), 0o600))

	frame2, err := stack.Push("final-polish", []string{"api.go"}, []string{"methods", "tuples"}, nil)
	require.NoError(t, err)
	require.Equal(t, 2, frame2.Index)
	require.Len(t, stack.List(), 3)

	// 4. Test Adjacent Diff (Frame 2 vs Frame 1)
	adjDiff, err := stack.DiffAdjacent()
	require.NoError(t, err)
	require.False(t, adjDiff.IsIdentical)
	require.Equal(t, 1, adjDiff.FromIndex)
	require.Equal(t, 2, adjDiff.ToIndex)
	require.Len(t, adjDiff.ASTEvolution.TupleRenames, 1)
	require.Equal(t, "Field4", adjDiff.ASTEvolution.TupleRenames[0].OldField)
	require.Equal(t, "MaxOutputTokens", adjDiff.ASTEvolution.TupleRenames[0].NewField)
	require.Len(t, adjDiff.ASTEvolution.MethodRenames, 1)
	require.Equal(t, "RPCMethod1", adjDiff.ASTEvolution.MethodRenames[0].OldMethod)
	require.Equal(t, "GenerateContent", adjDiff.ASTEvolution.MethodRenames[0].NewMethod)

	// 5. Test Cumulative Diff (Frame 2 vs Frame 0 Base)
	cumDiff, err := stack.DiffCumulative()
	require.NoError(t, err)
	require.False(t, cumDiff.IsIdentical)
	require.Equal(t, 0, cumDiff.FromIndex)
	require.Equal(t, "initial-scan", cumDiff.FromLabel)
	require.Equal(t, 2, cumDiff.ToIndex)
	require.Equal(t, "final-polish", cumDiff.ToLabel)

	// Both Field0->Model and Field4->MaxOutputTokens must be detected in cumulative
	require.Len(t, cumDiff.ASTEvolution.TupleRenames, 2)

	fieldMap := make(map[string]string)
	for _, tr := range cumDiff.ASTEvolution.TupleRenames {
		fieldMap[tr.Tag] = tr.NewField
	}

	require.Equal(t, "Model", fieldMap["0"])
	require.Equal(t, "MaxOutputTokens", fieldMap["4"])

	// 6. Test Pop with Restore
	popped, err := stack.Pop(true)
	require.NoError(t, err)
	require.Equal(t, "final-polish", popped.Label)
	require.Len(t, stack.List(), 2)

	// Verify file was restored to Frame 1 content
	restoredContent, err := os.ReadFile(file1)
	require.NoError(t, err)
	require.Contains(t, string(restoredContent), "Model  string")
	require.Contains(t, string(restoredContent), "Field4 int")
	require.Contains(t, string(restoredContent), "RPCMethod1")
}

func TestDiffStack_Persistence(t *testing.T) {
	tempDir := t.TempDir()
	f := filepath.Join(tempDir, "doc.txt")
	require.NoError(t, os.WriteFile(f, []byte("hello world"), 0o600))

	stack1, err := LoadStack(tempDir)
	require.NoError(t, err)
	_, err = stack1.Push("v1", []string{"doc.txt"}, []string{"txt"}, nil)
	require.NoError(t, err)

	// Reload from new instance
	stack2, err := LoadStack(tempDir)
	require.NoError(t, err)
	require.Len(t, stack2.List(), 1)
	require.Equal(t, "v1", stack2.Peek().Label)
	require.Contains(t, stack2.Peek().Files["doc.txt"], "hello world")
}

func TestDiffStack_PopToAndDiagram(t *testing.T) {
	tempDir := t.TempDir()
	f := filepath.Join(tempDir, "data.json")

	require.NoError(t, os.WriteFile(f, []byte(`{"version": 1}`), 0o600))

	stack, err := LoadStack(tempDir)
	require.NoError(t, err)

	_, err = stack.Push("base-frame", []string{"data.json"}, []string{"init"}, nil)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(f, []byte(`{"version": 2}`), 0o600))

	_, err = stack.Push("step-2", []string{"data.json"}, []string{"v2"}, nil)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(f, []byte(`{"version": 3}`), 0o600))

	_, err = stack.Push("step-3", []string{"data.json"}, []string{"v3"}, nil)
	require.NoError(t, err)

	diagram := stack.RenderStackDiagram()
	require.Contains(t, diagram, "base-frame")
	require.Contains(t, diagram, "step-2")
	require.Contains(t, diagram, "step-3")
	require.Contains(t, diagram, "[CURRENT HEAD]")
	require.Contains(t, diagram, "[BASE]")

	// PopTo step-2
	popped, err := stack.PopTo("step-2", true)
	require.NoError(t, err)
	require.Len(t, popped, 1)
	require.Equal(t, "step-3", popped[0].Label)
	require.Len(t, stack.List(), 2)

	content, err := os.ReadFile(f)
	require.NoError(t, err)
	require.Contains(t, string(content), `"version": 2`)
}
