// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package openapi_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/codegen/openapi"
)

const incomingSpecJSON = `{
  "openapi": "3.1.0",
  "info": {
    "title": "Marketplace API",
    "version": "v1.4.0"
  },
  "paths": {
    "/items/{item_id}": {
      "get": {
        "operationId": "api_v1_items_get",
        "summary": "Fetch item details",
        "parameters": [
          {
            "name": "item_id",
            "in": "path",
            "required": true,
            "schema": {
              "type": "integer"
            }
          },
          {
            "name": "currency",
            "in": "query",
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "description": "OK"
          }
        }
      }
    },
    "/analytics/sales": {
      "get": {
        "operationId": "GetSalesAnalytics",
        "summary": "Sales analytics report",
        "responses": {
          "200": {
            "description": "Analytics"
          }
        }
      }
    }
  }
}`

func TestMerge_TypeFidelityAndDirectiveUnion(t *testing.T) {
	// Existing Go contract:
	// - Developer gave idiomatic method name `GetItem` with custom domain type `itemID id.ID`
	// - Developer attached custom directives `@unwrap "data"` and `@persona "chrome_133"`
	// - Developer has legacy method `OldOrders` which was removed in new spec
	existingGoSrc := `package market

import (
	"context"
	"github.com/lemon4ksan/aoni"
)

// @aoni:service casing=snake_case
// @version "v1.0.0"
type MarketAPI interface {
	// GetItem fetches item details with custom unwrapping.
	// @get "items/{item_id}"
	// @bind "api_v1_items_get"
	// @unwrap "data"
	// @persona "chrome_133"
	GetItem(ctx context.Context, itemID id.ID, mods ...aoni.RequestModifier) (*ItemResponse, error)

	// OldOrders legacy method
	// @get "legacy/orders"
	OldOrders(ctx context.Context, mods ...aoni.RequestModifier) (map[string]any, error)
}
`

	doc, err := openapi.LoadSpec("spec.json", []byte(incomingSpecJSON))
	require.NoError(t, err)

	engine := openapi.NewMergeEngine()
	cfg := openapi.MergeConfig{
		SpecFile:    "spec.json",
		PackageName: "market",
		ServiceName: "MarketAPI",
		Prune:       false,
	}

	mergedBytes, summary, err := engine.ReconcileService([]byte(existingGoSrc), doc, cfg)
	require.NoError(t, err)
	require.NotNil(t, summary)

	mergedStr := string(mergedBytes)

	// 1. Verify Type Fidelity: custom type `itemID id.ID` is preserved!
	require.Contains(t, mergedStr, "itemID id.ID")

	// 2. Verify Directive Union: `@unwrap "data"` and `@persona "chrome_133"` are preserved!
	require.Contains(t, mergedStr, `// @unwrap "data"`)
	require.Contains(t, mergedStr, `// @persona "chrome_133"`)

	// 3. Verify Soft Deprecation: `OldOrders` is marked as `@deprecated` with since="v1.4.0"
	require.Contains(t, mergedStr, `// @deprecated reason="Removed from upstream OpenAPI specification" since="v1.4.0"`)
	require.Contains(t, mergedStr, "OldOrders")

	// 4. Verify New Endpoint & Auto `@since`: `GetSalesAnalytics` was appended with `@since "v1.4.0"`
	require.Contains(t, mergedStr, "GetSalesAnalytics")
	require.Contains(t, mergedStr, `// @since "v1.4.0"`)

	// 5. Verify NO DO NOT EDIT header
	require.NotContains(t, mergedStr, "DO NOT EDIT")

	// 6. Verify Summary metrics
	require.Len(t, summary.AddedMethods, 1)
	require.Len(t, summary.UpdatedMethods, 1)
	require.Len(t, summary.DeprecatedMethods, 1)
	require.Len(t, summary.PrunedMethods, 0)
}

func TestMerge_PruningMode(t *testing.T) {
	existingGoSrc := `package market

import (
	"context"
	"github.com/lemon4ksan/aoni"
)

// @aoni:service
type MarketAPI interface {
	// @get "legacy/orders"
	OldOrders(ctx context.Context, mods ...aoni.RequestModifier) (map[string]any, error)
}
`

	doc, err := openapi.LoadSpec("spec.json", []byte(incomingSpecJSON))
	require.NoError(t, err)

	engine := openapi.NewMergeEngine()
	cfg := openapi.MergeConfig{
		SpecFile: "spec.json",
		Prune:    true, // Prune deleted methods!
	}

	mergedBytes, summary, err := engine.ReconcileService([]byte(existingGoSrc), doc, cfg)
	require.NoError(t, err)

	mergedStr := string(mergedBytes)

	// OldOrders must be completely pruned
	require.NotContains(t, mergedStr, "OldOrders")
	require.Len(t, summary.PrunedMethods, 1)
	require.Equal(t, "OldOrders", summary.PrunedMethods[0])
}
