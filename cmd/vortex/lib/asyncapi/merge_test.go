// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package asyncapi_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/asyncapi"
)

func TestAsyncAPI_Merge_Union(t *testing.T) {
	spec1Yaml := `
asyncapi: 3.1.0
info:
  title: Chat Service
  version: 1.0.0
servers:
  wsServer:
    host: ws.example.com
    protocol: ws
channels:
  chat/messages:
    address: chat/messages
    messages:
      ChatMessage:
        $ref: '#/components/messages/ChatMessage'
operations:
  onChatMessage:
    action: send
    channel:
      $ref: '#/channels/chat/messages'
components:
  messages:
    ChatMessage:
      payload:
        type: object
        properties:
          text:
            type: string
`

	spec2Yaml := `
asyncapi: 3.1.0
info:
  title: Notification Service
  version: 1.0.0
servers:
  kafkaServer:
    host: kafka.example.com
    protocol: kafka
channels:
  notifications:
    address: notifications
    messages:
      AlertNotification:
        $ref: '#/components/messages/AlertNotification'
operations:
  onAlert:
    action: send
    channel:
      $ref: '#/channels/notifications'
components:
  messages:
    AlertNotification:
      payload:
        type: object
        properties:
          level:
            type: string
`

	doc1, err := asyncapi.ParseSpec([]byte(spec1Yaml))
	require.NoError(t, err)

	doc2, err := asyncapi.ParseSpec([]byte(spec2Yaml))
	require.NoError(t, err)

	merged := asyncapi.MergeSpecs(doc1, doc2)
	require.NotNil(t, merged)

	assert.Len(t, merged.Servers, 2)
	assert.Contains(t, merged.Servers, "wsServer")
	assert.Contains(t, merged.Servers, "kafkaServer")

	assert.Len(t, merged.Channels, 2)
	assert.Contains(t, merged.Channels, "chat/messages")
	assert.Contains(t, merged.Channels, "notifications")

	assert.Len(t, merged.Operations, 2)
	assert.Contains(t, merged.Operations, "onChatMessage")
	assert.Contains(t, merged.Operations, "onAlert")

	assert.Len(t, merged.Components.Messages, 2)
	assert.Contains(t, merged.Components.Messages, "ChatMessage")
	assert.Contains(t, merged.Components.Messages, "AlertNotification")
}

func TestAsyncAPI_Merge_IntersectionAndDiff(t *testing.T) {
	spec1Yaml := `
asyncapi: 3.1.0
channels:
  channelA:
    address: channelA
  channelB:
    address: channelB
operations:
  opA:
    action: send
    channel:
      $ref: '#/channels/channelA'
  opB:
    action: send
    channel:
      $ref: '#/channels/channelB'
`

	spec2Yaml := `
asyncapi: 3.1.0
channels:
  channelB:
    address: channelB
  channelC:
    address: channelC
operations:
  opB:
    action: send
    channel:
      $ref: '#/channels/channelB'
  opC:
    action: send
    channel:
      $ref: '#/channels/channelC'
`

	doc1, err := asyncapi.ParseSpec([]byte(spec1Yaml))
	require.NoError(t, err)

	doc2, err := asyncapi.ParseSpec([]byte(spec2Yaml))
	require.NoError(t, err)

	t.Run("intersection", func(t *testing.T) {
		intersected := asyncapi.MergeSpecsWithMode(asyncapi.MergeModeIntersection, doc1, doc2)
		require.NotNil(t, intersected)

		assert.Len(t, intersected.Channels, 1)
		assert.Contains(t, intersected.Channels, "channelB")

		assert.Len(t, intersected.Operations, 1)
		assert.Contains(t, intersected.Operations, "opB")
	})

	t.Run("difference", func(t *testing.T) {
		diffed := asyncapi.MergeSpecsWithMode(asyncapi.MergeModeDifference, doc1, doc2)
		require.NotNil(t, diffed)

		assert.Len(t, diffed.Channels, 1)
		assert.Contains(t, diffed.Channels, "channelA")

		assert.Len(t, diffed.Operations, 1)
		assert.Contains(t, diffed.Operations, "opA")
	})
}
