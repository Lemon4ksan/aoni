// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package option_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fingerprint/h2"
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
		assert.True(t, len(cfgBasic.Defaults.Headers.Get("Authorization")) > 0)
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
