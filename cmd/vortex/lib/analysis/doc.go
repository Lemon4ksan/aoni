// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package analysis provides an extensible, rule-based semantic analysis and compile-time verification engine
// for API Intermediate Representation ([ir.RootIR]).
//
// # Architecture Overview
//
// The analysis engine models verification passes as composable rule functions executed across distinct AST levels:
//   - [RootRule]: Package-wide checks (e.g. global type name uniqueness, unrecognized directive inspection).
//   - [ServiceRule]: Service-level checks (e.g. method uniqueness, interface completeness, timeout policies).
//   - [MethodRule]: Endpoint-level checks (e.g. context parameter position, path/header variable binding, RFC 9110 payload warnings).
//   - [StructRule]: DTO-level checks (e.g. wire name uniqueness and serialization collision prevention).
//   - [TupleRule]: Tuple-level checks (e.g. non-empty positional element verification).
//   - [BitpackRule]: Bitfield-level checks (e.g. non-empty bitpack element verification).
//
// # Extensibility & Custom Adapters
//
// Custom language-specific or framework-specific verification pipelines can be assembled by instantiating [NewEngine]
// and registering targeted rule sets:
//
//	engine := analysis.NewEngine()
//	engine.AddMethodRules(
//	    analysis.RuleMethodHTTPDirective,
//	    analysis.RuleMethodPathVariables,
//	    customPythonAsyncRule,
//	)
//	diags := engine.Run(rootIR)
package analysis
