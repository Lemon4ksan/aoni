// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package option_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fingerprint/h2"
	"github.com/lemon4ksan/aoni/netutil/dict"
	"github.com/lemon4ksan/aoni/netutil/dpop"
	"github.com/lemon4ksan/aoni/netutil/httpsig"
	"github.com/lemon4ksan/aoni/option"
)

func TestOption_BaseURL_Normalization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		input        string
		expectedStr  string
		expectedTrim string
	}{
		{
			name:         "adds_trailing_slash",
			input:        "https://api.example.com/v1",
			expectedStr:  "https://api.example.com/v1/",
			expectedTrim: "https://api.example.com/v1",
		},
		{
			name:         "preserves_existing_trailing_slash",
			input:        "https://api.example.com/v1/",
			expectedStr:  "https://api.example.com/v1/",
			expectedTrim: "https://api.example.com/v1",
		},
		{
			name:         "empty_string_resets_base_url",
			input:        "",
			expectedStr:  "",
			expectedTrim: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &aoni.Config{}
			option.WithBaseURL(tt.input)(cfg)

			if tt.input == "" {
				assert.Equal(t, &url.URL{}, cfg.Defaults.BaseURL)
			} else {
				assert.Equal(t, tt.expectedStr, cfg.Defaults.BaseURL.String())
			}
		})
	}
}

func TestOption_Headers_And_Auth(t *testing.T) {
	t.Parallel()

	t.Run("merge_headers_into_uninitialized_and_existing_map", func(t *testing.T) {
		t.Parallel()

		cfg := &aoni.Config{}

		option.WithHeaders(map[string]string{
			"X-App-ID": "test-app",
			"Accept":   "application/json",
		})(cfg)

		assert.Equal(t, "test-app", cfg.Defaults.Headers.Get("X-App-ID"))
		assert.Equal(t, "application/json", cfg.Defaults.Headers.Get("Accept"))

		option.WithHeader("X-Custom", "value")(cfg)
		assert.Equal(t, "value", cfg.Defaults.Headers.Get("X-Custom"))

		option.WithoutHeaders()(cfg)
		assert.Empty(t, cfg.Defaults.Headers.Get("X-App-ID"))
	})

	t.Run("basic_auth_and_bearer_formatting", func(t *testing.T) {
		t.Parallel()

		cfgBearer := &aoni.Config{}
		option.WithBearer("secret-token")(cfgBearer)
		assert.Equal(t, "Bearer secret-token", cfgBearer.Defaults.Headers.Get("Authorization"))

		cfgBasic := &aoni.Config{}
		option.WithBasicAuth("admin", "pass123")(cfgBasic)
		assert.Equal(t, "Basic YWRtaW46cGFzczEyMw==", cfgBasic.Defaults.Headers.Get("Authorization"))

		cfgDigest := &aoni.Config{}
		option.WithDigestAuth("user", "passwd")(cfgDigest)
		require.NotNil(t, cfgDigest.Engine.DigestAuth)
		assert.Equal(t, "user", cfgDigest.Engine.DigestAuth.Username)
		assert.Equal(t, "passwd", cfgDigest.Engine.DigestAuth.Password)
	})

	t.Run("dynamic_header_function", func(t *testing.T) {
		t.Parallel()

		cfg := &aoni.Config{}
		token := "tok-1"

		option.WithHeaderFunc("X-Token", func() string {
			return token
		})(cfg)

		assert.Len(t, cfg.Defaults.DefaultMods, 1)
	})
}

func TestOption_Engine_And_Network(t *testing.T) {
	t.Parallel()

	cfg := &aoni.Config{}

	opts := []aoni.ClientOption{
		option.WithTimeout(15 * time.Second),
		option.WithRedirectLimit(5),
		option.WithInsecureSkipVerify(),
		option.WithSSRFGuard(),
		option.WithProxyDNS(),
		option.WithHappyEyeballs(300 * time.Millisecond),
	}

	for _, opt := range opts {
		opt(cfg)
	}

	assert.Equal(t, 15*time.Second, cfg.Engine.Timeout)
	assert.Equal(t, 5, cfg.Engine.RedirectLimit)
	assert.True(t, cfg.Engine.InsecureSkipVerify)
	assert.True(t, cfg.Network.SSRFGuard)
	assert.True(t, cfg.Network.ProxyDNS)
	assert.Equal(t, 300*time.Millisecond, cfg.Network.HappyEyeballsDelay)
}

