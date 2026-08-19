// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package golden01

import (
	"context"

	"github.com/lemon4ksan/aoni"
)

// @aoni:service
type UserAPI interface {
	// @get "users/{id}"
	GetUser(ctx context.Context, id string, mods ...aoni.RequestModifier) (*UserDTO, error)

	// @post "users"
	CreateUser(ctx context.Context, req CreateUserRequest, mods ...aoni.RequestModifier) (*UserDTO, error)
}

type UserDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type CreateUserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}
