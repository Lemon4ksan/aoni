// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package codegen_test

import (
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/emitter"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/parser"
)

const socketTestInterface = `
package test

import (
	"context"
	"time"
)

type CMServer struct {
	Endpoint string
}

type Packet struct {
	Op int
	Payload []byte
}

// @aoni:socket
// @endpoint CMServer
// @packet *Packet
// @opcode int
// @job_id uint64
// @heartbeat interval="10s"
type SteamSocket interface {
	Connect(ctx context.Context, endpoint CMServer) error
	Disconnect() error
	Close() error
	IsConnected() bool

	RegisterMsgHandler(op int, handler func(p *Packet))
	RegisterServiceHandler(method string, handler func(p *Packet))

	Send(ctx context.Context, req []byte) error
}
`

func TestSocketGeneration(t *testing.T) {
	t.Parallel()

	p := parser.NewParser()
	root, err := p.ParseSource("socket_test.go", []byte(socketTestInterface))
	require.NoError(t, err)
	require.NotNil(t, root)

	require.Equal(t, 1, len(root.Services))
	svc := root.Services[0]
	assert.Equal(t, "SteamSocket", svc.Name)
	assert.Equal(t, "socket", string(svc.Protocol))
	require.NotNil(t, svc.SocketConfig)
	assert.Equal(t, "CMServer", svc.SocketConfig.EndpointType)
	assert.Equal(t, "*Packet", svc.SocketConfig.PacketType)
	assert.Equal(t, "int", svc.SocketConfig.OpCodeType)
	assert.Equal(t, "uint64", svc.SocketConfig.JobIDType)

	code, err := emitter.Emit(root)
	require.NoError(t, err)
	require.NotEmpty(t, code)

	codeStr := string(code)
	assert.Contains(t, codeStr, "type SteamSocketConfig struct {")
	assert.Contains(t, codeStr, "connector.Config[CMServer]")
	assert.Contains(t, codeStr, "processor.Config")
	assert.Contains(t, codeStr, "dispatcher.Config")
	assert.Contains(t, codeStr, "func NewSteamSocket(cfg SteamSocketConfig) SteamSocket {")
	assert.Contains(t, codeStr, "func (s *steamSocketImpl) Connect(ctx context.Context, endpoint CMServer) error")
	assert.Contains(t, codeStr, "func (s *steamSocketImpl) Disconnect() error")
	assert.Contains(t, codeStr, "func (s *steamSocketImpl) Close() error")
	assert.Contains(t, codeStr, "func (s *steamSocketImpl) IsConnected() bool")
	assert.Contains(t, codeStr, "func (s *steamSocketImpl) RegisterMsgHandler(op int, handler func(p *Packet))")
	assert.Contains(t, codeStr, "func (s *steamSocketImpl) RegisterServiceHandler(method string, handler func(p *Packet))")
	assert.Contains(t, codeStr, "func (s *steamSocketImpl) StartHeartbeat(interval time.Duration) error")
}
