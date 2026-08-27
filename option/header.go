// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package option

import (
	"crypto"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	fheader "github.com/lemon4ksan/foundation/net/http/header"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/netutil"
	"github.com/lemon4ksan/aoni/netutil/dpop"
	"github.com/lemon4ksan/aoni/netutil/httpsig"
	"github.com/lemon4ksan/aoni/netutil/priority"
	"github.com/lemon4ksan/aoni/netutil/secret"
)

// WithPriority sets the default RFC 9218 "Priority" header on every outbound request.
//
// Urgency ranges from 0 (highest/critical) to 7 (lowest/background). Incremental specifies whether
// the response can be streamed incrementally.
//
// # Wire Representation
//
//	Priority: u=1, i
//
// # RFC Compliance
//
// Conforms to RFC 9218 (Extensible Prioritization Scheme for HTTP).
func WithPriority(urgency int, incremental bool) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, mod.WithPriority(urgency, incremental))
	}
}

// WithPriorityPreset sets a default RFC 9218 "Priority" header from a structured [priority.Priority] preset.
func WithPriorityPreset(p priority.Priority) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, mod.WithPriorityPreset(p))
	}
}

// WithBaseURL sets the default Base URI for resolving relative request paths (RFC 3986 §5.1).
//
// # RFC 3986 Resolution & Slash Normalization
//
// Under RFC 3986 §5.2, a Base URI should ideally include a trailing slash (e.g. "https://api.example.com/v1/")
// to signify a directory component, ensuring relative paths (e.g. "users") do not displace the final path segment.
//
// aoni automatically normalizes slashes to prevent common routing errors:
//   - Appends a trailing slash if missing when parsing base URLs.
//   - Concatenates leading-slash request paths ("/users") onto subpath bases (".../v1/") safely into ".../v1/users".
//   - Deduplicates boundary slashes ("//") on the zero-allocation fast path.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithBaseURL("https://api.github.com"),
//	)
//	// Performs GET to "https://api.github.com/users/octocat"
//	res, err := client.GetTo[User](ctx, "/users/octocat")
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

// WithHeader sets a static default HTTP header key-value pair sent with every request.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithHeader("X-App-Version", "1.4.2"),
//	)
func WithHeader(key, value string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		cfg.Defaults.Headers.Set(key, value)
	}
}

// WithHeaderFunc configures a dynamic header value evaluated dynamically at request execution time.
//
// Useful for rotating tokens, timestamp nonces, or dynamic tracing identifiers.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithHeaderFunc("X-Timestamp", func() string {
//	        return strconv.FormatInt(time.Now().Unix(), 10)
//	    }),
//	)
func WithHeaderFunc(key string, provider func() string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if key == "" || provider == nil {
			return
		}

		cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, mod.WithHeaderFunc(key, provider))
	}
}

// WithHeaders merges a map of default HTTP headers into the client configuration.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithHeaders(map[string]string{
//	        "Accept": "application/json",
//	        "X-Environment": "production",
//	    }),
//	)
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

// WithoutHeaders purges all default request headers, providing a clean baseline.
func WithoutHeaders() aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.Headers = make(http.Header)
	}
}

// WithUserAgent overrides the default User-Agent request header string.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithUserAgent("MyBot/1.0 (+https://example.com/bot)"),
//	)
func WithUserAgent(ua string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		cfg.Defaults.Headers.Set(fheader.UserAgent, ua)
	}
}

// WithUARotationProfiles registers a slice of browser profiles for automated per-request User-Agent rotation.
func WithUARotationProfiles(profiles []aoni.BrowserProfile) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.UARotationProfiles = profiles
	}
}

// WithOrigin sets a static default Origin header field.
func WithOrigin(origin string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		cfg.Defaults.Headers.Set(fheader.Origin, origin)
	}
}

// WithBearer sets a static default HTTP Authorization Bearer token (RFC 6750 §2.1).
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithBearer("ghp_secret_access_token_123"),
//	)
//
// # RFC Compliance
//
// Conforms to RFC 6750 (OAuth 2.0 Bearer Token Usage).
func WithBearer(token string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		cfg.Defaults.Headers.Set(fheader.Authorization, netutil.FormatBearerAuth(token))
	}
}

