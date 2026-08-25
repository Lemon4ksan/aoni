// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import "time"

// UserDTO represents a typed user entity returned by the API.
type UserDTO struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateUserRequest represents the JSON request payload to create a new user.
type CreateUserRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Role     string `json:"role"`
}

// LoginResponse represents the authentication payload returned upon login.
type LoginResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// RealtimeNotification represents an incoming WebSocket event message.
type RealtimeNotification struct {
	Event     string `json:"event"`
	UserID    string `json:"user_id"`
	Timestamp int64  `json:"timestamp"`
	Message   string `json:"message"`
}
