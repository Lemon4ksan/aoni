// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package openapi_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/codegen/analysis"
	"github.com/lemon4ksan/aoni/internal/codegen/openapi"
	"github.com/lemon4ksan/aoni/internal/codegen/parser"
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