// WithSecretBearer sets a default Bearer token extracted from a protected [secret.Secret] memory container.
func WithSecretBearer(token secret.Secret[string]) aoni.ClientOption {
	return WithBearer(token.Value())
}

// WithBasicAuth sets static HTTP Basic Authentication credentials (RFC 7617).
//
// Automatically base64 encodes the credentials and injects "Authorization: Basic <base64>".
//
// # RFC Compliance
//
// Conforms to RFC 7617 (The 'Basic' HTTP Authentication Scheme).
func WithBasicAuth(username, password string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		cfg.Defaults.Headers.Set(fheader.Authorization, netutil.FormatBasicAuth(username, password))
	}
}

// WithSecretBasicAuth sets default HTTP Basic Authentication with password protected by [secret.Secret].
func WithSecretBasicAuth(username string, password secret.Secret[string]) aoni.ClientOption {
	return WithBasicAuth(username, password.Value())
}

// WithDigestAuth enables RFC 7616 HTTP Digest Access Authentication
// for transparently resolving HTTP 401 Digest challenges across requests.
//
// # RFC Compliance
//
// Conforms to RFC 7616 (HTTP Digest Access Authentication).
func WithDigestAuth(username, password string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.DigestAuth = &aoni.DigestAuthConfig{
			Username: username,
			Password: password,
		}
	}
}

// WithHTTPSignature applies an RFC 9421 HTTP Message Signature to every outbound request.
//
// Cryptographically signs specified headers, method, path, and body digest using asymmetric or symmetric keys.
//
// # RFC Compliance
//
// Conforms to RFC 9421 (HTTP Message Signatures).
func WithHTTPSignature(sigCfg httpsig.SignConfig) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, mod.WithHTTPSignature(sigCfg))
	}
}

// WithDPoPToken sets a default OAuth 2.0 DPoP-bound access token (RFC 9449 §7.1).
//
// Generates and attaches a fresh DPoP Proof JWT signed with privKey for every request.
//
// # RFC Compliance
//
// Conforms to RFC 9449 (OAuth 2.0 Demonstrating Proof-of-Possession at the Application Layer).
func WithDPoPToken(accessToken string, privKey crypto.PrivateKey, opts ...dpop.ProofOptions) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, mod.WithDPoPToken(accessToken, privKey, opts...))
	}
}

// WithRefererAutomaton toggles automatic Referer header tracking, simulating browser navigation chains.
func WithRefererAutomaton(enabled bool) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.RefererAutomaton = enabled
	}
}

// WithEnvHeader reads an environment variable and sets it as a default header if present.
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

// WithEnvBearer reads an environment variable and injects it as "Authorization: Bearer <val>" if present.
func WithEnvBearer(envVarName string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		val := os.Getenv(envVarName)
		if val == "" {
			return
		}

		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		cfg.Defaults.Headers.Set(fheader.Authorization, "Bearer "+val)
	}
}

// FromVortexCache searches parent directories for .vortex/cache/secrets.json and injects credentials.
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
					cfg.Defaults.Headers.Set(fheader.Authorization, "Bearer "+val)
				} else {
					cfg.Defaults.Headers.Set(entry.Header, val)
				}

				continue
			}

			switch strings.ToUpper(key) {
			case "AUTH_TOKEN", "BEARER_TOKEN", "API_TOKEN", "ACCESS_TOKEN":
				if !strings.HasPrefix(val, "Bearer ") {
					cfg.Defaults.Headers.Set(fheader.Authorization, "Bearer "+val)
				} else {
					cfg.Defaults.Headers.Set(fheader.Authorization, val)
				}

			case "API_KEY", "APIKEY", "X_API_KEY":
				cfg.Defaults.Headers.Set("X-API-Key", val)
			default:
				cfg.Defaults.Headers.Set(key, val)
			}
		}
	}
}
