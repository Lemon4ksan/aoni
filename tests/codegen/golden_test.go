// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package codegen_test

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/codegen/builder"
)

var updateGolden = flag.Bool("update", false, "Update golden test files with latest generator output")

func TestGolden_Contracts(t *testing.T) {
	goldenDir := filepath.Join("testdata", "golden")
	entries, err := os.ReadDir(goldenDir)
	if err != nil {
		t.Skipf("Golden directory not found: %v", err)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), ".golden.go") {
			continue
		}

		testName := strings.TrimSuffix(entry.Name(), ".go")
		srcFile := filepath.Join(goldenDir, entry.Name())
		goldenFile := filepath.Join(goldenDir, testName+".golden.go")

		t.Run(testName, func(t *testing.T) {
			b := builder.New(builder.Config{
				DryRun: true,
			})

			res, err := b.BuildFile(context.Background(), srcFile, "")
			require.NoError(t, err, "Generator must succeed on %s", srcFile)
			require.NotEmpty(t, res.Code, "Emitted code must not be empty")

			if *updateGolden {
				require.NoError(t, os.WriteFile(goldenFile, res.Code, 0o600))
				t.Logf("Updated golden file: %s", goldenFile)
				return
			}

			goldenBytes, err := os.ReadFile(goldenFile)
			if os.IsNotExist(err) {
				// If golden file does not exist yet, create it on first run
				require.NoError(t, os.WriteFile(goldenFile, res.Code, 0o600))
				t.Logf("Created new golden file: %s", goldenFile)
				return
			}
			require.NoError(t, err)

			assert.Equal(
				t,
				string(goldenBytes),
				string(res.Code),
				"Emitted code for %s does not match golden file %s. Run 'go test -update ./tests/codegen' if changes are intended.",
				srcFile,
				goldenFile,
			)
		})
	}
}
