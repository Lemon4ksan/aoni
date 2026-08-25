// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package codegen_test

import (
	"context"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/emitter"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/parser"
)

// @aoni:service
// @engine custom type="community.Requester" required
// @base_url "https://api.secure.internal"
type SecureAdminService interface {
	// @get "admin/status"
	GetStatus(ctx context.Context) (*AdminStatus, error)
}

type AdminStatus struct {
	Active bool `json:"active"`
}

func TestRequiredEngineAndRequester(t *testing.T) {
	p := parser.NewParser()
	root, err := p.ParseFile("required_engine_test.go")
	require.NoError(t, err)

	code, err := emitter.Emit(root)
	require.NoError(t, err)

	codeStr := string(code)

	// 1. Verify New constructor returns (Service, error)
	require.Contains(
		t,
		codeStr,
		"func NewSecureAdminService(client community.Requester, opts ...aoni.ClientOption) (SecureAdminService, error)",
	)

	// 2. Verify nil validation
	require.Contains(
		t,
		codeStr,
		`if client == nil {`,
	)
	require.Contains(
		t,
		codeStr,
		`return nil, errors.New("aoni: client (community.Requester) is required to initialize SecureAdminService")`,
	)

	// 3. Verify MustNew constructor helper
	require.Contains(
		t,
		codeStr,
		"func MustNewSecureAdminService(client community.Requester, opts ...aoni.ClientOption) SecureAdminService",
	)
	require.Contains(
		t,
		codeStr,
		"api, err := NewSecureAdminService(client, opts...)",
	)
	require.Contains(
		t,
		codeStr,
		"if err != nil {\n\t\tpanic(err)\n\t}",
	)
}
