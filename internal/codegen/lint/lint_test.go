// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lint_test

import (
	"bytes"
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/codegen/ir"
	"github.com/lemon4ksan/aoni/internal/codegen/lint"
)

func TestRegistry_CustomRuleRegistrationAndToggle(t *testing.T) {
	reg := lint.NewRegistry()
	require.Empty(t, reg.ActiveRules())

	reg.Register(&lint.RuleMissingContext{})
	require.Len(t, reg.ActiveRules(), 1)

	reg.Disable("missing-context")
	require.Empty(t, reg.ActiveRules())

	reg.Enable("missing-context")
	require.Len(t, reg.ActiveRules(), 1)
}

func TestIgnoreParser_SuppressesMatchingDiagnostics(t *testing.T) {
	src := `package test
// @aoni:service
type API interface {
	// @post /GetItems
	//vortex:ignore http-verb-mismatch, missing-context -- Allowed by Steam
	GetItems()
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	require.NoError(t, err)

	ignores := lint.ParseIgnores(fset, file)
	require.True(t, ignores.IsIgnored("W003", "http-verb-mismatch", "API.GetItems", 6))
	require.True(t, ignores.IsIgnored("E004", "missing-context", "API.GetItems", 6))
	require.False(t, ignores.IsIgnored("E001", "stale-codegen", "API.GetItems", 6))

	// Test standard //nolint
	srcNoLint := `package test
type API interface {
	// @get /item
	//nolint:sensitive-query-param
	GetItem()
}
`
	file2, err2 := parser.ParseFile(fset, "test2.go", srcNoLint, parser.ParseComments)
	require.NoError(t, err2)

	ignores2 := lint.ParseIgnores(fset, file2)
	require.True(t, ignores2.IsIgnored("S001", "sensitive-query-param", "API.GetItem", 4))
}

func TestRules_UnmatchedPath_DetectsMissingParameter(t *testing.T) {
	rootIR := &ir.RootIR{
		Services: []*ir.ServiceIR{
			{
				Name: "ItemsAPI",
				Methods: []*ir.MethodIR{
					{
						Name:      "GetItem",
						Operation: ir.OpHTTP,
						Path: &ir.PathIR{
							Segments: []ir.PathSegmentIR{
								{IsVariable: false, Literal: "items"},
								{IsVariable: true, VarName: "item_id"},
							},
						},
						Params: []*ir.ParamIR{
							{GoName: "ctx", Location: ir.LocContext},
							{GoName: "other_id", Location: ir.LocQuery},
						},
					},
				},
			},
		},
	}

	pass := &lint.Pass{
		Context:  context.Background(),
		RootIR:   rootIR,
		FilePath: "api.go",
	}

	rule := &lint.RuleUnmatchedPath{}
	diags := rule.Run(pass)

	require.Len(t, diags, 1)
	require.Equal(t, "E002", diags[0].RuleID)
	require.Contains(t, diags[0].Message, "item_id")
}

func TestRules_MissingContext_DetectsAbsence(t *testing.T) {
	rootIR := &ir.RootIR{
		Services: []*ir.ServiceIR{
			{
				Name: "ProfileAPI",
				Methods: []*ir.MethodIR{
					{
						Name:      "GetProfile",
						Operation: ir.OpHTTP,
						Params: []*ir.ParamIR{
							{GoName: "steamID", Location: ir.LocQuery},
						},
					},
				},
			},
		},
	}

	pass := &lint.Pass{
		Context:  context.Background(),
		RootIR:   rootIR,
		FilePath: "api.go",
	}

	rule := &lint.RuleMissingContext{}
	diags := rule.Run(pass)

	require.Len(t, diags, 1)
	require.Equal(t, "E004", diags[0].RuleID)
	require.Contains(t, diags[0].Message, "context.Context")
}

func TestRules_ParamLifting_DetectsRepetition(t *testing.T) {
	methods := make([]*ir.MethodIR, 5)
	for i := range methods {
		methods[i] = &ir.MethodIR{
			Name:      "Method",
			Operation: ir.OpHTTP,
			Params: []*ir.ParamIR{
				{GoName: "ctx", Location: ir.LocContext},
				{GoName: "sessionID", WireKey: "sessionid", Location: ir.LocQuery},
			},
		}
	}

	rootIR := &ir.RootIR{
		Services: []*ir.ServiceIR{
			{
				Name:    "MarketAPI",
				Methods: methods,
			},
		},
	}

	pass := &lint.Pass{
		Context:  context.Background(),
		RootIR:   rootIR,
		FilePath: "market.go",
	}

	rule := &lint.RuleParamLifting{}
	diags := rule.Run(pass)

	require.Len(t, diags, 1)
	require.Equal(t, "W001", diags[0].RuleID)
	require.Contains(t, diags[0].Message, "sessionid")
	require.False(t, diags[0].Fixable(), "Param lifting should be a suggestion only, not auto-fixed")
}

func TestRules_HTTPVerbMismatch_SuggestsVerification(t *testing.T) {
	rootIR := &ir.RootIR{
		Services: []*ir.ServiceIR{
			{
				Name: "MarketAPI",
				Methods: []*ir.MethodIR{
					{
						Name:        "GetMarketHistory",
						Operation:   ir.OpHTTP,
						HTTPMethod:  "POST",
						PayloadKind: ir.PayloadNone,
						Params: []*ir.ParamIR{
							{GoName: "ctx", Location: ir.LocContext},
						},
					},
				},
			},
		},
	}

	pass := &lint.Pass{
		Context:  context.Background(),
		RootIR:   rootIR,
		FilePath: "market.go",
	}

	rule := &lint.RuleHTTPVerbMismatch{}
	diags := rule.Run(pass)

	require.Len(t, diags, 1)
	require.Equal(t, "W003", diags[0].RuleID)
	require.Contains(t, diags[0].Message, "read-only naming prefix")
}

func TestEngine_FormatReport(t *testing.T) {
	report := &lint.Report{
		ServicesChecked: 2,
		MethodsChecked:  10,
		FilesChecked:    1,
		Diagnostics: []lint.Diagnostic{
			{
				RuleID:     "W001",
				RuleName:   "param-lifting",
				Severity:   lint.SeverityWarning,
				FilePath:   "api.go",
				Message:    "Duplicated param",
				Suggestion: "Lift to service",
			},
		},
	}

	var buf bytes.Buffer
	lint.FormatReport(&buf, "api.go", report)
	output := buf.String()

	require.Contains(t, output, "Vortex Contract Inspector")
	require.Contains(t, output, "param-lifting")
	require.Contains(t, output, "* W001 (param-lifting): 1")
}

func TestRules_StaleCodegen_AppliesFix(t *testing.T) {
	tmpDir := t.TempDir()
	apiFile := filepath.Join(tmpDir, "api.go")
	genFile := filepath.Join(tmpDir, "api.gen.go")

	err := os.WriteFile(apiFile, []byte("package api\n"), 0o600)
	require.NoError(t, err)

	rootIR := &ir.RootIR{
		PackageName: "api",
		SourceFile:  apiFile,
		Services: []*ir.ServiceIR{
			{
				Name: "SimpleAPI",
				Methods: []*ir.MethodIR{
					{
						Name:        "Ping",
						Operation:   ir.OpHTTP,
						HTTPMethod:  "GET",
						PayloadKind: ir.PayloadNone,
						Params: []*ir.ParamIR{
							{GoName: "ctx", Location: ir.LocContext},
						},
						Return: &ir.ReturnIR{SuccessType: ir.GoTypeIR{Name: "error"}},
					},
				},
			},
		},
	}

	pass := &lint.Pass{
		Context:  context.Background(),
		RootIR:   rootIR,
		FilePath: apiFile,
	}

	engine := lint.NewEngine(nil)
	report, err := engine.Run(pass)
	require.NoError(t, err)
	require.Equal(t, 1, report.Errors())
	require.Equal(t, 1, report.FixableCount())

	applied, err := report.ApplyFixes()
	require.NoError(t, err)
	require.Equal(t, 1, applied)

	// Now file exists and is in-sync
	require.FileExists(t, genFile)

	report2, err := engine.Run(pass)
	require.NoError(t, err)
	require.Equal(t, 0, report2.Errors())
}

func TestRules_RedundantTag_DetectsAndCleans(t *testing.T) {
	tmpDir := t.TempDir()
	apiFile := filepath.Join(tmpDir, "api.go")

	src := `package api

// @aoni:service
type MarketAPI interface {
	// @get /items
	GetItem(
		ctx context.Context,
		itemID string, // @query "item_id"
	) error
}
`
	err := os.WriteFile(apiFile, []byte(src), 0o600)
	require.NoError(t, err)

	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, apiFile, src, parser.ParseComments)
	require.NoError(t, err)

	rootIR := &ir.RootIR{
		PackageName: "api",
		Services: []*ir.ServiceIR{
			{
				Name:          "MarketAPI",
				DefaultCasing: ir.CasingSnakeCase,
				Methods: []*ir.MethodIR{
					{
						Name:        "GetItem",
						Operation:   ir.OpHTTP,
						HTTPMethod:  "GET",
						PayloadKind: ir.PayloadNone,
						Params: []*ir.ParamIR{
							{GoName: "ctx", Location: ir.LocContext},
							{GoName: "itemID", WireKey: "item_id", Location: ir.LocQuery},
						},
						Return: &ir.ReturnIR{IsVoid: true},
					},
				},
			},
		},
	}

	pass := &lint.Pass{
		Context:     context.Background(),
		FileSet:     fset,
		ASTFile:     astFile,
		RootIR:      rootIR,
		SourceBytes: []byte(src),
		FilePath:    apiFile,
	}

	rule := &lint.RuleRedundantTag{}
	diags := rule.Run(pass)

	require.Len(t, diags, 1)
	require.Equal(t, "W004", diags[0].RuleID)
	require.True(t, diags[0].Fixable())

	// Apply fix
	err = diags[0].Fix.Apply()
	require.NoError(t, err)

	cleaned, err := os.ReadFile(apiFile)
	require.NoError(t, err)
	require.NotContains(t, string(cleaned), `@query "item_id"`)
	require.Contains(t, string(cleaned), `itemID string,`)
}

func TestRules_CanonicalFormat_ReordersDirectives(t *testing.T) {
	tmpDir := t.TempDir()
	apiFile := filepath.Join(tmpDir, "api.go")

	src := `package api

// @aoni:service
type ProfileAPI interface {
	// @unwrap "data"
	// @preset :xhr
	// @get /profile/{steamID}
	GetProfile(ctx context.Context, steamID uint64) error
}
`
	err := os.WriteFile(apiFile, []byte(src), 0o600)
	require.NoError(t, err)

	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, apiFile, src, parser.ParseComments)
	require.NoError(t, err)

	pass := &lint.Pass{
		Context:     context.Background(),
		FileSet:     fset,
		ASTFile:     astFile,
		SourceBytes: []byte(src),
		FilePath:    apiFile,
	}

	rule := &lint.RuleCanonicalFormat{}
	diags := rule.Run(pass)

	require.Len(t, diags, 1)
	require.Equal(t, "W005", diags[0].RuleID)
	require.True(t, diags[0].Fixable())

	// Apply fix
	err = diags[0].Fix.Apply()
	require.NoError(t, err)

	reordered, err := os.ReadFile(apiFile)
	require.NoError(t, err)

	expectedBlock := `	// @get /profile/{steamID}
	// @preset :xhr
	// @unwrap "data"`

	require.Contains(t, string(reordered), expectedBlock)
}