func TestOption_Fingerprint_And_Settings(t *testing.T) {
	t.Parallel()

	cfg := &aoni.Config{}

	option.WithTLSFingerprint(aoni.BrowserChrome)(cfg)
	assert.Equal(t, aoni.BrowserChrome, cfg.Fingerprint.BrowserID)

	option.WithSettings(h2.ChromeSettings)(cfg)
	require.NotNil(t, cfg.Fingerprint.H2Settings)
	assert.Equal(t, uint32(65536), cfg.Fingerprint.H2Settings.HeaderTableSize)

	option.WithCertificatePin("example.com", "sha256/pin1")(cfg)
	assert.Contains(t, cfg.Fingerprint.CertificatePins["example.com"], "sha256/pin1")

	option.WithSPKIPin("example.com", "pin2")(cfg)
	assert.Contains(t, cfg.Fingerprint.CertificatePins["example.com"], "pin2")

	option.WithPinnedSPKI(
		"api.example.com",
		`pin-sha256="d6qzRu9zOECb90Uez27xWltNsj0e1Md7GkYYkVoZWmM="`,
		"E9CZ9INDbd+2eRQozYqqbQ2yXLVKB9+xcprMF+44U1g=",
	)(cfg)
	assert.Contains(
		t,
		cfg.Fingerprint.CertificatePins["api.example.com"],
		"d6qzRu9zOECb90Uez27xWltNsj0e1Md7GkYYkVoZWmM=",
	)
	assert.Contains(
		t,
		cfg.Fingerprint.CertificatePins["api.example.com"],
		"E9CZ9INDbd+2eRQozYqqbQ2yXLVKB9+xcprMF+44U1g=",
	)
}

func TestOption_Baremetal_And_BlockOverrides(t *testing.T) {
	t.Parallel()

	t.Run("baremetal_preset_disables_heavy_pipeline_layers", func(t *testing.T) {
		t.Parallel()

		cfg := &aoni.Config{
			Defaults: aoni.ClientDefaults{
				Headers: http.Header{"User-Agent": []string{"test"}},
			},
		}

		option.WithBaremetal()(cfg)

		assert.False(t, cfg.Defaults.Pipeline.Decompress)
		assert.False(t, cfg.Defaults.Pipeline.Validate)
		assert.False(t, cfg.Defaults.Pipeline.Challenge)
		assert.Equal(t, int64(-1), cfg.Defaults.MaxResponseSize)
		assert.Nil(t, cfg.Defaults.Headers)
	})

	t.Run("block_overrides_replace_entire_layer", func(t *testing.T) {
		t.Parallel()

		baseDefaults := aoni.ClientDefaults{
			MaxResponseSize: 1024,
		}

		cfg := &aoni.Config{}
		option.WithDefaultsBlock(baseDefaults)(cfg)

		assert.Equal(t, int64(1024), cfg.Defaults.MaxResponseSize)
	})

	t.Run("experimental_performance_options", func(t *testing.T) {
		t.Parallel()

		cfg := &aoni.Config{}
		option.WithExperimental(option.ExpKernelBypass, option.ExpSIMD, option.ExpTCPFastOpen)(cfg)
		option.WithCPUAffinity(0, 2)(cfg)

		assert.True(t, cfg.Network.HasExperimental(option.ExpKernelBypass))
		assert.True(t, cfg.Network.HasExperimental(option.ExpSIMD))
		assert.True(t, cfg.Network.HasExperimental(option.ExpTCPFastOpen))
		assert.False(t, cfg.Network.HasExperimental(option.ExpZeroCopy))
		assert.Equal(t, []int{0, 2}, cfg.Network.CPUAffinityCores)
	})
}

