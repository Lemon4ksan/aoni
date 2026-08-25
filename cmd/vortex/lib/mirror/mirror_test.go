// Copyright 2026 The Aoni Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package mirror_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/mirror"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/parser"
)

func TestMirror_DriftDetection(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Create pure legacy Go file (Root of truth, zero @aoni tags)
	legacyDir := filepath.Join(tempDir, "internal", "legacy", "steam")
	require.NoError(t, os.MkdirAll(legacyDir, 0o750))
	legacyFile := filepath.Join(legacyDir, "inventory.go")
	require.NoError(t, os.WriteFile(legacyFile, []byte(`package steam

import "context"

type LegacyItem struct {
	AssetID  uint64
	Name     string
	Tradable bool
}

type LegacyInventoryService interface {
	GetInventory(ctx context.Context, steamID uint64) ([]*LegacyItem, error)
	TransferItem(ctx context.Context, assetID uint64, toSteamID uint64) error
}
`), 0o600))

	// 2. Create Aoni wrapper with intentional parameter drift (steamID as string instead of uint64)
	wrapperDir := filepath.Join(tempDir, "pkg", "steam", "inventory")
	require.NoError(t, os.MkdirAll(wrapperDir, 0o750))
	wrapperFile := filepath.Join(wrapperDir, "api.go")
	require.NoError(t, os.WriteFile(wrapperFile, []byte(`package inventory

import (
	"context"
	"github.com/lemon4ksan/aoni"
)

type Item struct {
	AssetID  string // Drift: string instead of uint64
	Name     string
}

// @aoni:service
// @aoni:mirror "internal/legacy/steam/inventory.go:LegacyInventoryService"
type InventoryWrapperAPI interface {
	// @get "inventory"
	GetInventory(ctx context.Context, steamID string, mods ...aoni.RequestModifier) ([]*Item, error)
}
`), 0o600))

	p := parser.NewParser()
	root, err := p.ParseFile(wrapperFile)
	require.NoError(t, err)
	require.Len(t, root.Services, 1)
	require.NotNil(t, root.Services[0].Mirror)

	// Run Mirror Check
	diags, err := mirror.CheckService(tempDir, wrapperFile, root.Services[0], root.Structs)
	require.NoError(t, err)

	// Expect 2 drift diagnostics:
	// 1. GetInventory parameter mismatch (uint64 vs string)
	// 2. Item struct AssetID field mismatch (uint64 vs string)
	require.NotEmpty(t, diags)

	var foundParamDrift, foundFieldDrift bool
	for _, d := range diags {
		if d.Kind == mirror.DriftParamMismatch {
			foundParamDrift = true

			require.Contains(t, d.Message, "Signature drift in InventoryWrapperAPI.GetInventory")
		}

		if d.Kind == mirror.DriftFieldMismatch {
			foundFieldDrift = true

			require.Contains(t, d.Message, "Struct drift in Item.AssetID")
		}
	}

	require.True(t, foundParamDrift, "Expected parameter drift diagnostic")
	require.True(t, foundFieldDrift, "Expected struct field drift diagnostic")
}
