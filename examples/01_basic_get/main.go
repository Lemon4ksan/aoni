// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example: Basic GET request with typed JSON response.
//
// Demonstrates creating a client with a base URL and performing
// a simple GET request to fetch a JSON resource into a Go struct.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/lemon4ksan/aoni"
)

type Post struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := aoni.NewClient(nil).
		WithBaseURL("https://jsonplaceholder.typicode.com")

	post, err := aoni.GetJSON[Post](ctx, client, "/posts/1")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Post %d: %s\n", post.ID, post.Title)
}
