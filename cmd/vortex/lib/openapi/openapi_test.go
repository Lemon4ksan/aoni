// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package openapi_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/analysis"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/builder"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/openapi"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/parser"
)

const sampleSwaggerJSON = `{
  "swagger": "2.0",
  "info": {
    "title": "Backpack.tf API",
    "version": "1.0.0"
  },
  "host": "backpack.tf",
  "basePath": "/api",
  "schemes": ["https"],
  "paths": {
    "/v2/classifieds/alerts": {
      "get": {
        "summary": "Get classified alerts",
        "operationId": "GetClassifiedsAlerts",
        "parameters": [
          {
            "name": "skip",
            "in": "query",
            "type": "integer",
            "required": false
          },
          {
            "name": "limit",
            "in": "query",
            "type": "integer",
            "required": false
          }
        ],
        "responses": {
          "200": {
            "description": "Alerts response",
            "schema": {
              "$ref": "#/definitions/AlertsResponse"
            }
          }
        }
      },
      "post": {
        "summary": "Create alert",
        "operationId": "CreateAlert",
        "parameters": [
          {
            "name": "body",
            "in": "body",
            "required": true,
            "schema": {
              "$ref": "#/definitions/CreateAlertRequest"
            }
          }
        ],
        "responses": {
          "200": {
            "description": "Created alert response",
            "schema": {
              "$ref": "#/definitions/AlertsResponse"
            }
          }
        }
      }
    },
    "/users/{steamid}/profile": {
      "get": {
        "summary": "Get user profile",
        "operationId": "GetUserProfile",
        "parameters": [
          {
            "name": "steamid",
            "in": "path",
            "type": "string",
            "required": true
          }
        ],
        "responses": {
          "200": {
            "description": "User profile",
            "schema": {
              "$ref": "#/definitions/UserProfile"
            }
          }
        }
      }
    }
  },
  "definitions": {
    "AlertsResponse": {
      "type": "object",
      "properties": {
        "total": {
          "type": "integer"
        },
        "items": {
          "type": "array",
          "items": {
            "type": "string"
          }
        }
      }
    },
    "CreateAlertRequest": {
      "type": "object",
      "properties": {
        "item_name": {
          "type": "string"
        },
        "intent": {
          "type": "string"
        },
        "price": {
          "type": "number"
        }
      }
    },
    "UserProfile": {
      "type": "object",
      "properties": {
        "id": {
          "type": "string"
        },
        "name": {
          "type": "string"
        },
        "reputation": {
          "type": "integer"
        }
      }
    }
  }
}`

func TestOpenAPI_ImportAndExport(t *testing.T) {
	// 1. Load Spec
	doc, err := openapi.LoadSpec("test.json", []byte(sampleSwaggerJSON))
	require.NoError(t, err)
	require.NotNil(t, doc)

	// 2. Generate Aoni Contract (DSL)
	cfg := openapi.ImportConfig{
		PackageName: "bptf",
		ServiceName: "Client",
	}
	src, err := openapi.GenerateContract(doc, cfg)
	require.NoError(t, err)
	require.NotEmpty(t, src)

	srcStr := string(src)
	require.Contains(t, srcStr, "// @aoni:service casing=snake_case")
	require.Contains(t, srcStr, "// @aoni:dto casing=snake_case omitempty=true")
	require.Contains(t, srcStr, "// @get \"v2/classifieds/alerts\"")
	require.Contains(t, srcStr, "// @post \"v2/classifieds/alerts\"")
	require.Contains(t, srcStr, "// @get \"users/{steamid}/profile\"")
	require.Contains(t, srcStr, "type Client interface")

	// 3. Verify that generated Aoni Contract parses cleanly with Aoni Parser and passes Analyzer
	p := parser.NewParser()
	root, err := p.ParseSource("api.go", src)
	require.NoError(t, err)
	require.NotNil(t, root)

	analyzer := analysis.NewAnalyzer()

	diags := analyzer.Analyze(root)
	for _, d := range diags {
		require.NotEqual(
			t,
			analysis.SeverityError,
			d.Severity,
			"Analyzer diagnostic error: %s (%s)",
			d.Message,
			d.Target,
		)
	}

	// 4. Export from Aoni Contract IR back to OpenAPI 3.1
	exportCfg := openapi.ExportConfig{
		Title:   "Backpack.tf API",
		Version: "1.0.0",
		AsYAML:  false,
	}
	exportedJSON, err := openapi.ExportOpenAPI(root, exportCfg)
	require.NoError(t, err)
	require.NotEmpty(t, exportedJSON)

	exportedStr := string(exportedJSON)
	require.Contains(t, exportedStr, "\"openapi\": \"3.1.0\"")
	require.Contains(t, exportedStr, "GetClassifiedsAlerts")
	require.Contains(t, exportedStr, "CreateAlert")
	require.Contains(t, exportedStr, "GetUserProfile")
	require.Contains(t, exportedStr, "AlertsResponse")
}

