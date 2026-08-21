// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package option

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/netutil"
)

// WithBaseURL returns an [aoni.ClientOption] setting the default Base URI for resolving relative request paths (RFC 3986 §5.1).
//
// In accordance with RFC 3986 §5.2.3 (Merge Paths) and §6.2.3 (Scheme-Based Normalization),
// a trailing slash is ensured so that relative paths (e.g. "users") are resolved as subpaths
// within the base directory rather than replacing the final path segment of the Base URI.
func WithBaseURL(raw string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if raw == "" {
			cfg.Defaults.BaseURL = &url.URL{}
			return
		}

		formatted := raw
		if !strings.HasSuffix(formatted, "/") {
			formatted += "/"
		}

		baseURL, err := url.Parse(formatted)
		if err != nil {
			return
		}

		cfg.Defaults.BaseURL = baseURL
	}
}

// WithHeader returns an [aoni.ClientOption] adding a default header key-value pair sent with every request.
func WithHeader(key, value string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		cfg.Defaults.Headers.Set(key, value)
	}
}

// WithHeaderFunc returns an [aoni.ClientOption] setting a dynamic header evaluated via provider on every request.
func WithHeaderFunc(key string, provider func() string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if key == "" || provider == nil {
			return
		}

		cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, mod.WithHeaderFunc(key, provider))
	}
}

// WithHeaders returns an [aoni.ClientOption] merging a map of default headers into the client configuration.
func WithHeaders(headers map[string]string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header, len(headers))
		}

		for k, v := range headers {
			cfg.Defaults.Headers.Set(k, v)
		}
	}
}

// WithoutHeaders returns an [aoni.ClientOption] purging all default request headers.
func WithoutHeaders() aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.Headers = make(http.Header)
	}
}

// WithUserAgent returns an [aoni.ClientOption] overriding the default User-Agent header field.
func WithUserAgent(ua string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		cfg.Defaults.Headers.Set("User-Agent", ua)
	}
}

// WithUARotationProfiles returns an [aoni.ClientOption] configuring browser profiles for automatic User-Agent rotation.
func WithUARotationProfiles(profiles []aoni.BrowserProfile) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.UARotationProfiles = profiles
	}
}

// WithOrigin returns an [aoni.ClientOption] setting a default Origin header.
func WithOrigin(origin string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		cfg.Defaults.Headers.Set("Origin", origin)
	}
}

// WithBearer returns an [aoni.ClientOption] setting a default "Authorization: Bearer <token>" header (RFC 6750 §2.1).
func WithBearer(token string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		cfg.Defaults.Headers.Set("Authorization", netutil.FormatBearerAuth(token))
	}
}

// WithBasicAuth returns an [aoni.ClientOption] setting default HTTP Basic Authentication credentials (RFC 7617).
func WithBasicAuth(username, password string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		cfg.Defaults.Headers.Set("Authorization", netutil.FormatBasicAuth(username, password))
	}
}

// WithDigestAuth returns an [aoni.ClientOption] enabling RFC 7616 HTTP Digest Access Authentication
// for transparently resolving HTTP 401 Digest challenges.
func WithDigestAuth(username, password string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.DigestAuth = &aoni.DigestAuthConfig{
			Username: username,
			Password: password,
		}
	}
}

// WithRefererAutomaton returns an [aoni.ClientOption] toggling automatic Referer header tracking across requests.
func WithRefererAutomaton(enabled bool) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.RefererAutomaton = enabled
	}
}

// WithEnvHeader returns an [aoni.ClientOption] setting a request header from an environment variable if present.
func WithEnvHeader(headerName, envVarName string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		val := os.Getenv(envVarName)
		if val == "" {
			return
		}

		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		cfg.Defaults.Headers.Set(headerName, val)
	}
}

// WithEnvBearer returns an [aoni.ClientOption] setting "Authorization: Bearer <val>" from an environment variable if present.
func WithEnvBearer(envVarName string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		val := os.Getenv(envVarName)
		if val == "" {
			return
		}

		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		cfg.Defaults.Headers.Set("Authorization", "Bearer "+val)
	}
}

// FromVortexCache returns an [aoni.ClientOption] discovering and injecting credentials from .vortex/cache/secrets.json.
func FromVortexCache(startDirs ...string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		startDir := "."
		if len(startDirs) > 0 && startDirs[0] != "" {
			startDir = startDirs[0]
		}

		curr, err := filepath.Abs(startDir)
		if err != nil {
			curr = startDir
		}

		var secretsData []byte

		for {
			candidate := filepath.Join(curr, ".vortex", "cache", "secrets.json")
			if data, err := os.ReadFile(candidate); err == nil {
				secretsData = data
				break
			}

			parent := filepath.Dir(curr)
			if parent == curr || parent == "" {
				break
			}

			curr = parent
		}

		if len(secretsData) == 0 {
			return
		}

		type vaultSecretEntry struct {
			Key    string `json:"key"`
			Value  string `json:"value"`
			Header string `json:"header,omitempty"`
			Query  string `json:"query,omitempty"`
			Cookie string `json:"cookie,omitempty"`
		}

		type vaultWrapper struct {
			Secrets map[string]vaultSecretEntry `json:"secrets"`
		}

		var v vaultWrapper
		if err := json.Unmarshal(secretsData, &v); err != nil || len(v.Secrets) == 0 {
			return
		}

		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		for key, entry := range v.Secrets {
			val := entry.Value
			if val == "" {
				continue
			}

			if entry.Header != "" {
				if strings.EqualFold(entry.Header, "authorization") && !strings.HasPrefix(val, "Bearer ") {
					cfg.Defaults.Headers.Set("Authorization", "Bearer "+val)
				} else {
					cfg.Defaults.Headers.Set(entry.Header, val)
				}

				continue
			}

			switch strings.ToUpper(key) {
			case "AUTH_TOKEN", "BEARER_TOKEN", "API_TOKEN", "ACCESS_TOKEN":
				if !strings.HasPrefix(val, "Bearer ") {
					cfg.Defaults.Headers.Set("Authorization", "Bearer "+val)
				} else {
					cfg.Defaults.Headers.Set("Authorization", val)
				}

			case "API_KEY", "APIKEY", "X_API_KEY":
				cfg.Defaults.Headers.Set("X-API-Key", val)
			default:
				cfg.Defaults.Headers.Set(key, val)
			}
		}
	}
}
