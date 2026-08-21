// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lemon4ksan/foundation/generic"
)

// SecretEntry represents a single credentials or session token stored in the local vault.
type SecretEntry struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	Masked    string    `json:"masked"`
	Origin    string    `json:"origin,omitempty"`
	Header    string    `json:"header,omitempty"`
	Query     string    `json:"query,omitempty"`
	Cookie    string    `json:"cookie,omitempty"`
	Target    string    `json:"target,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SecretsConfig specifies secret detection, header/query/cookie masking, and environment mappings.
type SecretsConfig struct {
	Headers  map[string]string `yaml:"headers,omitempty"`
	Query    map[string]string `yaml:"query,omitempty"`
	Cookies  map[string]string `yaml:"cookies,omitempty"`
	Body     map[string]string `yaml:"body,omitempty"`
	Paths    []SecretPathRule  `yaml:"paths,omitempty"`
	Patterns []SecretPattern   `yaml:"patterns,omitempty"`
}

// SecretPathRule associates a URL path segment pattern with a secret variable name.
type SecretPathRule struct {
	Pattern string `yaml:"pattern"`
	Var     string `yaml:"var"`
}

// SecretPattern associates a regular expression pattern with a secret variable name.
type SecretPattern struct {
	Regex string `yaml:"regex"`
	Var   string `yaml:"var"`
}

// GetHeaderEnv returns the configured or heuristically inferred environment variable name for a header.
func (sc *SecretsConfig) GetHeaderEnv(name string) (string, bool) {
	if sc != nil && len(sc.Headers) > 0 {
		for k, v := range sc.Headers {
			if strings.EqualFold(k, name) {
				return v, true
			}
		}
	}

	lower := strings.ToLower(name)
	switch lower {
	case "authorization":
		return "AUTH_TOKEN", true
	case "proxy-authorization":
		return "PROXY_AUTH_TOKEN", true
	}

	// Heuristics for standard API keys and tokens
	if strings.Contains(lower, "api-key") || strings.Contains(lower, "apikey") ||
		strings.Contains(lower, "token") || strings.Contains(lower, "secret") ||
		strings.Contains(lower, "auth") || strings.Contains(lower, "signature") ||
		strings.Contains(lower, "session-id") || strings.Contains(lower, "visit-id") ||
		strings.Contains(lower, "instanceid") || strings.Contains(lower, "connection-id") {
		return NormalizeHeaderToEnv(name), true
	}

	return "", false
}

// GetQueryEnv returns the configured or heuristically inferred environment variable name for a query parameter.
func (sc *SecretsConfig) GetQueryEnv(name string) (string, bool) {
	if sc != nil && len(sc.Query) > 0 {
		for k, v := range sc.Query {
			if strings.EqualFold(k, name) {
				return v, true
			}
		}
	}

	lower := strings.ToLower(name)
	switch lower {
	case "key", "api_key", "apikey":
		return "API_KEY", true
	case "access_token", "token", "auth":
		return "ACCESS_TOKEN", true
	case "sig", "signature":
		return "SIGNATURE", true
	}

	return "", false
}

// GetCookieEnv returns the configured or heuristically inferred environment variable name for a cookie.
func (sc *SecretsConfig) GetCookieEnv(name string) (string, bool) {
	if sc != nil && len(sc.Cookies) > 0 {
		for k, v := range sc.Cookies {
			if strings.EqualFold(k, name) {
				return v, true
			}
		}
	}

	lower := strings.ToLower(name)
	if strings.Contains(lower, "session") || strings.Contains(lower, "token") ||
		strings.Contains(lower, "jwt") || strings.Contains(lower, "auth") ||
		strings.Contains(lower, "sid") || strings.Contains(lower, "psid") ||
		strings.Contains(lower, "csrf") {
		return NormalizeHeaderToEnv(name), true
	}

	return "", false
}

// NormalizeHeaderToEnv converts header names into clean uppercase environment variable names.
// e.g. "x-goog-api-key" -> "GOOGLE_API_KEY", "x-slack-bot-token" -> "SLACK_BOT_TOKEN".
func NormalizeHeaderToEnv(headerName string) string {
	clean := strings.ToLower(headerName)
	clean = strings.TrimPrefix(clean, "x-")
	clean = strings.TrimPrefix(clean, "x_")
	clean = strings.ReplaceAll(clean, "-", "_")
	clean = strings.ReplaceAll(clean, ".", "_")

	return strings.ToUpper(clean)
}

// SecretsVault manages local, machine-isolated secrets stored in .vortex/cache/secrets.json.
type SecretsVault struct {
	mu      sync.RWMutex
	Secrets map[string]SecretEntry `json:"secrets"`
}

// NewSecretsVault creates an empty SecretsVault instance.
func NewSecretsVault() *SecretsVault {
	return &SecretsVault{
		Secrets: make(map[string]SecretEntry),
	}
}

// LoadSecrets discovers and loads the secrets vault from startDir traversing upwards.
func LoadSecrets(startDir string) (*SecretsVault, string, error) {
	curr, err := filepath.Abs(startDir)
	if err != nil {
		curr = startDir
	}

	for {
		candidate := filepath.Join(curr, ".vortex", "cache", "secrets.json")
		if data, err := os.ReadFile(candidate); err == nil {
			var v SecretsVault
			if err := json.Unmarshal(data, &v); err != nil {
				return nil, candidate, fmt.Errorf("parsing secrets vault: %w", err)
			}

			if v.Secrets == nil {
				v.Secrets = make(map[string]SecretEntry)
			}

			return &v, candidate, nil
		}

		vortexYml := filepath.Join(curr, ".vortex.yml")
		if _, err := os.Stat(vortexYml); err == nil {
			// Found repo root with .vortex.yml but secrets.json does not exist yet
			vaultPath := filepath.Join(curr, ".vortex", "cache", "secrets.json")
			return NewSecretsVault(), vaultPath, nil
		}

		parent := filepath.Dir(curr)
		if parent == curr || parent == "" {
			break
		}

		curr = parent
	}

	defaultPath := filepath.Join(startDir, ".vortex", "cache", "secrets.json")
	return NewSecretsVault(), defaultPath, nil
}

// Save persists the vault to disk.
func (v *SecretsVault) Save(targetPath string) error {
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	var (
		data []byte
		err  error
	)

	generic.WithRLock(&v.mu, func() {
		data, err = json.MarshalIndent(v, "", "  ")
	})

	if err != nil {
		return err
	}

	return os.WriteFile(targetPath, data, 0o600)
}

// Set saves or updates a secret key-value pair.
func (v *SecretsVault) Set(key, value, origin string) {
	v.SetWithTarget(key, value, origin, "", "", "")
}

// SetWithTarget saves or updates a secret key-value pair with target mapping metadata.
func (v *SecretsVault) SetWithTarget(key, value, origin, header, query, cookie string) {
	generic.WithLock(&v.mu, func() {
		if v.Secrets == nil {
			v.Secrets = make(map[string]SecretEntry)
		}

		target := ""
		switch {
		case header != "":
			target = "header:" + header
		case query != "":
			target = "query:" + query
		case cookie != "":
			target = "cookie:" + cookie
		}

		v.Secrets[key] = SecretEntry{
			Key:       key,
			Value:     value,
			Masked:    maskSecret(value),
			Origin:    origin,
			Header:    header,
			Query:     query,
			Cookie:    cookie,
			Target:    target,
			UpdatedAt: time.Now(),
		}
	})
}

// Get retrieves a secret value by key.
func (v *SecretsVault) Get(key string) (string, bool) {
	var (
		val string
		ok  bool
	)

	generic.WithRLock(&v.mu, func() {
		if v.Secrets == nil {
			return
		}
		entry, exists := v.Secrets[key]
		if exists {
			val = entry.Value
			ok = true
		}
	})

	return val, ok
}

// Delete removes a secret by key.
func (v *SecretsVault) Delete(key string) bool {
	var exists bool

	generic.WithLock(&v.mu, func() {
		if v.Secrets == nil {
			return
		}
		_, exists = v.Secrets[key]
		delete(v.Secrets, key)
	})

	return exists
}

// Clear purges all secrets.
func (v *SecretsVault) Clear() {
	generic.WithLock(&v.mu, func() {
		v.Secrets = make(map[string]SecretEntry)
	})
}

// All returns a copy of all secret entries sorted by key.
func (v *SecretsVault) All() []SecretEntry {
	var res []SecretEntry

	generic.WithRLock(&v.mu, func() {
		res = slices.Collect(maps.Values(v.Secrets))
	})

	slices.SortFunc(res, func(a, b SecretEntry) int {
		return cmp.Compare(a.Key, b.Key)
	})

	return res
}

func maskSecret(val string) string {
	if len(val) <= 8 {
		return "********"
	}

	if strings.HasPrefix(val, "Bearer ") {
		token := strings.TrimPrefix(val, "Bearer ")
		if len(token) > 10 {
			return "Bearer " + token[:4] + "..." + token[len(token)-4:]
		}
		return "Bearer ********"
	}

	return val[:4] + "..." + val[len(val)-4:]
}
