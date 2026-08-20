// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package emitter_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/codegen/emitter"
	"github.com/lemon4ksan/aoni/internal/codegen/parser"
)

func TestEmitter_PipelineReturn(t *testing.T) {
	src := `
package profile

// @aoni:service
// @base_url "https://steamcommunity.com"
type ProfileService interface {
	// @get "/profiles/{steamID}/edit/info"
	// @return body | attr(css="#profile_edit_config", name="data-profile-edit") | html_unescape | json
	GetEditConfig(ctx context.Context, steamID string) (*ProfileEditConfig, error)

	// @post "/profiles/{steamID}/edit/settings"
	// @form
	// @field "sessionid" = session_id
	// @field "Privacy" = json | url_escape
	UpdatePrivacy(ctx context.Context, steamID string, privacy *ProfilePrivacy) (*UpdateResponse, error)
}

type ProfileEditConfig struct {
	PersonaName string ` + "`json:\"persona_name\"`" + `
}

type ProfilePrivacy struct {
	ProfileState int ` + "`json:\"PrivacyProfile\"`" + `
}

type UpdateResponse struct {
	Success int ` + "`json:\"success\"`" + `
}
`

	p := parser.NewParser()
	root, err := p.ParseSource("api.go", []byte(src))
	require.NoError(t, err)

	em := emitter.NewEmitter()
	codeBytes, err := em.Emit(root)
	require.NoError(t, err)

	code := string(codeBytes)
	require.Contains(t, code, `extract.Attr(stageIn, "#profile_edit_config", "data-profile-edit")`)
	require.Contains(t, code, `stageIn = extract.HTMLUnescape(stageIn)`)
	require.Contains(t, code, `decode.UnmarshalJSON(stageIn, &result)`)
	require.Contains(t, code, `privacyBytes, err := json.Marshal(privacy)`)
	require.Contains(t, code, `url.QueryEscape(string(privacyBytes))`)
}

func TestEmitter_PipelineBetweenAndRegex(t *testing.T) {
	src := `
package booster

// @aoni:service
type BoosterService interface {
	// @get "/tradingcards/boostercreator"
	// @return body | between(prefix="CBoosterCreatorPage.Init( ", suffix=", 100,") | json
	GetBoosterData(ctx context.Context) ([]*BoosterData, error)

	// @get "/market"
	// @return body | regex("var g_rgAppContextData = (.*?);") | json
	GetAppContext(ctx context.Context) (map[string]any, error)
}

type BoosterData struct {
	AppID int ` + "`json:\"appid\"`" + `
}
`

	p := parser.NewParser()
	root, err := p.ParseSource("api.go", []byte(src))
	require.NoError(t, err)

	em := emitter.NewEmitter()
	codeBytes, err := em.Emit(root)
	require.NoError(t, err)

	code := string(codeBytes)
	require.Contains(t, code, `extract.Between(stageIn, "CBoosterCreatorPage.Init( ", ", 100,")`)
	require.Contains(t, code, `extract.Regex(stageIn, "var g_rgAppContextData = (.*?);")`)
	require.Contains(t, code, `decode.UnmarshalJSON(stageIn, &result)`)
}