func TestRules_InvalidBitpack_DetectsErrors(t *testing.T) {
	rootIR := &ir.RootIR{
		Bitpacks: []*ir.BitpackIR{
			{
				Name: "BadHeader",
				Fields: []*ir.BitpackFieldIR{
					{
						GoName:   "Flag",
						Type:     ir.GoTypeIR{Name: "bool"},
						BitWidth: 2, // Error: bool must be 1 bit
						IsBool:   true,
					},
					{
						GoName:   "SmallInt",
						Type:     ir.GoTypeIR{Name: "uint8"},
						BitWidth: 10, // Error: uint8 exceeds 8 bits
					},
				},
				TotalBits:  12,
				TotalBytes: 2,
			},
		},
	}

	pass := &lint.Pass{
		RootIR:   rootIR,
		FilePath: "bad.go",
	}

	rule := &lint.RuleInvalidBitpack{}
	diags := rule.Run(pass)

	require.Len(t, diags, 2)
	require.Equal(t, "E012", diags[0].RuleID)
	require.Contains(t, diags[0].Message, "Bool field Flag must have bit width 1")
	require.Equal(t, "E012", diags[1].RuleID)
	require.Contains(t, diags[1].Message, "exceeds maximum type bit width 8")
}

func TestRules_ProtocolErrors_Detection(t *testing.T) {
	// E007: illegal body on GET
	rootIR := &ir.RootIR{
		Services: []*ir.ServiceIR{
			{
				Name: "TestService",
				Methods: []*ir.MethodIR{
					{
						Name:       "GetWithBody",
						HTTPMethod: "GET",
						Params: []*ir.ParamIR{
							{GoName: "Payload", Location: ir.LocBody},
						},
					},
					{
						Name:       "RouteA",
						HTTPMethod: "POST",
						Path:       &ir.PathIR{RawTemplate: "/orders"},
					},
					{
						Name:       "RouteB",
						HTTPMethod: "POST",
						Path:       &ir.PathIR{RawTemplate: "/orders"},
					},
				},
			},
		},
	}

	pass := &lint.Pass{RootIR: rootIR, FilePath: "api.go"}

	// Test E007
	r7 := &lint.RuleIllegalBodyMethod{}
	d7 := r7.Run(pass)
	require.Len(t, d7, 1)
	require.Equal(t, "E007", d7[0].RuleID)
	require.Contains(t, d7[0].Message, "cannot have a request body")

	// Test E010
	r10 := &lint.RuleDuplicateRouteCollision{}
	d10 := r10.Run(pass)
	require.Len(t, d10, 1)
	require.Equal(t, "E010", d10[0].RuleID)
	require.Contains(t, d10[0].Message, "Duplicate route collision")
}

