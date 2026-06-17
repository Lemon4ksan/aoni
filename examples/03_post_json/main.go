// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example: POST with JSON body and custom error model.
//
// Demonstrates PostJSON with WithJSONBody for creating resources,
// plus using a custom error response model to capture API errors.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/lemon4ksan/aoni"
)

type CreatePostRequest struct {
	Title  string `json:"title"`
	Body   string `json:"body"`
	UserID int    `json:"userId"`
}

type PostResponse struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	UserID int    `json:"userId"`
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := aoni.NewClient(nil).
		WithBaseURL("https://jsonplaceholder.typicode.com")

	payload := CreatePostRequest{
		Title:  "foo",
		Body:   "bar",
		UserID: 1,
	}

	result, err := aoni.PostJSON[CreatePostRequest, PostResponse](
		ctx, client, "/posts", payload,
		aoni.WithErrorModel(&ErrorResponse{}),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Created post %d: %s\n", result.ID, result.Title)
}
