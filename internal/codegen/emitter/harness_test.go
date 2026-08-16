// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package emitter_test

import (
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/codegen/emitter"
	"github.com/lemon4ksan/aoni/internal/codegen/ir"
)

func TestEmitter_Harness_Generation(t *testing.T) {
	t.Parallel()

	allocLimit := 0
	root := &ir.RootIR{
		PackageName: "itemservice",
		Services: []*ir.ServiceIR{
			{
				Name: "ItemService",
				Methods: []*ir.MethodIR{
					{
						Name:                "GetItem",
						HTTPMethod:          "GET",
						BenchWeight:         80,
						BudgetClientAllocs:  &allocLimit,
						BudgetMaxClientTime: "300ns",
						Path:                &ir.PathIR{RawTemplate: "/v1/items/{id}"},
						Params: []*ir.ParamIR{
							{GoName: "id", GoType: ir.GoTypeIR{Name: "uint64"}, Location: ir.LocPath},
						},
						Return: &ir.ReturnIR{
							SuccessType: ir.GoTypeIR{Name: "*ItemResponse", IsCustomType: true},
						},
					},
					{
						Name:        "CreateItem",
						HTTPMethod:  "POST",
						BenchWeight: 20,
						Path:        &ir.PathIR{RawTemplate: "/v1/items"},
						Params: []*ir.ParamIR{
							{GoName: "req", GoType: ir.GoTypeIR{Name: "CreateItemRequest"}, Location: ir.LocBody},
						},
						Return: &ir.ReturnIR{
							SuccessType: ir.GoTypeIR{Name: "*ItemResponse", IsCustomType: true},
						},
					},
				},
			},
		},
		Structs: []*ir.StructIR{
			{
				Name: "CreateItemRequest",
				Fields: []*ir.FieldIR{
					{GoName: "Title", WireName: "title", Type: ir.GoTypeIR{Name: "string"}},
					{GoName: "Price", WireName: "price", Type: ir.GoTypeIR{Name: "uint64"}},
				},
			},
			{
				Name: "ItemResponse",
				Fields: []*ir.FieldIR{
					{GoName: "ID", WireName: "id", Type: ir.GoTypeIR{Name: "uint64"}},
				},
			},
		},
	}

	emit := emitter.NewEmitter()
	code, err := emit.EmitHarness(root)
	require.NoError(t, err)
	assert.NotEmpty(t, code)

	codeStr := string(code)

	// Block 1: Feeders
	assert.Contains(t, codeStr, "type ItemServiceDataFeeder struct")
	assert.Contains(t, codeStr, "func (f *ItemServiceDataFeeder) FeedGetItemParams() uint64")
	assert.Contains(t, codeStr, "func (f *ItemServiceDataFeeder) FeedCreateItemRequest(dst *CreateItemRequest)")

	// Block 2: Actions & Scenarios
	assert.Contains(t, codeStr, "type ItemServiceGetItemAction struct")
	assert.Contains(t, codeStr, "type ItemServiceHarness struct")
	assert.Contains(t, codeStr, "func (h *ItemServiceHarness) Scenarios() []Scenario")

	// Block 3: Attribution & pprof
	assert.Contains(t, codeStr, "pprof.WithLabels")
	assert.Contains(t, codeStr, "type AttributionResult struct")

	// Block 4: Budget Asserter
	assert.Contains(t, codeStr, "func (h *ItemServiceHarness) VerifyBudget(ctx context.Context) error")
	assert.Contains(t, codeStr, "budget violation in ItemService.GetItem")

	// Validate valid Go syntax for harness
	fset := token.NewFileSet()
	_, parseErr := parser.ParseFile(fset, "api_harness.gen.go", code, parser.AllErrors)
	require.NoError(t, parseErr, "Generated harness must be 100% syntactically valid Go code")

	// Block 5: Native Benchmark & Fuzzing Targets in companion test file
	testCode, tErr := emit.EmitHarnessTests(root)
	require.NoError(t, tErr)
	assert.NotEmpty(t, testCode)

	testCodeStr := string(testCode)
	assert.Contains(t, testCodeStr, "func Benchmark_ItemService_GetItem(b *testing.B)")
	assert.Contains(t, testCodeStr, "func Benchmark_ItemService_CreateItem(b *testing.B)")
	assert.Contains(t, testCodeStr, "func Fuzz_ItemService_GetItem_Response(f *testing.F)")
	assert.Contains(t, testCodeStr, "func Fuzz_ItemService_CreateItem_Response(f *testing.F)")

	_, parseTestErr := parser.ParseFile(fset, "api_harness_test.go", testCode, parser.AllErrors)
	require.NoError(t, parseTestErr, "Generated harness test must be 100% syntactically valid Go code")
}
