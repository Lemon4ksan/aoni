// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example: Path variables and query parameters.
//
// Demonstrates {id} path variable substitution via WithVar/WithVars,
// struct-based query parameters, and automated fallback to 'default' tags.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/lemon4ksan/aoni"
)

type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type Comment struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Body string `json:"body"`
}

type Post struct {
	ID     int    `json:"id"`
	UserID int    `json:"userId"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

type PostFilter struct {
	UserID int    `url:"userId" default:"1"`
	Limit  int    `url:"limit"  default:"3"`
	Sort   string `url:"sort"   default:"id"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := aoni.NewClient(nil).
		WithBaseURL("https://jsonplaceholder.typicode.com")

	// 1. Path variable: fetch user /users/{id}
	user, err := aoni.GetJSON[User](ctx, client,
		"/users/{id}",
		aoni.WithVar("id", 1),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("User %d: %s <%s>\n", user.ID, user.Name, user.Email)

	// 2. Path + query: fetch comments /posts/{id}/comments?postId={id}
	// By passing a struct to WithQuery, aoni automatically encodes its fields.
	params := struct {
		PostID int `url:"postId"`
	}{PostID: 1}

	comments, err := aoni.GetJSON[[]Comment](ctx, client,
		"/posts/{id}/comments",
		aoni.WithVar("id", 1),
		aoni.WithQuery(params),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Got %d comments for post 1\n", len(*comments))

	// 3. Automated Defaults: fetch filtered posts /posts?userId=1&_limit=3&_sort=id
	// By passing an empty filter, aoni detects that fields are zero-valued
	// and automatically applies the values defined in 'default' tags.
	posts, err := aoni.GetJSON[[]Post](ctx, client,
		"/posts",
		aoni.WithQuery(PostFilter{}), // Zero-value struct -> defaults will be applied!
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("\nFetched %d posts using automated defaults:\n", len(*posts))

	for _, p := range *posts {
		fmt.Printf("  - Post %d: %s (User %d)\n", p.ID, p.Title, p.UserID)
	}
}
