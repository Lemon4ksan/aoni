// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package generator

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/lemon4ksan/aoni/ast"
	"github.com/lemon4ksan/aoni/oracle/spec"
)

// GenerateGo compiles an OracleSpec AST into strongly-typed Go declarative contracts using aoni/ast.
func GenerateGo(s *spec.OracleSpec, pkgName string) ([]byte, error) {
	if s == nil {
		return nil, errors.New("oracle spec is nil")
	}

	if pkgName == "" {
		pkgName = "oracle"
	}

	port := s.Port
	if port <= 0 {
		port = 64055
	}

	f := ast.NewFile(pkgName)
	f.AddImport("github.com/lemon4ksan/aoni", "")
	f.AddImport("github.com/lemon4ksan/aoni/oracle", "")

	svcName := capitalize(s.Name) + "API"
	svc := f.NewService(svcName).
		WithBaseURL("http://127.0.0.1:" + strconv.Itoa(port)).
		WithEngine(ast.EngineFast).
		WithCasing(ast.CasingSnake).
		WithDoc(
			fmt.Sprintf("%s provides declarative HTTP client bindings for the %s oracle sidecar bridge.", svcName, s.Name),
		)

	// Status method
	svc.NewMethod("Status", "GET", "/status").
		WithResponse("*oracle.StatusResponse").
		WithDoc("Status checks if the sidecar bridge is alive.")

	// Init method
	svc.NewMethod("Init", "POST", "/init").
		WithRequest("*oracle.InitRequest").
		WithResponse("*oracle.InitResponse").
		WithDoc("Init initializes the browser session with optional cookies.")

	// Named flow methods
	for _, flow := range s.Flows {
		methodName := capitalize(flow.Name)
		svc.NewMethod(methodName, "POST", "/token").
			WithRequest("*oracle.TokenRequest").
			WithResponse("*oracle.TokenResponse").
			WithDoc(fmt.Sprintf("%s requests an attestation signature token for the %s flow.", methodName, flow.Name))
	}

	return ast.Format(f)
}

func capitalize(s string) string {
	if s == "" {
		return ""
	}

	return strings.ToUpper(s[:1]) + s[1:]
}
