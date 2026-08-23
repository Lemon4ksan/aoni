// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package ir provides intermediate representations (IR) for declarative aoni API client code generation.
//
// The IR model decouples the source code AST and directive parsing layer from the code emission layer.
// It organizes API definitions across four hierarchical scopes:
//  1. Service Scope: Global client options, engine selection, base URLs, stealth/evasion profiles, L3/L4 settings.
//  2. Method Scope: Endpoint routing, HTTP/RPC methods, content types, streaming modes, response checks, and modifiers.
//  3. Parameter Scope: Method arguments mapped to path variables, query parameters, headers, bodies, or modifiers.
//  4. Return Scope: Typed DTOs, response envelope unwrapping, multi-status unions, and error models.
package ir
