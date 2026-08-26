// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/option"
)

// UserService represents the canonical service wrapper for managing user accounts.
type UserService struct {
	client *aoni.Client
}

// NewUserService constructs a new UserService following Aoni's golden rules:
//  1. Accepts an any (aoni.RequestDoer / aoni.HTTPRequester / nil).
//  2. Defaults to fast.NewClient() when doer is nil (zero-alloc engine).
//  3. Configures default baseURL, timeout, and options via aoni.NewClient.
func NewUserService(doer any, baseURL string, opts ...option.Option) *UserService {
	if doer == nil {
		// Zero-allocation line speed engine by default
		doer = fast.NewClient()
	}

	defaultOpts := []option.Option{
		option.WithBaseURL(baseURL),
		option.WithTimeout(10 * time.Second),
	}

	allOpts := append(defaultOpts, opts...)

	return &UserService{
		client: aoni.NewClient(doer, allOpts...),
	}
}
