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

func TestSteamProfileScrapingAndMultipartGeneration(t *testing.T) {
	src := `
package profile

import (
	"context"

	"github.com/lemon4ksan/aoni"
)

// @aoni:service
// @base_url "https://steamcommunity.com/"
// @engine fast
// @header "X-Requested-With: XMLHttpRequest"
type SteamProfile interface {
	// 1. Extract JSON from HTML attribute (Pipeline)
	// @get "profiles/{steam_id}/edit/info"
	// @return body | attr(css="#profile_edit_config", name="data-profile-edit") | html_unescape | json
	GetEditConfig(ctx context.Context, steam_id uint64, mods ...aoni.RequestModifier) (*RawProfileEditConfig, error)

	// 2. Extract JSON from Script tag via Regex (Pipeline)
	// @get "tradingcards/boostercreator"
	// @return body | regex("CBoosterCreatorPage\\.Init\\(\\s*([\\{\\[].*?[\\}\\]])") | json
	GetBoosterCatalog(ctx context.Context, mods ...aoni.RequestModifier) (*BoosterCatalog, error)

	// 3. Extract JSON between prefix and suffix (Pipeline)
	// @get "profiles/{steam_id}/edit/settings"
	// @return body | between(prefix="data-profile-edit=\"", suffix="\"") | json
	GetPrivacyConfig(ctx context.Context, steam_id uint64, mods ...aoni.RequestModifier) (*RawPrivacyConfig, error)

	// 4. Form POST with JSON-in-Form pipeline
	// @post "profiles/{steam_id}/ajaxsetprivacy"
	// @form
	// @check "success == 1"
	SavePrivacy(
		ctx context.Context,
		steam_id uint64,
		// @field "Privacy" = json | url_escape
		privacy RawPrivacySettings,
		// @field "eCommentPermission"
		commentPermission int,
		mods ...aoni.RequestModifier,
	) error

	// 5. Multipart File Upload (@multipart, @part, @file)
	// @post "actions/FileUploader"
	// @multipart
	// @header "Accept: application/json, text/javascript; q=0.01"
	UploadAvatarFile(
		ctx context.Context,
		// @part "type"
		uploadType string,
		// @part "sId"
		sId uint64,
		// @part "sessionid"
		sessionID string,
		// @part "doSub"
		doSub string,
		// @part "json"
		jsonFlag string,
		// @file name="avatar" filename="{filename}" content_type="{content_type}"
		image []byte,
		filename string,
		contentType string,
		mods ...aoni.RequestModifier,
	) (*UploadAvatarResponse, error)
}

// @aoni:dto casing=snake_case
type RawProfileEditConfig struct {
	PersonaName string
	RealName    string
	CustomURL   string
}

// @aoni:dto casing=snake_case
type BoosterCatalog struct {
	AppID uint32
}

// @aoni:dto casing=snake_case
type RawPrivacyConfig struct {
	PrivacyState string
}

// @aoni:dto casing=snake_case
type RawPrivacySettings struct {
	ProfileState int
	Inventory    int
}

// @aoni:dto casing=snake_case
type UploadAvatarResponse struct {
	Success bool
	Message string
}
`

	p := parserpkg.NewParser()
	root, err := p.ParseSource("profile.go", []byte(src))
	require.NoError(t, err)

	an := analysis.NewAnalyzer()
	diags := an.Analyze(root)
	for _, d := range diags {
		println("DIAG: " + d.Target + ": " + d.Message)
	}
	require.False(t, analysis.HasErrors(diags), "Diagnostics: %v", diags)

	opt := optimizer.NewOptimizer()
	opt.Optimize(root)

	code, err := emitter.Emit(root)
	require.NoError(t, err)
	require.NotEmpty(t, code)

	// Verify that generated code parses without syntax errors
	fset := token.NewFileSet()
	_, parseErr := parser.ParseFile(fset, "profile.gen.go", code, parser.AllErrors)
	if parseErr != nil {
		println("PARSE ERR: " + parseErr.Error())
		println("CODE:\n" + string(code))
	}
	require.NoError(t, parseErr, "PARSE ERR: %v", parseErr)

	codeStr := string(code)

	// 1. Verify HTML Token/CSS extraction
	require.Contains(t, codeStr, `extract.Attr(stageIn, "#profile_edit_config", "data-profile-edit")`)
	require.Contains(t, codeStr, `extract.HTMLUnescape(stageIn)`)

	// 2. Verify Regex extraction
	require.Contains(t, codeStr, `extract.Regex(stageIn, "CBoosterCreatorPage\\.Init\\(\\s*([\\{\\[].*?[\\}\\]])")`)

	// 3. Verify Between extraction (0 alloc bytes.Index)
	require.Contains(t, codeStr, `extract.Between(stageIn, "data-profile-edit=\"", "\"")`)

	// 4. Verify JSON-in-Form serialization
	require.Contains(t, codeStr, "privacyBytes, err := json.Marshal(privacy)")
	require.Contains(t, codeStr, "formBytes = append(formBytes, url.QueryEscape(string(privacyBytes))...)")

	// 5. Verify Multipart writer code
	require.Contains(t, codeStr, "mw := multipart.NewWriter(&bodyBuf)")
	require.Contains(t, codeStr, `_ = mw.WriteField("sessionid", fmt.Sprint(sessionID))`)
	require.Contains(t, codeStr, `hdr.Set("Content-Disposition", fmt.Sprintf(`+"`"+`form-data; name=%q; filename=%q`+"`"+`, "avatar", filename))`)
	require.Contains(t, codeStr, `hdr.Set("Content-Type", contentType)`)
}