func TestOpenAPI_LosslessRoundtrip(t *testing.T) {
	src := `package testapi

import (
	"context"
	"github.com/lemon4ksan/aoni"
)

// @aoni:service casing=snake_case
// @base_url "https://api.example.com/v1"
// @persona "chrome_133"
// @tls_spec "chrome_133"
type API interface {
	// GetItem fetches item by id
	// @get "items/{id}"
	// @bind "api_v1_get_item_external"
	// @unwrap "item"
	// @idempotent
	// @etag
	// @deprecated reason="Use GetItemV2 instead" replace="GetItemV2"
	GetItem(ctx context.Context, id string, mods ...aoni.RequestModifier) (*Item, error)
}

// @aoni:dto casing=snake_case omitempty=true
type Item struct {
	ID string ` + "`" + `json:"id"` + "`" + `
	Name string ` + "`" + `json:"name"` + "`" + `
}
`

	p := parser.NewParser()
	root, err := p.ParseSource("api.go", []byte(src))
	require.NoError(t, err)
	require.NotNil(t, root)

	// 1. Export to OpenAPI 3.1 with Vortex extensions
	exportCfg := openapi.ExportConfig{
		Title:   "Test API",
		Version: "1.0.0",
		Vortex:  true,
	}
	specData, err := openapi.ExportOpenAPI(root, exportCfg)
	require.NoError(t, err)

	specStr := string(specData)
	require.Contains(t, specStr, "\"x-vortex-persona\": \"chrome_133\"")
	require.Contains(t, specStr, "\"x-vortex-tlsspec\": \"chrome_133\"")
	require.Contains(t, specStr, "\"x-vortex-unwrap\": \"item\"")
	require.Contains(t, specStr, "\"x-vortex-idempotent\": true")
	require.Contains(t, specStr, "\"x-vortex-etag\": true")
	require.Contains(t, specStr, "\"deprecated\": true")
	require.Contains(t, specStr, "api_v1_get_item_external")

	// 2. Import back from OpenAPI to Go Contract
	doc, err := openapi.LoadSpec("spec.json", specData)
	require.NoError(t, err)

	importCfg := openapi.ImportConfig{
		PackageName: "testapi",
		ServiceName: "API",
	}
	reimportedSrc, err := openapi.GenerateContract(doc, importCfg)
	require.NoError(t, err)

	reimportedStr := string(reimportedSrc)
	require.Contains(t, reimportedStr, "// @persona \"chrome_133\"")
	require.Contains(t, reimportedStr, "// @tls_spec \"chrome_133\"")
	require.Contains(t, reimportedStr, "// @unwrap \"item\"")
	require.Contains(t, reimportedStr, "// @idempotent")
	require.Contains(t, reimportedStr, "// @etag")
	require.Contains(t, reimportedStr, "// @deprecated")
	require.Contains(t, reimportedStr, "// @bind \"api_v1_get_item_external\"")
}

func TestOpenAPI_CleanExportByDefault(t *testing.T) {
	src := `package testapi

import (
	"context"
	"github.com/lemon4ksan/aoni"
)

// @aoni:service
// @persona "chrome_133"
// @tls_spec "chrome_133"
type API interface {
	// @get "items/{id}"
	// @unwrap "data"
	GetItem(ctx context.Context, id string, mods ...aoni.RequestModifier) (map[string]any, error)
}
`

	p := parser.NewParser()
	root, err := p.ParseSource("api.go", []byte(src))
	require.NoError(t, err)

	// Export without Vortex flag (clean by default)
	specData, err := openapi.ExportOpenAPI(root, openapi.ExportConfig{
		Title:   "Clean API",
		Version: "1.0.0",
		Vortex:  false,
	})
	require.NoError(t, err)

	specStr := string(specData)
	require.NotContains(t, specStr, "x-vortex")
	require.Contains(t, specStr, "\"openapi\": \"3.1.0\"")
	require.Contains(t, specStr, "GetItem")
}

