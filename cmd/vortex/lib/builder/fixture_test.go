// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package builder_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/builder"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/cache"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
)

func TestPopulateMockFixtures_FromCache(t *testing.T) {
	tempDir := t.TempDir()

	harJSON := `{
		"log": {
			"entries": [
				{
					"request": {
						"method": "GET",
						"url": "https://api.example.com/v1/users/123",
						"headers": []
					},
					"response": {
						"status": 200,
						"headers": [
							{"name": "Content-Type", "value": "application/json"},
							{"name": "x-server-node", "value": "node-42"}
						],
						"content": {
							"mimeType": "application/json",
							"text": "{\"id\":\"123\",\"name\":\"Alice\",\"role\":\"admin\"}"
						}
					}
				},
				{
					"request": {
						"method": "POST",
						"url": "https://api.example.com/v1/rpc/CountTokens",
						"headers": []
					},
					"response": {
						"status": 200,
						"headers": [
							{"name": "Content-Type", "value": "application/json"}
						],
						"content": {
							"mimeType": "application/json",
							"text": "[142, 512]"
						}
					}
				}
			]
		}
	}`

	_, _, err := cache.StoreTraffic(tempDir, "test_session.har", []byte(harJSON), false, false)
	require.NoError(t, err)

	svc := &ir.ServiceIR{
		Name:   "UserAPI",
		Source: "cache:test_session",
		Methods: []*ir.MethodIR{
			{
				Name:       "GetUser",
				HTTPMethod: "GET",
				Path:       &ir.PathIR{RawTemplate: "/v1/users/{id}"},
			},
			{
				Name:       "CountTokens",
				HTTPMethod: "POST",
				Path:       &ir.PathIR{RawTemplate: "/v1/rpc/CountTokens"},
			},
			{
				Name:       "UnknownMethod",
				HTTPMethod: "GET",
				Path:       &ir.PathIR{RawTemplate: "/v1/unknown"},
			},
		},
	}

	err = builder.PopulateMockFixtures(tempDir, svc)
	require.NoError(t, err)

	// Verify GetUser fixture
	require.NotNil(t, svc.Methods[0].MockFixture)
	require.Equal(t, 200, svc.Methods[0].MockFixture.StatusCode)
	require.Equal(t, "application/json", svc.Methods[0].MockFixture.ContentType)
	require.Equal(t, "node-42", svc.Methods[0].MockFixture.Headers["x-server-node"])
	require.Contains(t, svc.Methods[0].MockFixture.Body, `"name":"Alice"`)

	// Verify CountTokens fixture
	require.NotNil(t, svc.Methods[1].MockFixture)
	require.Equal(t, 200, svc.Methods[1].MockFixture.StatusCode)
	require.Equal(t, "[142, 512]", svc.Methods[1].MockFixture.Body)

	// Verify UnknownMethod has no fixture
	require.Nil(t, svc.Methods[2].MockFixture)
}

func TestPopulateMockFixtures_FromHARFile(t *testing.T) {
	tempDir := t.TempDir()
	harPath := filepath.Join(tempDir, "sample.har")

	harJSON := `{
		"log": {
			"entries": [
				{
					"request": {
						"method": "GET",
						"url": "https://api.example.com/api/features",
						"headers": []
					},
					"response": {
						"status": 200,
						"headers": [],
						"content": {
							"mimeType": "application/json",
							"text": "{\"features\":[\"alpha\",\"beta\"]}"
						}
					}
				}
			]
		}
	}`

	err := os.WriteFile(harPath, []byte(harJSON), 0o600)
	require.NoError(t, err)

	svc := &ir.ServiceIR{
		Name:   "FeatureAPI",
		Source: harPath,
		Methods: []*ir.MethodIR{
			{
				Name:       "GetFeatures",
				HTTPMethod: "GET",
				Path:       &ir.PathIR{RawTemplate: "/api/features"},
			},
		},
	}

	err = builder.PopulateMockFixtures(tempDir, svc)
	require.NoError(t, err)

	require.NotNil(t, svc.Methods[0].MockFixture)
	require.Contains(t, svc.Methods[0].MockFixture.Body, `"alpha"`)
}
