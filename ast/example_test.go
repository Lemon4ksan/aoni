// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ast_test

import (
	"fmt"
	"log"

	"github.com/lemon4ksan/aoni/ast"
)

func Example() {
	// 1. Create a new Go file AST in package "github"
	file := ast.NewFile("github")

	// 2. Define an API service interface with BaseURL and Fast engine
	svc := file.NewService("GitHubAPI").
		WithBaseURL("https://api.github.com").
		WithEngine(ast.EngineFast).
		WithDoc("GitHubAPI provides programmatic client bindings for GitHub REST API.")

	// 3. Add an endpoint method: GET /users/{username} -> *User
	svc.NewMethod("GetUser", "GET", "users/{username}").
		WithDoc("GetUser retrieves public user profile information.").
		AddParam("username", "string").
		WithResponse("*User")

	// 4. Define the returned User DTO struct with JSON tags
	user := file.NewStruct("User").
		WithDoc("User represents a GitHub user account.")

	user.AddField("ID", "int64", "id", true)
	user.AddField("Login", "string", "login", true)
	user.AddField("Name", "string", "name", false)
	user.AddField("Bio", "*string", "bio", false)

	// 5. Render the AST tree into formatted Go source code
	code, err := ast.Format(file)
	if err != nil {
		log.Fatalf("failed to format AST: %v", err)
	}

	fmt.Println(string(code))
}
