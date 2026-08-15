// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package parser_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/codegen/ir"
	"github.com/lemon4ksan/aoni/internal/codegen/parser"
)

func TestLexerDirectives(t *testing.T) {
	t.Run("basic directive", func(t *testing.T) {
		d := parser.ParseDirective("// @aoni:service")
		require.NotNil(t, d)
		require.Equal(t, "aoni:service", d.Name)
	})

	t.Run("directive with value and args", func(t *testing.T) {
		d := parser.ParseDirective(`// @retry attempts=3 backoff="200ms" jitter=full on="500,502"`)
		require.NotNil(t, d)
		require.Equal(t, "retry", d.Name)
		require.Equal(t, "3", d.Args["attempts"])
		require.Equal(t, "200ms", d.Args["backoff"])
		require.Equal(t, "full", d.Args["jitter"])
		require.Equal(t, "500,502", d.Args["on"])
	})

	t.Run("check directive", func(t *testing.T) {
		chk := parser.ParseCheckDirective("success == true")
		require.NotNil(t, chk)
		require.Equal(t, "success", chk.Field)
		require.Equal(t, ir.OpEqual, chk.Operator)
		require.Equal(t, "true", chk.ExpectedVal)
	})

	t.Run("path template decomposition", func(t *testing.T) {
		tmpl := parser.ParsePathTemplate("market/listings/{app_id}/{market_hash_name:path_escape}")
		require.NotNil(t, tmpl)
		require.Len(t, tmpl.Segments, 4)

		require.False(t, tmpl.Segments[0].IsVariable)
		require.Equal(t, "market/listings/", tmpl.Segments[0].Literal)

		require.True(t, tmpl.Segments[1].IsVariable)
		require.Equal(t, "app_id", tmpl.Segments[1].VarName)
		require.Equal(t, ir.TransformNone, tmpl.Segments[1].Transform)

		require.False(t, tmpl.Segments[2].IsVariable)
		require.Equal(t, "/", tmpl.Segments[2].Literal)

		require.True(t, tmpl.Segments[3].IsVariable)
		require.Equal(t, "market_hash_name", tmpl.Segments[3].VarName)
		require.Equal(t, ir.TransformPathEscape, tmpl.Segments[3].Transform)
	})
}

func TestParserFullSource(t *testing.T) {
	src := `
package testapi

import (
	"context"
	"github.com/lemon4ksan/aoni"
)

// @aoni:service
// @base_url "https://api.github.com"
// @engine fast
// @header "User-Agent: Aoni-Bot"
type GitHubAPI interface {
	// @get "users/{username}"
	// @header "Accept: application/vnd.github.v3+json"
	// @unwrap data
	GetUser(ctx context.Context, username string, mods ...aoni.RequestModifier) (*User, error)

	// @post "repos/{owner}/{repo}/issues"
	// @expect_status 201
	CreateIssue(ctx context.Context, owner, repo string, req *CreateIssueRequest) (*Issue, error)
}

// @aoni:dto casing=snake_case omitempty=true
type User struct {
	ID       int64
	Login    string
	// @field "avatar_url_custom"
	AvatarURL string
}

// @aoni:tuple
type GraphPoint struct {
	Price       float64
	Volume      int64
	Description string
}
`

	p := parser.NewParser()
	root, err := p.ParseSource("testapi.go", []byte(src))
	require.NoError(t, err)
	require.NotNil(t, root)

	// Verify Services
	require.Len(t, root.Services, 1)
	svc := root.Services[0]
	require.Equal(t, "GitHubAPI", svc.Name)
	require.Equal(t, "https://api.github.com", svc.BaseURL)
	require.Equal(t, ir.EngineFast, svc.Engine)
	require.Len(t, svc.Headers, 1)
	require.Equal(t, "User-Agent", svc.Headers[0].Key)
	require.Equal(t, "Aoni-Bot", svc.Headers[0].StaticValue)

	// Verify Methods
	require.Len(t, svc.Methods, 2)

	// Method 1: GetUser
	m1 := svc.Methods[0]
	require.Equal(t, "GetUser", m1.Name)
	require.Equal(t, "GET", m1.HTTPMethod)
	require.Equal(t, "data", m1.UnwrapField)
	require.Len(t, m1.Params, 3)
	require.Equal(t, ir.LocContext, m1.Params[0].Location)
	require.Equal(t, ir.LocPath, m1.Params[1].Location)
	require.Equal(t, "username", m1.Params[1].WireKey)
	require.Equal(t, ir.LocModifiers, m1.Params[2].Location)
	require.Equal(t, "*User", m1.Return.SuccessType.Name)

	// Method 2: CreateIssue
	m2 := svc.Methods[1]
	require.Equal(t, "CreateIssue", m2.Name)
	require.Equal(t, "POST", m2.HTTPMethod)
	require.Equal(t, []int{201}, m2.ExpectStatus)
	require.Equal(t, ir.LocBody, m2.Params[3].Location)

	// Verify DTOs
	require.Len(t, root.Structs, 1)
	strct := root.Structs[0]
	require.Equal(t, "User", strct.Name)
	require.Equal(t, ir.CasingSnakeCase, strct.Casing)
	require.Len(t, strct.Fields, 3)
	require.Equal(t, "id", strct.Fields[0].WireName)
	require.Equal(t, "login", strct.Fields[1].WireName)
	require.Equal(t, "avatar_url_custom", strct.Fields[2].WireName)

	// Verify Tuples
	require.Len(t, root.Tuples, 1)
	tuple := root.Tuples[0]
	require.Equal(t, "GraphPoint", tuple.Name)
	require.Len(t, tuple.Fields, 3)
	require.Equal(t, 0, tuple.Fields[0].Index)
	require.Equal(t, "Price", tuple.Fields[0].GoName)
	require.Equal(t, 1, tuple.Fields[1].Index)
	require.Equal(t, "Volume", tuple.Fields[1].GoName)
	require.Equal(t, 2, tuple.Fields[2].Index)
	require.Equal(t, "Description", tuple.Fields[2].GoName)
}

