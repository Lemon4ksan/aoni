// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package oracle provides universal browser attestation bridge clients,
// sidecar supervisors, and request modifiers for aoni.
package oracle

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/request"
)

// DefaultPort is the default local HTTP port for oracle sidecar bridges.
const DefaultPort = 64055

// DefaultBaseURL is the default local sidecar URL.
const DefaultBaseURL = "http://127.0.0.1:64055"

// InitRequest is the payload for initializing the browser session.
type InitRequest struct {
	Cookies string `json:"cookies,omitempty"`
}

// InitResponse is the response from the browser initialization.
type InitResponse struct {
	Status string `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
}

// TokenRequest is the payload for requesting a signature token.
type TokenRequest struct {
	Flow    string `json:"flow,omitempty"`
	Content string `json:"content,omitempty"`
}

// TokenResponse contains the generated signature token, intercepted headers, and matching live cookies.
type TokenResponse struct {
	Status  string            `json:"status,omitempty"`
	Token   string            `json:"token,omitempty"`
	Cookies string            `json:"cookies,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Source  string            `json:"source,omitempty"`
	Error   string            `json:"error,omitempty"`
}

// StatusResponse contains sidecar health status.
type StatusResponse struct {
	Status       string `json:"status,omitempty"`
	Ready        bool   `json:"ready,omitempty"`
	PageURL      string `json:"pageUrl,omitempty"`
	PoolSize     int    `json:"poolSize,omitempty"`
	IdleTabs     int    `json:"idleTabs,omitempty"`
	WaitingQueue int    `json:"waitingQueue,omitempty"`
	Error        string `json:"error,omitempty"`
}

// Client provides HTTP communication with the browser oracle sidecar.
type Client struct {
	baseURL string
	client  *aoni.Client
}

// NewClient creates a new Oracle sidecar client.
func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	baseURL = strings.TrimRight(baseURL, "/")

	return &Client{
		baseURL: baseURL,
		client:  aoni.NewClient(nil),
	}
}

// BaseURL returns the configured base URL.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// Status checks if the sidecar bridge is healthy and ready.
func (c *Client) Status(ctx context.Context) (*StatusResponse, error) {
	resp, err := request.GetTo[StatusResponse](ctx, c.client, c.baseURL+"/status")
	if err != nil {
		return nil, fmt.Errorf("checking oracle status: %w", err)
	}

	return resp, nil
}

// Init initializes the browser session with optional cookies.
func (c *Client) Init(ctx context.Context, cookies string) error {
	req := InitRequest{Cookies: cookies}

	resp, err := request.PostTo[InitResponse](ctx, c.client, c.baseURL+"/init", req)
	if err != nil {
		return fmt.Errorf("initializing oracle browser: %w", err)
	}

	if resp.Error != "" {
		return fmt.Errorf("oracle initialization error: %s", resp.Error)
	}

	return nil
}

// GetToken requests a fresh attestation token for the default flow.
func (c *Client) GetToken(ctx context.Context, content string) (*TokenResponse, error) {
	return c.GetTokenWithFlow(ctx, "", content)
}

// GetTokenWithFlow requests a fresh attestation token for a named flow.
func (c *Client) GetTokenWithFlow(ctx context.Context, flow, content string) (*TokenResponse, error) {
	req := TokenRequest{
		Flow:    flow,
		Content: content,
	}

	resp, err := request.PostTo[TokenResponse](ctx, c.client, c.baseURL+"/token", req)
	if err != nil {
		return nil, fmt.Errorf("fetching oracle token: %w", err)
	}

	if resp.Error != "" {
		return nil, fmt.Errorf("oracle token error: %s", resp.Error)
	}

	if resp.Token == "" {
		return nil, errors.New("oracle returned empty token")
	}

	return resp, nil
}
