// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"github.com/lemon4ksan/aoni/mod"
)

// Authenticator defines a pluggable authentication strategy for HTTP requests.
type Authenticator interface {
	AuthModifier() RequestModifier
}

// ClientConfiguringAuth is an optional capability interface implemented by authenticators
// that require client engine configuration (such as Digest Access Authentication).
type ClientConfiguringAuth interface {
	ConfigureClient(client HTTPRequester) HTTPRequester
}

type bearerAuth string

func (b bearerAuth) AuthModifier() RequestModifier {
	return mod.WithBearer(string(b))
}

// BearerAuth creates an [Authenticator] that sets an "Authorization: Bearer <token>" header.
func BearerAuth(token string) Authenticator {
	return bearerAuth(token)
}

type basicAuth struct {
	username string
	password string
}

func (b basicAuth) AuthModifier() RequestModifier {
	return mod.WithBasicAuth(b.username, b.password)
}

// BasicAuth creates an [Authenticator] that sets an "Authorization: Basic <base64>" header.
func BasicAuth(username, password string) Authenticator {
	return basicAuth{username: username, password: password}
}

type digestAuth struct {
	username string
	password string
}

func (d digestAuth) AuthModifier() RequestModifier {
	return RequestModifier{}
}

func (d digestAuth) ConfigureClient(client HTTPRequester) HTTPRequester {
	if c, ok := client.(*Client); ok {
		return c.With(func(cfg *Config) {
			cfg.Engine.DigestAuth = &DigestAuthConfig{
				Username: d.username,
				Password: d.password,
			}
		})
	}

	return client
}

// DigestAuth creates an [Authenticator] configuring RFC 7616 Digest Access Authentication.
func DigestAuth(username, password string) Authenticator {
	return digestAuth{username: username, password: password}
}
