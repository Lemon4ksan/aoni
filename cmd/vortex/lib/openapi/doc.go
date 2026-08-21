// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package openapi provides zero-dependency parsing, version normalization, multi-specification 3-way merging,
// and declarative Go contract generation for OpenAPI 2.0 (Swagger), 3.0, 3.1, and HAR traffic captures.
//
// # Architecture Overview
//
// The openapi package translates heterogeneous API specifications into strongly-typed, declarative Go interface contracts
// suitable for execution by the aoni network client:
//
//  1. Ingestion & In-Memory Normalization (loader.go, parser.go):
//     Accepts YAML or JSON bytes representing OpenAPI 3.1.0, OpenAPI 3.0.3, Swagger 2.0, or HAR archives.
//     Legacy Swagger 2.0 structures (host/basePath fragmentation, definitions, body parameters) are normalized
//     into unified OpenAPI 3.x Document AST without external heavy dependencies.
//
//  2. Multi-Specification Set Algebra (merge.go):
//     Performs 3-way specification composition using algebraic set operations:
//     - [MergeModeUnion] (A ∪ B): Combines all endpoints and unifies payload schemas (default).
//     - [MergeModeIntersection] (A ∩ B): Extracts common endpoints shared across all target specs.
//     - [MergeModeDifference] (A \ B): Computes delta endpoints present in source and absent in others.
//
//  3. Declarative Go Contract Generation (contract.go, models.go):
//     Generates clean, idiomatic Go interfaces equipped with aoni directives (@service, @get, @post, @query, @form, @unwrap)
//     and strongly-typed DTO structs with omitempty tags.
//
// # Supported Standards
//
//   - OpenAPI Specification v3.1.0: https://spec.openapis.org/oas/v3.1.0
//   - OpenAPI Specification v3.0.3: https://spec.openapis.org/oas/v3.0.3
//   - Swagger 2.0 Specification: https://swagger.io/specification/v2/
//   - RFC 9110 (HTTP Semantics): https://datatracker.ietf.org/doc/html/rfc9110
//   - RFC 6570 (URI Template): https://datatracker.ietf.org/doc/html/rfc6570
//   - RFC 3339 (Date and Time on the Internet): https://datatracker.ietf.org/doc/html/rfc3339
//   - JSON Schema Draft 2020-12: https://json-schema.org/draft/2020-12/json-schema-core.html
//
// # Example
//
//	doc, err := openapi.LoadSpecWithMode("api_v1.json,api_v2.json", nil, openapi.MergeModeUnion)
//	if err != nil {
//	    log.Fatalf("failed loading specs: %v", err)
//	}
//
//	apiCode, modelsCode, err := openapi.GenerateSplitContract(doc, openapi.ImportConfig{
//	    PackageName: "authapi",
//	    ServiceName: "AuthService",
//	})
//	if err != nil {
//	    log.Fatalf("failed generating contracts: %v", err)
//	}
package openapi
