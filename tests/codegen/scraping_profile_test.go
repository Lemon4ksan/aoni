// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package codegen_test

import (
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/codegen/analysis"
	"github.com/lemon4ksan/aoni/internal/codegen/emitter"
	"github.com/lemon4ksan/aoni/internal/codegen/optimizer"
	parserpkg "github.com/lemon4ksan/aoni/internal/codegen/parser"
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
	// 1. Extract JSON from HTML attribute (Strategy 3: HTML token / CSS)
	// @get "profiles/{steam_id}/edit/info"
	// @extract css="#profile_edit_config" attr="data-profile-edit"
	GetEditConfig(ctx context.Context, steam_id uint64, mods ...aoni.RequestModifier) (*RawProfileEditConfig, error)

	// 2. Extract JSON from Script tag via Regex (Strategy 1: Regex)
	// @get "tradingcards/boostercreator"
	// @extract regex="CBoosterCreatorPage\\.Init\\(\\s*([\\{\\[].*?[\\}\\]])"
	GetBoosterCatalog(ctx context.Context, mods ...aoni.RequestModifier) (*BoosterCatalog, error)

	// 3. Extract JSON between prefix and suffix (Strategy 2: Zero-Alloc Between)
	// @get "profiles/{steam_id}/edit/settings"
	// @extract prefix="data-profile-edit=\"" suffix="\""
	GetPrivacyConfig(ctx context.Context, steam_id uint64, mods ...aoni.RequestModifier) (*RawPrivacyConfig, error)

	// 4. Form POST with JSON-in-Form (@format json_string)
	// @post "profiles/{steam_id}/ajaxsetprivacy"
	// @form
	// @check "success == 1"
	SavePrivacy(
		ctx context.Context,
		steam_id uint64,
		// @field "Privacy"
		// @format json_string
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

	em := emitter.NewEmitter()
	code, err := em.Emit(root)
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
	require.Contains(t, codeStr, `start := bytes.Index(bodyBytes, []byte("profile_edit_config"))`)
	require.Contains(t, codeStr, `attrIdx := bytes.Index(bodyBytes[start:], []byte("data-profile-edit="))`)

	// 2. Verify Regex extraction
	require.Contains(t, codeStr, "rx := regexp.MustCompile(`CBoosterCreatorPage")
	require.Contains(t, codeStr, "matches := rx.FindSubmatch(bodyBytes)")

	// 3. Verify Between extraction (0 alloc bytes.Index)
	require.Contains(t, codeStr, `prefix := []byte("data-profile-edit=\"")`)
	require.Contains(t, codeStr, `suffix := []byte("\"")`)

	// 4. Verify JSON-in-Form serialization
	require.Contains(t, codeStr, "privacyJSON, err := json.Marshal(privacy)")
	require.Contains(t, codeStr, "formBytes = append(formBytes, url.QueryEscape(string(privacyJSON))...)")

	// 5. Verify Multipart writer code
	require.Contains(t, codeStr, "mw := multipart.NewWriter(&bodyBuf)")
	require.Contains(t, codeStr, `_ = mw.WriteField("sessionid", fmt.Sprint(sessionID))`)
	require.Contains(t, codeStr, `hdr.Set("Content-Disposition", fmt.Sprintf(`+"`"+`form-data; name=%q; filename=%q`+"`"+`, "avatar", filename))`)
	require.Contains(t, codeStr, `hdr.Set("Content-Type", contentType)`)
}
