// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package decode_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/codec/decode"
)

type ConfigSample struct {
	Name    string   `yaml:"name"`
	Port    int      `yaml:"port"`
	Enabled bool     `yaml:"enabled"`
	Tags    []string `yaml:"tags"`
}

func TestYAMLDecoder(t *testing.T) {
	yamlStr := `name: aoni-service
port: 8080
enabled: true
tags:
  - fast
  - stealth
`

	// 1. Test generic decode.YAML[T]
	cfg, err := decode.YAML[ConfigSample](strings.NewReader(yamlStr))
	require.NoError(t, err)
	require.Equal(t, "aoni-service", cfg.Name)
	require.Equal(t, 8080, cfg.Port)
	require.True(t, cfg.Enabled)
	require.Equal(t, []string{"fast", "stealth"}, cfg.Tags)

	// 2. Test decode.UnmarshalYAML
	var cfg2 ConfigSample

	err = decode.UnmarshalYAML([]byte(yamlStr), &cfg2)
	require.NoError(t, err)
	require.Equal(t, "aoni-service", cfg2.Name)

	// 3. Test LookupDecoder for YAML MIME types
	for _, mime := range []string{"application/x-yaml", "application/yaml", "text/yaml", "text/x-yaml"} {
		d := decode.LookupDecoder(mime)
		require.Equal(t, decode.YAMLDecoder, d)

		var res ConfigSample

		err := decode.ByContentType(strings.NewReader(yamlStr), mime, &res)
		require.NoError(t, err)
		require.Equal(t, "aoni-service", res.Name)
	}

	// 4. Test BOM Stripping with YAML
	bomYAML := append([]byte{0xEF, 0xBB, 0xBF}, []byte(yamlStr)...)
	cfgBOM, err := decode.YAML[ConfigSample](bytes.NewReader(bomYAML))
	require.NoError(t, err)
	require.Equal(t, "aoni-service", cfgBOM.Name)

	// 5. Test Custom YAML Decoder with KnownFields
	customDec := decode.NewYAMLDecoder(decode.YAMLDecoderConfig{KnownFields: true})
	badYAML := "name: test\nunknown_field: true\n"

	var target ConfigSample

	err = customDec.Decode(strings.NewReader(badYAML), &target)
	require.Error(t, err)
}