func TestRules_SecurityAndPerf_Detection(t *testing.T) {
	rootIR := &ir.RootIR{
		Services: []*ir.ServiceIR{
			{
				Name: "AuthService",
				Methods: []*ir.MethodIR{
					{
						Name:       "Login",
						HTTPMethod: "GET",
						Params: []*ir.ParamIR{
							{GoName: "Password", Location: ir.LocQuery, WireKey: "password"},
							{
								GoName:   "Tags",
								Location: ir.LocQuery,
								WireKey:  "tags",
								GoType:   ir.GoTypeIR{IsSlice: true},
							},
						},
					},
					{
						Name:       "Scrape",
						HTTPMethod: "GET",
						Extract:    &ir.ExtractIR{Kind: ir.ExtractBetween},
					},
				},
			},
		},
	}

	pass := &lint.Pass{RootIR: rootIR, FilePath: "auth.go"}

	// S001: Sensitive query param
	rS1 := &lint.RuleSensitiveQueryParam{}
	dS1 := rS1.Run(pass)
	require.Len(t, dS1, 1)
	require.Equal(t, "S001", dS1[0].RuleID)
	require.Contains(t, dS1[0].Message, "Sensitive credential")

	// S004: Naked scraper
	rS4 := &lint.RuleNakedScraperContract{}
	dS4 := rS4.Run(pass)
	require.Len(t, dS4, 1)
	require.Equal(t, "S004", dS4[0].RuleID)
	require.Contains(t, dS4[0].Message, "without service-level @persona")

	// P003: Unformatted slice strategy
	rP3 := &lint.RuleUnformattedSliceStrategy{}
	dP3 := rP3.Run(pass)
	require.Len(t, dP3, 1)
	require.Equal(t, "P003", dP3[0].RuleID)
}