func TestOption_FromVortexCache_And_Env(t *testing.T) {
	tempDir := t.TempDir()
	vaultDir := filepath.Join(tempDir, ".vortex", "cache")
	require.NoError(t, os.MkdirAll(vaultDir, 0o750))

	vaultJSON := `{
		"secrets": {
			"AUTH_TOKEN": {"key": "AUTH_TOKEN", "value": "ya29.sample_test_token"},
			"API_KEY": {"key": "API_KEY", "value": "test-api-key-value"},
			"CUSTOM_HEADER": {"key": "CUSTOM_HEADER", "header": "X-Custom-Header", "value": "TEST-INSTANCE-123"}
		}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "secrets.json"), []byte(vaultJSON), 0o600))

	// 1. FromVortexCache
	cfg := &aoni.Config{}
	option.FromVortexCache(tempDir)(cfg)

	require.NotNil(t, cfg.Defaults.Headers)
	assert.Equal(t, "Bearer ya29.sample_test_token", cfg.Defaults.Headers.Get("Authorization"))
	assert.Equal(t, "test-api-key-value", cfg.Defaults.Headers.Get("X-Api-Key"))
	assert.Equal(t, "TEST-INSTANCE-123", cfg.Defaults.Headers.Get("X-Custom-Header"))

	// 2. WithEnvBearer & WithEnvHeader
	t.Setenv("TEST_APP_TOKEN", "env_secret_token_abc")
	t.Setenv("TEST_CUSTOM_HEADER", "my-custom-value")

	cfg2 := &aoni.Config{}
	option.WithEnvBearer("TEST_APP_TOKEN")(cfg2)
	option.WithEnvHeader("X-Custom", "TEST_CUSTOM_HEADER")(cfg2)

	assert.Equal(t, "Bearer env_secret_token_abc", cfg2.Defaults.Headers.Get("Authorization"))
	assert.Equal(t, "my-custom-value", cfg2.Defaults.Headers.Get("X-Custom"))
}

func TestOption_WithNetwork(t *testing.T) {
	t.Parallel()

	cfg := &aoni.Config{}
	option.WithNetwork(aoni.NetworkUnix)(cfg)
	assert.Equal(t, aoni.NetworkUnix, cfg.Network.Network)

	option.WithNetworkString("unixgram")(cfg)
	assert.Equal(t, aoni.NetworkUnixGram, cfg.Network.Network)

	assert.True(t, aoni.NetworkTCP.IsTCP())
	assert.True(t, aoni.NetworkTCP4.IsTCP())
	assert.True(t, aoni.NetworkTCP6.IsTCP())
	assert.False(t, aoni.NetworkUDP.IsTCP())

	assert.True(t, aoni.NetworkUDP.IsUDP())
	assert.True(t, aoni.NetworkUDP4.IsUDP())
	assert.True(t, aoni.NetworkUDP6.IsUDP())
	assert.False(t, aoni.NetworkTCP.IsUDP())

	assert.True(t, aoni.NetworkUnix.IsUnix())
	assert.True(t, aoni.NetworkUnixGram.IsUnix())
	assert.True(t, aoni.NetworkUnixPacket.IsUnix())
	assert.False(t, aoni.NetworkTCP.IsUnix())

	assert.True(t, aoni.NetworkIP.IsIP())
	assert.True(t, aoni.NetworkIP4.IsIP())
	assert.True(t, aoni.NetworkIP6.IsIP())
	assert.False(t, aoni.NetworkTCP.IsIP())
}

func TestOption_DictionaryCompression(t *testing.T) {
	t.Parallel()

	customStore := dict.NewStore(dict.WithMaxBytes(32 * 1024 * 1024))
	cfg := &aoni.Config{}

	option.WithDictionaryStore(customStore)(cfg)
	assert.Equal(t, customStore, cfg.Defaults.DictionaryStore)

	option.WithDisableDictionaryCompression(true)(cfg)
	assert.True(t, cfg.Defaults.DisableDictionaryCompression)
}

func TestOption_WithHTTPSignature(t *testing.T) {
	t.Parallel()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	signer, err := httpsig.NewEd25519Signer("key1", priv)
	require.NoError(t, err)

	cfg := &aoni.Config{}
	option.WithHTTPSignature(httpsig.SignConfig{
		Signer: signer,
	})(cfg)

	assert.Equal(t, 1, len(cfg.Defaults.DefaultMods))
}

func TestOption_WithDPoPToken(t *testing.T) {
	t.Parallel()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	cfg := &aoni.Config{}
	option.WithDPoPToken("access-token-123", priv, dpop.ProofOptions{Nonce: "nonce-1"})(cfg)

	assert.Equal(t, 1, len(cfg.Defaults.DefaultMods))
}
