// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package asyncapi_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/asyncapi"
)

func TestAsyncAPI_Import_Simple3(t *testing.T) {
	specYaml := `
asyncapi: 3.1.0
info:
  title: Account Service
  version: 1.0.0
channels:
  userSignedup:
    address: user/signedup
    messages:
      UserSignedUp:
        $ref: '#/components/messages/UserSignedUp'
operations:
  sendUserSignedup:
    action: send
    channel:
      $ref: '#/channels/userSignedup'
    messages:
      - $ref: '#/channels/userSignedup/messages/UserSignedUp'
components:
  messages:
    UserSignedUp:
      payload:
        type: object
        properties:
          displayName:
            type: string
          email:
            type: string
`

	res, err := asyncapi.Import(asyncapi.ImportConfig{
		SpecData:    []byte(specYaml),
		PackageName: "account",
		ServiceName: "AccountAPI",
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, 1, res.ServicesCount)
	assert.Equal(t, 1, res.MethodsCount)

	code := string(res.ContractCode)
	assert.Contains(t, code, "package account")
	assert.Contains(t, code, "type AccountAPI interface")
	assert.Contains(t, code, `// @event "sendUserSignedup"`)
	assert.Contains(
		t,
		code,
		"SendUserSignedup(ctx context.Context, handler func(msg *UserSignedUpDTO)) (Subscription, error)",
	)
	assert.Contains(t, code, "type UserSignedUpDTO struct")
	assert.Contains(t, code, "DisplayName string")
	assert.Contains(t, code, `json:"displayName,omitempty"`)
	assert.Contains(t, code, "Email       string")
	assert.Contains(t, code, `json:"email,omitempty"`)
}

func TestAsyncAPI_Import_RequestReply(t *testing.T) {
	specYaml := `
asyncapi: 3.1.0
info:
  title: Ping Service
  version: 1.0.0
channels:
  pingChannel:
    address: /ping
    messages:
      pingMsg:
        $ref: '#/components/messages/PingMsg'
  pongChannel:
    address: /pong
    messages:
      pongMsg:
        $ref: '#/components/messages/PongMsg'
operations:
  pingRequest:
    action: send
    channel:
      $ref: '#/channels/pingChannel'
    messages:
      - $ref: '#/channels/pingChannel/messages/pingMsg'
    reply:
      channel:
        $ref: '#/channels/pongChannel'
      messages:
        - $ref: '#/channels/pongChannel/messages/pongMsg'
components:
  messages:
    PingMsg:
      payload:
        type: object
        properties:
          timestamp:
            type: integer
            format: int64
    PongMsg:
      payload:
        type: object
        properties:
          ack:
            type: boolean
`

	res, err := asyncapi.Import(asyncapi.ImportConfig{
		SpecData:    []byte(specYaml),
		PackageName: "pingpong",
		ServiceName: "PingPongAPI",
	})
	require.NoError(t, err)
	require.NotNil(t, res)

	code := string(res.ContractCode)
	assert.Contains(t, code, "package pingpong")
	assert.Contains(t, code, "type PingPongAPI interface")
	assert.Contains(t, code, `// @rpc "pingRequest"`)
	assert.Contains(t, code, `PingRequest(ctx context.Context, req *PingMsgDTO, mods ...aoni.RequestModifier) (*PongMsgDTO, error)`)
	assert.Contains(t, code, "type PingMsgDTO struct")
	assert.Contains(t, code, "Timestamp int64")
	assert.Contains(t, code, "type PongMsgDTO struct")
	assert.Contains(t, code, "Ack bool")
}

func TestAsyncAPI_Import_TraitsAndDynamicAddress(t *testing.T) {
	specYaml := `
asyncapi: 3.1.0
info:
  title: Streetlights IoT API
  version: 1.0.0
channels:
  lightingMeasured:
    address: 'smartylighting/streetlights/1/0/event/{streetlightId}/lighting/measured'
    messages:
      lightMeasured:
        $ref: '#/components/messages/lightMeasured'
operations:
  onLightingMeasured:
    action: send
    channel:
      $ref: '#/channels/lightingMeasured'
    messages:
      - $ref: '#/channels/lightingMeasured/messages/lightMeasured'
    traits:
      - $ref: '#/components/operationTraits/iotOpTrait'
components:
  operationTraits:
    iotOpTrait:
      summary: "Inform about environmental lighting conditions"
  messages:
    lightMeasured:
      payload:
        type: object
        properties:
          lumens:
            type: integer
`

	res, err := asyncapi.Import(asyncapi.ImportConfig{
		SpecData:    []byte(specYaml),
		PackageName: "streetlights",
		ServiceName: "StreetlightsAPI",
	})
	require.NoError(t, err)
	require.NotNil(t, res)

	code := string(res.ContractCode)
	assert.Contains(t, code, "package streetlights")
	assert.Contains(t, code, "OnLightingMeasured — Inform about environmental lighting conditions")
	assert.Contains(
		t,
		code,
		"OnLightingMeasured(ctx context.Context, streetlightId string, handler func(msg *LightMeasuredDTO)) (Subscription, error)",
	)
}

func TestAsyncAPI_Import_v2(t *testing.T) {
	specYaml := `
asyncapi: 2.6.0
info:
  title: Streetlights API
  version: 1.0.0
servers:
  production:
    url: api.streetlights.com
    protocol: wss
channels:
  smartylighting/streetlights/1/0/event/{streetlightId}/lighting/measured:
    publish:
      summary: Inform about environmental lighting conditions of a particular streetlight.
      operationId: onLightMeasured
  smartylighting/streetlights/1/0/action/{streetlightId}/turn/on:
    subscribe:
      summary: Command a particular streetlight to turn the lights on.
      operationId: turnOnLight
`

	res, err := asyncapi.Import(asyncapi.ImportConfig{
		SpecData:    []byte(specYaml),
		PackageName: "streetlights",
		ServiceName: "StreetlightsAPI",
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, 1, res.ServicesCount)
	assert.Equal(t, 2, res.MethodsCount)

	code := string(res.ContractCode)
	assert.Contains(t, code, "package streetlights")
	assert.Contains(t, code, "type StreetlightsAPI interface")
	assert.Contains(t, code, `// @event "onLightMeasured"`)
	assert.Contains(t, code, `// @ws:emit "turnOnLight"`)
	assert.Contains(
		t,
		code,
		"OnLightMeasured(ctx context.Context, streetlightId string, handler func(msg *OnLightMeasuredPayloadDTO)) (Subscription, error)",
	)
	assert.Contains(
		t,
		code,
		"TurnOnLight(ctx context.Context, streetlightId string, req *TurnOnLightPayloadDTO, mods ...aoni.RequestModifier) error",
	)
}

func TestAsyncAPI_Import_RealWorldGeminiWS(t *testing.T) {
	geminiPath := filepath.Join("..", "..", "..", "..", "asyncapi-spec", "examples", "websocket-gemini-asyncapi.yml")
	if _, err := os.Stat(geminiPath); os.IsNotExist(err) {
		t.Skip("Gemini asyncapi spec file not found")
	}

	res, err := asyncapi.Import(asyncapi.ImportConfig{
		SpecFile:    geminiPath,
		PackageName: "gemini",
		ServiceName: "GeminiMarketAPI",
	})
	require.NoError(t, err)
	require.NotNil(t, res)

	code := string(res.ContractCode)
	assert.Contains(t, code, "package gemini")
	assert.Contains(t, code, "type GeminiMarketAPI interface")
	assert.Contains(t, code, `// @base_url "wss://api.gemini.com"`)
	assert.Contains(t, code, `// @protocol ws`)
	assert.Contains(t, code, `// @event "sendMarketData"`)
	assert.Contains(
		t,
		code,
		`SendMarketData(ctx context.Context, symbol string, handler func(msg *MarketDataDTO)) (Subscription, error)`,
	)
}
