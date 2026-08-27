// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
)

// GetUser retrieves a single user by ID using type-safe JSON decoding.
func (s *UserService) GetUser(ctx context.Context, id string, mods ...aoni.RequestModifier) (*UserDTO, error) {
	user, err := s.client.GetTo[UserDTO](ctx, "users/"+url.PathEscape(id), mods...)
	if err != nil {
		switch {
		case aoni.IsNotFound(err):
			return nil, fmt.Errorf("user %q does not exist: %w", id, err)
		case aoni.IsUnauthorized(err):
			return nil, fmt.Errorf("authentication required: %w", err)
		case aoni.IsRateLimited(err):
			return nil, fmt.Errorf("rate limit exceeded, please backoff: %w", err)
		default:
			return nil, fmt.Errorf("fetching user: %w", err)
		}
	}

	return user, nil
}

// CreateUser creates a new user sending a typed JSON body.
func (s *UserService) CreateUser(ctx context.Context, req CreateUserRequest, mods ...aoni.RequestModifier) (*UserDTO, error) {
	created, err := s.client.PostTo[UserDTO](ctx, "users", req, mods...)
	if err != nil {
		if aoni.IsForbidden(err) {
			return nil, fmt.Errorf("insufficient permissions to create user: %w", err)
		}

		return nil, fmt.Errorf("creating user: %w", err)
	}

	return created, nil
}

// Login performs form URL-encoded authentication.
func (s *UserService) Login(ctx context.Context, username, password string) (*LoginResponse, error) {
	form := url.Values{
		"username": {username},
		"password": {password},
	}

	loginResp, err := s.client.PostTo[LoginResponse](ctx, "auth/login", nil, mod.WithFormValues(form))
	if err != nil {
		if aoni.IsUnauthorized(err) {
			return nil, fmt.Errorf("invalid username or password: %w", err)
		}

		return nil, fmt.Errorf("logging in: %w", err)
	}

	return loginResp, nil
}

// ListUsers queries a paginated list of users with dynamic query parameters.
func (s *UserService) ListUsers(ctx context.Context, page, limit int, mods ...aoni.RequestModifier) ([]UserDTO, error) {
	queryMods := []aoni.RequestModifier{
		mod.WithQuery("page", strconv.Itoa(page)),
		mod.WithQuery("limit", strconv.Itoa(limit)),
	}

	allMods := append(queryMods, mods...)

	users, err := s.client.GetTo[[]UserDTO](ctx, "users", allMods...)
	if err != nil {
		return nil, fmt.Errorf("listing users: %w", err)
	}

	if users == nil {
		return []UserDTO{}, nil
	}

	return *users, nil
}
