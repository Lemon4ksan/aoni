// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package codegen_test

import (
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/analysis"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/emitter"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/optimizer"
	parserpkg "github.com/lemon4ksan/aoni/cmd/vortex/lib/parser"
)

func TestRPCPrimitives_RPC_Notify_Event(t *testing.T) {
	src := `
package account

import (
	"context"
)

type AccountInfoMsg struct {
	PersonaName string ` + "`json:\"persona_name\"`" + `
	Flags       uint32 ` + "`json:\"flags\"`" + `
}

type WalletInfoMsg struct {
	Balance int64 ` + "`json:\"balance\"`" + `
}

type CustomBans struct {
	NumBans uint32
}

func parseBans(raw []byte) (*CustomBans, error) {
	return &CustomBans{}, nil
}

// SteamAccountAPI describes inbound push events and account operations.
//
// @aoni:service
// @protocol rpc
type SteamAccountAPI interface {
	// @event 5501
	// @return json
	OnAccountInfo(handler func(msg *AccountInfoMsg)) (unsubscribe func())

	// @event "wallet.update"
	// @return json
	OnWalletUpdate(handler func(msg *WalletInfoMsg)) func()

	// @event 5503
	// @return custom=parseBans
	OnVACBans(handler func(bans *CustomBans)) (unsubscribe func())

	// @op 5504
	// @body json
	// @return json
	GetStatus(ctx context.Context, req *AccountInfoMsg) (*AccountInfoMsg, error)

	// @op 5505
	// @notify
	// @body json
	SendHeartbeat(ctx context.Context, req *AccountInfoMsg) error
}
`

	p := parserpkg.NewParser()
	root, err := p.ParseSource("account.go", []byte(src))
	require.NoError(t, err)

	an := analysis.NewAnalyzer()
	diags := an.Analyze(root)
	require.False(t, analysis.HasErrors(diags), "Diagnostics: %v", diags)

	opt := optimizer.NewOptimizer()
	opt.Optimize(root)

	em := emitter.NewEmitter()
	codeBytes, err := em.Emit(root)
	require.NoError(t, err)
	require.NotEmpty(t, codeBytes)

	codeStr := string(codeBytes)

	// Verify that generated code parses without syntax errors
	fset := token.NewFileSet()
	_, parseErr := parser.ParseFile(fset, "account.gen.go", codeBytes, parser.AllErrors)
	if parseErr != nil {
		t.Fatalf("Generated code syntax error: %v\nCode:\n%s", parseErr, codeStr)
	}
	require.NoError(t, parseErr)

	// 1. Verify Service struct and Constructor
	require.Contains(t, codeStr, "type steamAccountAPIClient struct {")
	require.Contains(t, codeStr, "transport request.Transport")
	require.Contains(t, codeStr, "func NewSteamAccountAPI(transport request.Transport) SteamAccountAPI {")

	// 2. Verify Event 5501 with JSON decode
	require.Contains(t, codeStr, `unsub := c.transport.Subscribe(5501, func(raw []byte) {`)
	require.Contains(t, codeStr, "decode.UnmarshalJSON(stageIn, &msg)")
	require.Contains(t, codeStr, "handler(&msg)")
	require.Contains(t, codeStr, "c.unregs = append(c.unregs, unsub)")

	// 3. Verify Event string ID with JSON decode
	require.Contains(t, codeStr, `unsub := c.transport.Subscribe("wallet.update", func(raw []byte) {`)

	// 4. Verify Event with custom decoder pipeline
	require.Contains(t, codeStr, `unsub := c.transport.Subscribe(5503, func(raw []byte) {`)
	require.Contains(t, codeStr, "res, err := parseBans(stageIn)")
	require.Contains(t, codeStr, "handler(res)")

	// 5. Verify RPC Invoke
	require.Contains(t, codeStr, "payloadBytes, err := json.Marshal(req)")
	require.Contains(t, codeStr, "rawResp, err := c.transport.Invoke(ctx, 5504, payloadBytes)")
	require.Contains(t, codeStr, "decode.UnmarshalJSON(rawResp, &result)")

	// 6. Verify One-Way Notify
	require.Contains(t, codeStr, "return c.transport.Notify(ctx, 5505, payloadBytes)")

	// 7. Verify Close
	require.Contains(t, codeStr, "func (c *steamAccountAPIClient) Close() error {")
}

func TestRPCPrimitives_ProtobufAndEnums(t *testing.T) {
	src := `
package account

import (
	"context"
	"github.com/lemon4ksan/g-man/pkg/steam/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/protobuf/steam/steammessages_clientserver_login"
)

// SocketAccountAPI handles account management over binary sockets.
//
// @aoni:service
// @protocol rpc
type SocketAccountAPI interface {
	// @event enums.EMsg_ClientAccountInfo
	// @return proto
	OnAccountInfo(handler func(msg *steammessages_clientserver_login.CMsgClientAccountInfo)) (unsubscribe func())

	// @op enums.EMsg_ClientGetStatus
	// @body proto
	// @return proto
	GetStatus(ctx context.Context, req *steammessages_clientserver_login.CMsgClientAccountInfo) (*steammessages_clientserver_login.CMsgClientAccountInfo, error)

	// @op enums.EMsg_ClientHeartbeat
	// @notify
	// @body proto
	Heartbeat(ctx context.Context, req *steammessages_clientserver_login.CMsgClientAccountInfo) error
}
`

	p := parserpkg.NewParser()
	root, err := p.ParseSource("socket_account.go", []byte(src))
	require.NoError(t, err)

	an := analysis.NewAnalyzer()
	diags := an.Analyze(root)
	require.False(t, analysis.HasErrors(diags), "Diagnostics: %v", diags)

	opt := optimizer.NewOptimizer()
	opt.Optimize(root)

	em := emitter.NewEmitter()
	codeBytes, err := em.Emit(root)
	require.NoError(t, err)
	require.NotEmpty(t, codeBytes)

	codeStr := string(codeBytes)

	// 1. Verify Enum identifier is emitted unquoted
	require.Contains(t, codeStr, "unsub := c.transport.Subscribe(enums.EMsg_ClientAccountInfo, func(raw []byte) {")
	require.Contains(t, codeStr, "proto.Unmarshal(stageIn, &msg)")
	require.Contains(t, codeStr, "c.unregs = append(c.unregs, unsub)")

	// 2. Verify Protobuf RPC Invoke with unquoted Enum op ID
	require.Contains(t, codeStr, "payloadBytes, err := proto.Marshal(req)")
	require.Contains(t, codeStr, "rawResp, err := c.transport.Invoke(ctx, enums.EMsg_ClientGetStatus, payloadBytes)")
	require.Contains(t, codeStr, "proto.Unmarshal(rawResp, &result)")

	// 3. Verify Protobuf Notify with unquoted Enum op ID
	require.Contains(t, codeStr, "return c.transport.Notify(ctx, enums.EMsg_ClientHeartbeat, payloadBytes)")
}