func TestParser_FormCasingAndImplicitBinding(t *testing.T) {
	src := `package testapi

import "context"

// @aoni:service casing=snake_case
type TradeAPI interface {
	// @post "/tradeoffer/{offer_id}/accept"
	// @form
	Accept(ctx context.Context, offerID uint64, serverID int, sessionID string) (*AcceptResponse, error)

	// @post "/tradeoffer/{offer_id}/cancel"
	// @form casing=flatcase
	Cancel(ctx context.Context, offerID uint64, serverID int, sessionID string) error
}

type AcceptResponse struct {
	TradeError string ` + "`json:\"trade_error\"`" + `
}
`

	p := parser.NewParser()
	root, err := p.ParseSource("tradeapi.go", []byte(src))
	require.NoError(t, err)
	require.NotNil(t, root)

	require.Len(t, root.Services, 1)
	svc := root.Services[0]
	require.Equal(t, ir.CasingSnakeCase, svc.DefaultCasing)

	// Method 1: Accept (snake_case)
	m1 := svc.Methods[0]
	require.Equal(t, "Accept", m1.Name)
	require.Equal(t, ir.PayloadForm, m1.PayloadKind)
	// offerID -> binds to {offer_id} path var
	require.Equal(t, ir.LocPath, m1.Params[1].Location)
	require.Equal(t, "offer_id", m1.Params[1].WireKey)
	// serverID -> binds to server_id form field
	require.Equal(t, ir.LocFormFields, m1.Params[2].Location)
	require.Equal(t, "server_id", m1.Params[2].WireKey)
	// sessionID -> binds to session_id form field
	require.Equal(t, ir.LocFormFields, m1.Params[3].Location)
	require.Equal(t, "session_id", m1.Params[3].WireKey)

	// Method 2: Cancel (flatcase)
	m2 := svc.Methods[1]
	require.Equal(t, "Cancel", m2.Name)
	require.Equal(t, ir.CasingFlatCase, m2.FormCasing)
	// offerID -> binds to {offer_id} path var
	require.Equal(t, ir.LocPath, m2.Params[1].Location)
	require.Equal(t, "offer_id", m2.Params[1].WireKey)
	// serverID -> binds to serverid
	require.Equal(t, ir.LocFormFields, m2.Params[2].Location)
	require.Equal(t, "serverid", m2.Params[2].WireKey)
	// sessionID -> binds to sessionid
	require.Equal(t, ir.LocFormFields, m2.Params[3].Location)
	require.Equal(t, "sessionid", m2.Params[3].WireKey)
}

func TestParser_ServiceLevelUnwrapInheritance(t *testing.T) {
	src := `
package test

import (
	"context"
	"github.com/lemon4ksan/aoni"
)

// @aoni:service
// @unwrap "content"
type StoreAPI interface {
	// @get "items"
	GetItems(ctx context.Context) ([]string, error)

	// @get "raw"
	// @unwrap none
	GetRaw(ctx context.Context) (string, error)

	// @get "custom"
	// @unwrap "result"
	GetCustom(ctx context.Context) (int, error)
}
`

	p := parser.NewParser()
	root, err := p.ParseSource("storeapi.go", []byte(src))
	require.NoError(t, err)
	require.NotNil(t, root)

	require.Len(t, root.Services, 1)
	svc := root.Services[0]
	require.Equal(t, "content", svc.DefaultUnwrapField)
	require.Len(t, svc.Methods, 3)

	// Inherited from service
	require.Equal(t, "content", svc.Methods[0].UnwrapField)
	// Explicitly disabled with 'none'
	require.Equal(t, "", svc.Methods[1].UnwrapField)
	// Overridden with 'result'
	require.Equal(t, "result", svc.Methods[2].UnwrapField)
}
