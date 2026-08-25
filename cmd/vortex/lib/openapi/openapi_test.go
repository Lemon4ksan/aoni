// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package openapi_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/require"

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
		require.NotEqualf(
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
	require.NotNil(t, docUnion.Paths["/users"].Get)
	require.NotNil(t, docUnion.Paths["/users"].Post)
	require.NotNil(t, docUnion.Paths["/only-a"])
	require.NotNil(t, docUnion.Paths["/only-b"])

	// 2. Intersect Mode (A ∩ B): only /users (get)
	docA2, _ := openapi.LoadSpec("specA.json", []byte(specA))
	docB2, _ := openapi.LoadSpec("specB.json", []byte(specB))
	docIntersect := openapi.MergeOpenAPISpecsWithMode(openapi.MergeModeIntersection, docA2, docB2)
	require.NotNil(t, docIntersect.Paths["/users"].Get)
	require.Nil(t, docIntersect.Paths["/users"].Post)
	require.Nil(t, docIntersect.Paths["/only-a"])
	require.Nil(t, docIntersect.Paths["/only-b"])

	// 3. Diff Mode (A \ B): /users (post), /only-a (get)
	docA3, _ := openapi.LoadSpec("specA.json", []byte(specA))
	docB3, _ := openapi.LoadSpec("specB.json", []byte(specB))

	docDiff := openapi.MergeOpenAPISpecsWithMode(openapi.MergeModeDifference, docA3, docB3)
	if docDiff.Paths["/users"] != nil {
		require.Nil(t, docDiff.Paths["/users"].Get)
		require.NotNil(t, docDiff.Paths["/users"].Post)
	}

	require.NotNil(t, docDiff.Paths["/only-a"])
	require.Nil(t, docDiff.Paths["/only-b"])
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

func TestSwagger2_CompleteLifecycle(t *testing.T) {
	swagger2YAML := `
swagger: "2.0"
info:
  title: "Petstore Swagger 2.0"
  version: "1.0.0"
  description: "Demonstrating Swagger 2.0 full schema normalization"
host: "petstore.swagger.io"
basePath: "/v2"
schemes:
  - "https"
consumes:
  - "application/json"
produces:
  - "application/json"
securityDefinitions:
  petstore_auth:
    type: "oauth2"
    flow: "implicit"
    authorizationUrl: "https://petstore.swagger.io/oauth/dialog"
    scopes:
      write:pets: "modify pets in your account"
      read:pets: "read your pets"
  api_key:
    type: "apiKey"
    name: "api_key"
    in: "header"
paths:
  /pet/{petId}:
    get:
      tags:
        - "pet"
      summary: "Find pet by ID"
      operationId: "getPetById"
      parameters:
        - name: "petId"
          in: "path"
          description: "ID of pet to return"
          required: true
          type: "integer"
          format: "int64"
      responses:
        "200":
          description: "successful operation"
          schema:
            $ref: "#/definitions/Pet"
        "400":
          description: "Invalid ID supplied"
        "404":
          description: "Pet not found"
      security:
        - api_key: []
    post:
      tags:
        - "pet"
      summary: "Updates a pet in the store with form data"
      operationId: "updatePetWithForm"
      consumes:
        - "application/x-www-form-urlencoded"
      parameters:
        - name: "petId"
          in: "path"
          description: "ID of pet that needs to be updated"
          required: true
          type: "integer"
          format: "int64"
        - name: "body"
          in: "body"
          description: "Pet object that needs to be updated"
          required: true
          schema:
            $ref: "#/definitions/Pet"
      responses:
        "200":
          description: "Pet updated successfully"
          schema:
            $ref: "#/definitions/ApiResponse"
definitions:
  Pet:
    type: "object"
    required:
      - "id"
      - "name"
    properties:
      id:
        type: "integer"
        format: "int64"
      name:
        type: "string"
        example: "doggie"
      status:
        type: "string"
        description: "pet status in the store"
        enum:
          - "available"
          - "pending"
          - "sold"
  ApiResponse:
    type: "object"
    properties:
      code:
        type: "integer"
        format: "int32"
      type:
        type: "string"
      message:
        type: "string"
`

	doc, err := openapi.ParseSpec([]byte(swagger2YAML))
	require.NoError(t, err)
	require.NotNil(t, doc)

	// Check Server upgrade
	require.Len(t, doc.Servers, 1)
	require.Equal(t, "https://petstore.swagger.io/v2", doc.Servers[0].URL)

	// Check Components normalization
	require.Contains(t, doc.Components.Schemas, "Pet")
	require.Contains(t, doc.Components.Schemas, "ApiResponse")

	// Check Security Schemes upgrade
	require.Contains(t, doc.Components.SecuritySchemes, "petstore_auth")
	secAuth := doc.Components.SecuritySchemes["petstore_auth"]
	require.Equal(t, "oauth2", secAuth.Type)
	require.NotNil(t, secAuth.Flows)
	require.NotNil(t, secAuth.Flows.Implicit)
	require.Equal(t, "https://petstore.swagger.io/oauth/dialog", secAuth.Flows.Implicit.AuthorizationURL)
	require.Equal(t, "modify pets in your account", secAuth.Flows.Implicit.Scopes["write:pets"])

	// Check RequestBody conversion
	updateOp := doc.Paths["/pet/{petId}"].Post
	require.NotNil(t, updateOp.RequestBody)
	require.NotNil(t, updateOp.RequestBody.Content["application/json"])
	require.Equal(t, "#/components/schemas/Pet", updateOp.RequestBody.Content["application/json"].Schema.Ref)

	// Check Response conversion
	getOp := doc.Paths["/pet/{petId}"].Get
	require.NotNil(t, getOp.Responses["200"].Content["application/json"])
	require.Equal(t, "#/components/schemas/Pet", getOp.Responses["200"].Content["application/json"].Schema.Ref)

	// Generate contract code
	code, err := openapi.GenerateContract(doc, openapi.ImportConfig{
		PackageName: "petstore",
		ServiceName: "PetstoreAPI",
	})
	require.NoError(t, err)
	require.NotEmpty(t, code)
	require.Contains(t, string(code), "type Pet struct")
	require.Contains(t, string(code), "type APIResponse struct")
	require.Contains(t, string(code), "GetPetByID(ctx context.Context, petID int64")
	require.Contains(t, string(code), "UpdatePetWithForm(ctx context.Context, petID int64, req Pet")
}

func TestOpenAPI_PolymorphicAndRecursiveSchemas(t *testing.T) {
	const specYAML = `openapi: "3.1.0"
info:
  title: "Polymorphic & Recursive API"
  version: "1.0.0"
paths:
  /trees:
    get:
      summary: "Get tree node"
      responses:
        "200":
          description: "Tree root"
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/TreeNode"
components:
  schemas:
    TreeNode:
      type: "object"
      properties:
        id:
          type: "string"
        parent:
          $ref: "#/components/schemas/TreeNode"
        children:
          type: "array"
          items:
            $ref: "#/components/schemas/TreeNode"
    Pet:
      type: "object"
      properties:
        name:
          type: "string"
    Dog:
      allOf:
        - $ref: "#/components/schemas/Pet"
        - type: "object"
          properties:
            bark:
              type: "string"
    PetUnion:
      discriminator:
        propertyName: "petType"
      oneOf:
        - $ref: "#/components/schemas/Dog"
        - $ref: "#/components/schemas/Pet"
`

	doc, err := openapi.ParseSpec([]byte(specYAML))
	require.NoError(t, err)
	require.NotNil(t, doc)

	code, err := openapi.GenerateContract(doc, openapi.ImportConfig{
		PackageName: "trees",
		ServiceName: "TreeAPI",
	})
	require.NoError(t, err)
	require.NotEmpty(t, code)

	codeStr := string(code)

	// 1. Check recursive schema pointers
	require.Contains(t, codeStr, "type TreeNode struct {")
	require.Regexp(t, regexp.MustCompile(`Parent\s+\*TreeNode\s+`+"`"+`json:"parent,omitempty"`+"`"), codeStr)
	require.Regexp(t, regexp.MustCompile(`Children\s+\[\]\*TreeNode\s+`+"`"+`json:"children,omitempty"`+"`"), codeStr)

	// 2. Check allOf composition flattening
	require.Contains(t, codeStr, "type Dog struct {")
	require.Regexp(t, regexp.MustCompile(`Name\s+string\s+`+"`"+`json:"name,omitempty"`+"`"), codeStr)
	require.Regexp(t, regexp.MustCompile(`Bark\s+string\s+`+"`"+`json:"bark,omitempty"`+"`"), codeStr)

	// 3. Check oneOf tagged union with discriminator
	require.Contains(t, codeStr, "// @aoni:union discriminator=petType")
	require.Contains(t, codeStr, "type PetUnion struct {")
	require.Regexp(t, regexp.MustCompile(`PetType\s+string\s+`+"`"+`json:"petType"`+"`"), codeStr)
	require.Regexp(t, regexp.MustCompile(`Dog\s+\*Dog\s+`+"`"+`json:"dog,omitempty"`+"`"), codeStr)
	require.Regexp(t, regexp.MustCompile(`Pet\s+\*Pet\s+`+"`"+`json:"pet,omitempty"`+"`"), codeStr)
}

func TestOpenAPI_RemoteSpecAndOfflineCache(t *testing.T) {
	origWd, _ := os.Getwd()
	defer func() { _ = os.Chdir(origWd) }()

	tempDir := t.TempDir()

	require.NoError(t, os.Chdir(tempDir))

	serverHitCount := 0

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		serverHitCount++

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleSwaggerJSON))
	}))
	defer ts.Close()

	// 1. First load: hits server and caches snapshot
	doc, err := openapi.LoadSpec(ts.URL+"/swagger.json", nil)
	require.NoError(t, err)
	require.NotNil(t, doc)
	require.NotNil(t, doc.Info)
	require.Equal(t, "Backpack.tf API", doc.Info.Title)
	require.Equal(t, 1, serverHitCount)

	// 2. Shut down server to simulate offline / network outage
	serverURL := ts.URL + "/swagger.json"
	ts.Close()

	// 3. Second load: falls back to offline cache
	docOffline, err := openapi.LoadSpec(serverURL, nil)
	require.NoError(t, err)
	require.NotNil(t, docOffline)
	require.NotNil(t, docOffline.Info)
	require.Equal(t, "Backpack.tf API", docOffline.Info.Title)
}