func TestRules_StyleAndHygiene_Detection(t *testing.T) {
	rootIR := &ir.RootIR{
		Services: []*ir.ServiceIR{
			{
				Name: "OrderService",
				Methods: []*ir.MethodIR{
					{
						Name:          "DeleteOrder",
						HTTPMethod:    "DELETE",
						LocalCacheTTL: "5m", // Dead @cache on DELETE
						UnwrapField:   "data",
						Return:        &ir.ReturnIR{IsVoid: true}, // Dead @unwrap on void
						ExpectStatus:  []int{999},                 // Invalid status code
					},
				},
			},
		},
	}

	pass := &lint.Pass{RootIR: rootIR, FilePath: "order.go"}

	// W006: Dead directive
	rW6 := &lint.RuleDeadDirective{}
	dW6 := rW6.Run(pass)
	require.Len(t, dW6, 2)
	require.Equal(t, "W006", dW6[0].RuleID)
	require.Equal(t, "W006", dW6[1].RuleID)

	// W008: Invalid status code range
	rW8 := &lint.RuleInvalidStatusCodeRange{}
	dW8 := rW8.Run(pass)
	require.Len(t, dW8, 1)
	require.Equal(t, "W008", dW8[0].RuleID)
	require.Contains(t, dW8[0].Message, "outside valid RFC range")
}

