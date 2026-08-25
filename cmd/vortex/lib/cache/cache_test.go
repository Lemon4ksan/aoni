// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/cache"
)

func TestSecretsVault(t *testing.T) {
	tempDir := t.TempDir()

	vault, vaultPath, err := cache.LoadSecrets(tempDir)
	require.NoError(t, err)
	require.NotNil(t, vault)

	// 1. Set secrets
	vault.Set(
		"AUTH_TOKEN",
		"ya29.a0AdMD6EhMjTB36W8ICAIX7zzH1AtrrPxV_hjt06G9J5A1Velpm4CfeIuPIfTf8HDObALczKcFFajQ4fZ",
		"test_har",
	)
	vault.Set("GOOGLE_API_KEY", "AIzaSyD-mock-api-key-value-12345", "aistudio")

	// 2. Get secrets
	val, ok := vault.Get("AUTH_TOKEN")
	require.True(t, ok)
	require.Contains(t, val, "ya29.")

	// 3. Check All and masking
	all := vault.All()
	require.Len(t, all, 2)

	for _, entry := range all {
		if entry.Key == "AUTH_TOKEN" {
			require.Contains(t, entry.Masked, "ya29...")
		}
	}

	// 4. Save and Reload
	require.NoError(t, vault.Save(vaultPath))
	require.FileExists(t, vaultPath)

	reloaded, _, err := cache.LoadSecrets(tempDir)
	require.NoError(t, err)

	val2, ok2 := reloaded.Get("GOOGLE_API_KEY")
	require.True(t, ok2)
	require.Equal(t, "AIzaSyD-mock-api-key-value-12345", val2)

	// 5. Delete and Clear
	require.True(t, reloaded.Delete("GOOGLE_API_KEY"))
	require.False(t, reloaded.Delete("NON_EXISTENT"))
	require.Len(t, reloaded.All(), 1)

	reloaded.Clear()
	require.Empty(t, reloaded.All())
}

func TestTrafficStore(t *testing.T) {
	tempDir := t.TempDir()

	harData := []byte(`{
		"log": {
			"version": "1.2",
			"entries": [
				{
					"request": {
						"method": "POST",
						"url": "https://api.example.com/v1/models",
						"headers": [
							{"name": "Authorization", "value": "Bearer secret_token_12345"},
							{"name": "x-goog-api-key", "value": "AIzaSyKey123"},
							{"name": "Cookie", "value": "session=sensitive_cookie"}
						]
					},
					"response": {
						"status": 200,
						"headers": [
							{"name": "Content-Type", "value": "application/json"},
							{"name": "Set-Cookie", "value": "sid=123; HttpOnly"}
						]
					}
				}
			]
		}
	}`)

	srcPath := filepath.Join(tempDir, "sample.har")
	require.NoError(t, os.WriteFile(srcPath, harData, 0o600))

	// 1. Store traffic with sanitization and move
	entry, secrets, err := cache.StoreTraffic(tempDir, srcPath, harData, true, true)
	require.NoError(t, err)
	require.NotNil(t, entry)
	require.Equal(t, "sample", entry.ID)
	require.Equal(t, 1, entry.EndpointCount)
	require.True(t, entry.Sanitized)

	// Source file should have been moved/deleted
	require.NoFileExists(t, srcPath)

	// Check extracted secrets
	require.Equal(t, "secret_token_12345", secrets["AUTH_TOKEN"].Value)
	require.Equal(t, "AIzaSyKey123", secrets["GOOGLE_API_KEY"].Value)

	// 2. Retrieve traffic from cache
	decompressed, retrievedEntry, err := cache.GetTraffic(tempDir, "sample")
	require.NoError(t, err)
	require.NotNil(t, retrievedEntry)
	require.Contains(t, string(decompressed), "https://api.example.com/v1/models")
	require.Contains(t, string(decompressed), "Bearer ${AUTH_TOKEN}")
	require.NotContains(t, string(decompressed), "secret_token_12345")
	require.NotContains(t, string(decompressed), "sensitive_cookie")

	// 3. Test fuzzy matching
	_, _, err = cache.GetTraffic(tempDir, "samp")
	require.NoError(t, err)

	// 4. List traffic
	list, err := cache.ListTraffic(tempDir)
	require.NoError(t, err)
	require.Len(t, list, 1)

	// 5. Prune traffic
	pruned, err := cache.PruneTraffic(tempDir, time.Hour, true)
	require.NoError(t, err)
	require.Equal(t, 1, pruned)

	listAfter, err := cache.ListTraffic(tempDir)
	require.NoError(t, err)
	require.Empty(t, listAfter)
}
