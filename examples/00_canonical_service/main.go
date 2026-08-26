// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package main demonstrates the canonical, production-grade service architecture for Aoni.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	fheader "github.com/lemon4ksan/foundation/net/http/header"

	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/mod"
)

func main() {
	// 1. Spin up a test server simulating a REST microservice
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(fheader.ContentType, fheader.MIMEApplicationJSON)

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/auth/login":
			_ = r.ParseForm()
			if r.FormValue("username") == "alice" && r.FormValue("password") == "secret123" {
				_ = json.NewEncoder(w).Encode(LoginResponse{
					AccessToken: "aoni_jwt_token_987654321",
					TokenType:   "Bearer",
					ExpiresIn:   3600,
				})
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/users/"):
			id := strings.TrimPrefix(r.URL.Path, "/users/")
			if id == "user_1" {
				_ = json.NewEncoder(w).Encode(UserDTO{
					ID:        "user_1",
					Username:  "alice",
					Email:     "alice@example.com",
					Role:      "admin",
					CreatedAt: time.Now().UTC().Add(-24 * time.Hour),
				})
				return
			}
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"user not found"}`))

		case r.Method == http.MethodPost && r.URL.Path == "/users":
			var req CreateUserRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
				_ = json.NewEncoder(w).Encode(UserDTO{
					ID:        "user_2",
					Username:  req.Username,
					Email:     req.Email,
					Role:      req.Role,
					CreatedAt: time.Now().UTC(),
				})
				return
			}
			w.WriteHeader(http.StatusBadRequest)

		case r.Method == http.MethodGet && r.URL.Path == "/users":
			_ = json.NewEncoder(w).Encode([]UserDTO{
				{
					ID:        "user_1",
					Username:  "alice",
					Email:     "alice@example.com",
					Role:      "admin",
					CreatedAt: time.Now().UTC().Add(-24 * time.Hour),
				},
				{
					ID:        "user_2",
					Username:  "bob",
					Email:     "bob@example.com",
					Role:      "member",
					CreatedAt: time.Now().UTC(),
				},
			})
		}
	}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fmt.Println("🚀 Aoni Canonical Service Blueprint Demonstration")
	fmt.Printf("   Target API: %s\n\n", ts.URL)

	// 2. Instantiate canonical service with Fast zero-allocation engine
	service := NewUserService(fast.NewClient(), ts.URL)

	// 3. Authenticate with Form URL-encoded login
	fmt.Println("1️⃣  Authenticating via Form URL-Encoded Login...")
	auth, err := service.Login(ctx, "alice", "secret123")
	if err != nil {
		log.Fatalf("Login failed: %v", err)
	}
	fmt.Printf("   ✔ Token received: %s (expires in %ds)\n\n", auth.AccessToken, auth.ExpiresIn)

	// 4. Create new user with typed JSON payload & Bearer Auth
	fmt.Println("2️⃣  Creating user via JSON POST...")
	newUser, err := service.CreateUser(
		ctx,
		CreateUserRequest{
			Username: "charlie",
			Email:    "charlie@example.com",
			Role:     "engineer",
		},
		mod.WithBearer(auth.AccessToken),
	)
	if err != nil {
		log.Fatalf("Create user failed: %v", err)
	}
	fmt.Printf("   ✔ Created user: ID=%s, Username=%s, Role=%s\n\n", newUser.ID, newUser.Username, newUser.Role)

	// 5. Query user by ID
	fmt.Println("3️⃣  Fetching user by ID...")
	user, err := service.GetUser(ctx, "user_1", mod.WithBearer(auth.AccessToken))
	if err != nil {
		log.Fatalf("Get user failed: %v", err)
	}
	fmt.Printf("   ✔ Found user: %s <%s> (%s)\n\n", user.Username, user.Email, user.Role)

	// 6. Demonstrate typed 404 error handling
	fmt.Println("4️⃣  Testing not found error handling (IsNotFound)...")
	_, err = service.GetUser(ctx, "non_existent_id")
	if err != nil {
		fmt.Printf("   ✔ Cleanly caught expected error: %v\n\n", err)
	}

	// 7. Paginated list query
	fmt.Println("5️⃣  Listing users with query parameters...")
	users, err := service.ListUsers(ctx, 1, 10, mod.WithBearer(auth.AccessToken))
	if err != nil {
		log.Fatalf("List users failed: %v", err)
	}
	fmt.Printf("   ✔ Retrieved %d user(s):\n", len(users))
	for _, u := range users {
		fmt.Printf("      • %-8s %-12s %s\n", u.ID, u.Username, u.Email)
	}

	fmt.Println("\n✨ Canonical Aoni Service Blueprint executed with 0 errors!")
}
