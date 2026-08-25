// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/request"
)

// UserService represents the canonical service wrapper for managing user accounts.
type UserService struct {
	req  request.Requester
	doer aoni.RequestDoer
}

// NewUserService constructs a new UserService following Aoni's golden rules:
//  1. Accepts an aoni.RequestDoer interface (not concrete &http.Client).
//  2. Defaults to fast.NewClient() when doer is nil (zero-alloc engine).
//  3. Configures default baseURL, timeout, and options via request.Configure.
func NewUserService(doer aoni.RequestDoer, baseURL string, opts ...option.Option) *UserService {
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
		req:  request.Configure(doer, allOpts...),
		doer: doer,
	}
}
