// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package diff_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/codegen/diff"
	"github.com/lemon4ksan/aoni/internal/codegen/openapi"
	"github.com/lemon4ksan/aoni/internal/codegen/parser"
)

const remoteSpecJSON = `{
  "openapi": "3.1.0",
  "info": {
    "title": "Store API",
    "version": "1.0.0"
  },
  "paths": {
    "/items/{item_id}": {
      "get": {
        "summary": "Get item by ID",
        "operationId": "GetItem",
        "parameters": [
          {
            "name": "item_id",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string"
            }
          },
          {
            "name": "currency",
            "in": "query",
            "required": true,
            "schema": {
              "type": "integer"
            }
          },
          {
            "name": "detail",
            "in": "query",
            "required": false,
            "schema": {
              "type": "boolean"
            }
          }
        ],
        "responses": {
          "200": {
            "description": "Item details"
          }
        }
      }
    },
    "/orders": {
      "post": {
        "summary": "Create order",
        "operationId": "CreateOrder",
        "deprecated": true,
        "responses": {
          "200": {
            "description": "Order created"
          }
        }
      }
    },
    "/legacy/stats": {
      "get": {
        "summary": "Legacy stats",
        "responses": {
          "200": {
            "description": "Stats"
          }
        }
      }
    }
  }
}`

func TestDiff_ExactSync(t *testing.T) {
	goSrc := `package store

import (
	"context"
	"github.com/lemon4ksan/aoni"
)

// @aoni:service
type StoreAPI interface {
	// @get "items/{item_id}"
	GetItem(ctx context.Context, itemID string, currency int, detail bool, mods ...aoni.RequestModifier) (map[string]any, error)

	// @post "orders"
	// @deprecated
	CreateOrder(ctx context.Context, mods ...aoni.RequestModifier) (map[string]any, error)

	// @get "legacy/stats"
	LegacyStats(ctx context.Context, mods ...aoni.RequestModifier) (map[string]any, error)
}
`

	p := parser.NewParser()
	root, err := p.ParseSource("api.go", []byte(goSrc))
	require.NoError(t, err)

	doc, err := openapi.LoadSpec("spec.json", []byte(remoteSpecJSON))
	require.NoError(t, err)

	engine := diff.NewEngine()
	report := engine.Compare(root, doc, "store/api.go", "spec.json")

	require.NotNil(t, report)
	require.False(t, report.HasBreaking())
	require.Equal(t, 0, report.BreakingCount())
	require.Equal(t, 0, report.GhostCount())

	rendered := report.Render(false)
	require.Contains(t, rendered, "100% in sync")
}

func TestDiff_BreakingAndGhost(t *testing.T) {
	// Local contract is missing required query param 'currency', missing endpoint '/legacy/stats',
	// and has extra ghost endpoint '/ghost/test'.
	goSrc := `package store

import (
	"context"
	"github.com/lemon4ksan/aoni"
)

// @aoni:service
type StoreAPI interface {
	// Missing required 'currency' parameter, and has type mismatch on detail (string vs bool)
	// @get "items/{item_id}"
	GetItem(ctx context.Context, itemID string, detail string, mods ...aoni.RequestModifier) (map[string]any, error)

	// Not deprecated locally
	// @post "orders"
	CreateOrder(ctx context.Context, mods ...aoni.RequestModifier) (map[string]any, error)

	// Ghost endpoint only in Go
	// @get "ghost/test"
	GhostEndpoint(ctx context.Context, mods ...aoni.RequestModifier) (map[string]any, error)
}
`

	p := parser.NewParser()
	root, err := p.ParseSource("api.go", []byte(goSrc))
	require.NoError(t, err)

	doc, err := openapi.LoadSpec("spec.json", []byte(remoteSpecJSON))
	require.NoError(t, err)

	engine := diff.NewEngine()
	report := engine.Compare(root, doc, "store/api.go", "spec.json")

	require.NotNil(t, report)
	require.True(t, report.HasBreaking())
	require.True(t, report.HasDrift())
	require.Greater(t, report.BreakingCount(), 0)
	require.Greater(t, report.GhostCount(), 0)

	rendered := report.Render(false)
	require.Contains(t, rendered, "Breaking Changes")
	require.Contains(t, rendered, "Ghost Endpoints")
	require.Contains(t, rendered, "Non-Breaking Drifts")

	jsonBytes, err := report.RenderJSON()
	require.NoError(t, err)
	require.Contains(t, string(jsonBytes), "\"severity\": \"BREAKING\"")
	require.Contains(t, string(jsonBytes), "\"severity\": \"GHOST\"")
}

func TestDiff_SpecToSpecComparison(t *testing.T) {
	baseSpecJSON := `{
		"openapi": "3.0.3",
		"info": {"title": "Base Spec", "version": "1.0.0"},
		"paths": {
			"/users/{id}": {
				"get": {
					"parameters": [{"name": "id", "in": "path", "required": true, "schema": {"type": "string"}}],
					"responses": {"200": {"description": "OK"}}
				}
			},
			"/old/route": {
				"get": {
					"responses": {"200": {"description": "OK"}}
				}
			}
		}
	}`

	headSpecJSON := `{
		"openapi": "3.0.3",
		"info": {"title": "Head Spec", "version": "2.0.0"},
		"paths": {
			"/users/{id}": {
				"get": {
					"parameters": [
						{"name": "id", "in": "path", "required": true, "schema": {"type": "string"}},
						{"name": "filter", "in": "query", "required": false, "schema": {"type": "string"}},
						{"name": "auth_token", "in": "query", "required": true, "schema": {"type": "string"}}
					],
					"responses": {"200": {"description": "OK"}}
				}
			},
			"/new/route": {
				"post": {
					"responses": {"200": {"description": "OK"}}
				}
			}
		}
	}`

	baseDoc, err := openapi.LoadSpec("base.json", []byte(baseSpecJSON))
	require.NoError(t, err)

	headDoc, err := openapi.LoadSpec("head.json", []byte(headSpecJSON))
	require.NoError(t, err)

	engine := diff.NewEngine()
	report := engine.CompareSpecs(baseDoc, headDoc, "base.json", "head.json")

	require.NotNil(t, report)
	require.True(t, report.HasBreaking())
	require.True(t, report.HasDrift())

	// 1. /old/route was removed -> Breaking
	// 2. /users/{id} added required query param 'auth_token' -> Breaking
	require.Equal(t, 2, report.BreakingCount())

	// 3. /new/route was added -> Ghost (Addition)
	require.Equal(t, 1, report.GhostCount())

	// 4. /users/{id} added optional query param 'filter' -> Non-Breaking
	require.Equal(t, 1, report.NonBreakingCount())

	rendered := report.Render(false)
	require.Contains(t, rendered, "Endpoint GET /old/route was removed")
	require.Contains(t, rendered, "added required query parameter \"auth_token\"")
	require.Contains(t, rendered, "Endpoint POST /new/route was added")
}