func TestRules_InvalidUnionStatus(t *testing.T) {
	rootIR := &ir.RootIR{
		PackageName: "orderapi",
		Unions: []*ir.UnionIR{
			{
				Name: "EmptyUnion",
			},
			{
				Name: "MissingTagsUnion",
				Fields: []*ir.UnionFieldIR{
					{GoName: "Order", Type: ir.GoTypeIR{Name: "*Order"}},
				},
			},
		},
	}

	pass := &lint.Pass{RootIR: rootIR, FilePath: "order.go"}
	rE14 := &lint.RuleInvalidUnionStatus{}
	diags := rE14.Run(pass)
	require.Len(t, diags, 2)
	require.Equal(t, "E014", diags[0].RuleID)
	require.Equal(t, "E014", diags[1].RuleID)
	require.Contains(t, diags[0].Message, "has no variant fields")
	require.Contains(t, diags[1].Message, "missing `status:\"...\"` tag")
}

func TestRules_DuplicateOpID_And_DeprecatedTarget(t *testing.T) {
	rootIR := &ir.RootIR{
		PackageName: "testapi",
		Services: []*ir.ServiceIR{
			{
				Name: "TestAPI",
				Methods: []*ir.MethodIR{
					{
						Name:        "GetItem",
						OperationID: "api_get_item",
					},
					{
						Name:        "FetchItem",
						OperationID: "api_get_item", // Duplicate opID
					},
					{
						Name: "OldMethod",
						Deprecation: &ir.DeprecationIR{
							Replacement: "NonExistentMethod",
						},
					},
				},
			},
		},
	}

	pass := &lint.Pass{RootIR: rootIR, FilePath: "api.go"}

	// W009
	rW9 := &lint.RuleDuplicateOperationID{}
	dW9 := rW9.Run(pass)
	require.Len(t, dW9, 1)
	require.Equal(t, "W009", dW9[0].RuleID)
	require.Contains(t, dW9[0].Message, "duplicate operation ID")

	// W010
	rW10 := &lint.RuleDeprecatedTargetValidation{}
	dW10 := rW10.Run(pass)
	require.Len(t, dW10, 1)
	require.Equal(t, "W010", dW10[0].RuleID)
	require.Contains(t, dW10[0].Message, "does not exist")
}
