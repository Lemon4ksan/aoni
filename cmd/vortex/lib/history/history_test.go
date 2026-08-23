// Copyright 2026 The Aoni Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package history_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/history"
)

func TestHistory_RecordAndUndo(t *testing.T) {
	tempDir := t.TempDir()

	fileA := filepath.Join(tempDir, "api.go")
	require.NoError(t, os.WriteFile(fileA, []byte("original content"), 0o600))

	// 1. Record pre-flight snapshot
	entry, err := history.Record(tempDir, "vortex refactor rename", []string{fileA})
	require.NoError(t, err)
	require.NotEmpty(t, entry.ID)

	// 2. Modify file on disk
	require.NoError(t, os.WriteFile(fileA, []byte("modified content"), 0o600))

	// 3. List operations
	list, err := history.List(tempDir)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, entry.ID, list[0].ID)

	// 4. Undo operation
	undone, err := history.Undo(context.Background(), tempDir, "")
	require.NoError(t, err)
	require.Equal(t, entry.ID, undone.ID)

	// 5. Verify original content restored
	restored, err := os.ReadFile(fileA)
	require.NoError(t, err)
	require.Equal(t, "original content", string(restored))
}
