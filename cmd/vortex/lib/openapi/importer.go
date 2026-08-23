// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package openapi provides parsing, loading, 3-way specification merging,
// and declarative Go contract generation for OpenAPI 2.0/3.0/3.1 and HAR specifications.
package openapi

import "fmt"

// ImportConfig controls how OpenAPI specifications are imported into aoni declarative contracts.
type ImportConfig struct {
	SpecFile       string
	SpecData       []byte
	PackageName    string
	ServiceName    string
	OutputFile     string
	ModelsFile     string
	BaseURL        string
	SkipDeprecated bool
	SplitModels    bool
	IncludePaths   []string
	ExcludePaths   []string
	TypeMap        map[string]string
	MergeMode      MergeMode // "union", "intersect", "diff"
}

// ImportResult captures the outcome of an OpenAPI contract generation pass.
type ImportResult struct {
	ContractCode  []byte
	ModelsCode    []byte
	ServicesCount int
	MethodsCount  int
	StructsCount  int
}

// Import loads an OpenAPI specification and translates it into declarative aoni Go contracts.
func Import(cfg ImportConfig) (*ImportResult, error) {
	mode := cfg.MergeMode
	if mode == "" {
		mode = MergeModeUnion
	}

	spec, err := LoadSpecWithMode(cfg.SpecFile, cfg.SpecData, mode)
	if err != nil {
		return nil, fmt.Errorf("vortex/openapi: load spec: %w", err)
	}

	code, err := GenerateContract(spec, cfg)
	if err != nil {
		return nil, fmt.Errorf("vortex/openapi: generate contract: %w", err)
	}

	res := &ImportResult{
		ContractCode:  code,
		ServicesCount: 1,
	}

	if spec.Paths != nil {
		res.MethodsCount = len(spec.Paths)
	}

	if spec.Components != nil {
		res.StructsCount = len(spec.Components.Schemas)
	}

	return res, nil
}