func TestOpenAPI_MergeModes(t *testing.T) {
	specA := `{
		"openapi": "3.0.0",
		"info": {"title": "Spec A", "version": "1.0"},
		"paths": {
			"/users": {
				"get": {"operationId": "ListUsers", "responses": {"200": {"description": "ok"}}},
				"post": {"operationId": "CreateUser", "responses": {"201": {"description": "created"}}}
			},
			"/only-a": {
				"get": {"operationId": "GetOnlyA", "responses": {"200": {"description": "ok"}}}
			}
		}
	}`

	specB := `{
		"openapi": "3.0.0",
		"info": {"title": "Spec B", "version": "1.0"},
		"paths": {
			"/users": {
				"get": {"operationId": "ListUsers", "responses": {"200": {"description": "ok"}}}
			},
			"/only-b": {
				"get": {"operationId": "GetOnlyB", "responses": {"200": {"description": "ok"}}}
			}
		}
	}`

	docA, err := openapi.LoadSpec("specA.json", []byte(specA))
	require.NoError(t, err)

	docB, err := openapi.LoadSpec("specB.json", []byte(specB))
	require.NoError(t, err)

	// 1. Union Mode (A ∪ B): /users (get, post), /only-a (get), /only-b (get)
	docUnion := openapi.MergeOpenAPISpecsWithMode(openapi.MergeModeUnion, docA, docB)
	require.NotNil(t, docUnion.Paths.Value("/users").Get)
	require.NotNil(t, docUnion.Paths.Value("/users").Post)
	require.NotNil(t, docUnion.Paths.Value("/only-a"))
	require.NotNil(t, docUnion.Paths.Value("/only-b"))

	// 2. Intersect Mode (A ∩ B): only /users (get)
	docA2, _ := openapi.LoadSpec("specA.json", []byte(specA))
	docB2, _ := openapi.LoadSpec("specB.json", []byte(specB))
	docIntersect := openapi.MergeOpenAPISpecsWithMode(openapi.MergeModeIntersection, docA2, docB2)
	require.NotNil(t, docIntersect.Paths.Value("/users").Get)
	require.Nil(t, docIntersect.Paths.Value("/users").Post)
	require.Nil(t, docIntersect.Paths.Value("/only-a"))
	require.Nil(t, docIntersect.Paths.Value("/only-b"))

	// 3. Diff Mode (A \ B): /users (post), /only-a (get)
	docA3, _ := openapi.LoadSpec("specA.json", []byte(specA))
	docB3, _ := openapi.LoadSpec("specB.json", []byte(specB))
	docDiff := openapi.MergeOpenAPISpecsWithMode(openapi.MergeModeDifference, docA3, docB3)
	require.Nil(t, docDiff.Paths.Value("/users").Get)
	require.NotNil(t, docDiff.Paths.Value("/users").Post)
	require.NotNil(t, docDiff.Paths.Value("/only-a"))
	require.Nil(t, docDiff.Paths.Value("/only-b"))
}

func TestDiscordImport(t *testing.T) {
	data, err := os.ReadFile(
		`C:\Users\senya\.gemini\antigravity\brain\38a522d0-96a7-4c36-af14-1e0509265cb3\scratch\discord_openapi.json`,
	)
	if err != nil {
		t.Skip("discord spec not found")
	}

	doc, err := openapi.LoadSpec("discord.json", data)
	require.NoError(t, err)

	code, err := openapi.GenerateContract(doc, openapi.ImportConfig{
		PackageName: "discord",
		ServiceName: "DiscordAPI",
	})
	require.NoError(t, err)
	require.NotEmpty(t, code)

	for _, line := range strings.Split(string(code), "\n") {
		if strings.Contains(line, "UpdateUserMessage") || strings.Contains(line, "user_id_") {
			t.Logf("MATCH: %s", line)
		}
	}

	tmpContract := filepath.Join(t.TempDir(), "discord_api.go")
	err = os.WriteFile(tmpContract, code, 0o600)
	require.NoError(t, err)

	b := builder.New(builder.Config{})
	res, err := b.BuildFile(t.Context(), tmpContract, "")
	require.NoError(t, err)
	require.False(t, res.Skipped)
	require.Greater(t, res.BytesCount, 0)
	t.Logf("Generated client: %d bytes, %d services, %d structs", res.BytesCount, res.ServicesCount, res.StructsCount)
}
