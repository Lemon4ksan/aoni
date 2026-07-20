// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
)

func TestRawDecoder_Decode_Success(t *testing.T) {
	t.Parallel()

	r := strings.NewReader("raw payload data")

	var output []byte

	err := aoni.RawDecoder.Decode(r, &output)
	require.NoError(t, err)
	assert.Equal(t, "raw payload data", string(output))
}

func TestRawDecoder_Decode_InvalidTargetType(t *testing.T) {
	t.Parallel()

	r := strings.NewReader("some data")

	var output string // not *[]byte

	err := aoni.RawDecoder.Decode(r, &output)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "aoni: RawDecoder requires *[]byte as output type")
}

type errorReader struct{}

func (errorReader) Read(p []byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func TestRawDecoder_Decode_CopyError(t *testing.T) {
	t.Parallel()

	var output []byte

	err := aoni.RawDecoder.Decode(errorReader{}, &output)
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func TestJSONDecoder_Decode(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		r := strings.NewReader(`{"name":"test_user"}`)

		var target struct {
			Name string `json:"name"`
		}

		err := aoni.JSONDecoder.Decode(r, &target)
		require.NoError(t, err)
		assert.Equal(t, "test_user", target.Name)
	})

	t.Run("error", func(t *testing.T) {
		t.Parallel()

		r := strings.NewReader(`{"name":`) // invalid JSON

		var target struct {
			Name string `json:"name"`
		}

		err := aoni.JSONDecoder.Decode(r, &target)
		assert.Error(t, err)
	})
}

func TestXMLDecoder_Decode(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		r := strings.NewReader(`<user><name>test_xml</name></user>`)

		var target struct {
			Name string `xml:"name"`
		}

		err := aoni.XMLDecoder.Decode(r, &target)
		require.NoError(t, err)
		assert.Equal(t, "test_xml", target.Name)
	})

	t.Run("error", func(t *testing.T) {
		t.Parallel()

		r := strings.NewReader(`<user><name>`) // invalid XML

		var target struct {
			Name string `xml:"name"`
		}

		err := aoni.XMLDecoder.Decode(r, &target)
		assert.Error(t, err)
	})
}

func TestYAMLDecoder_Decode(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		r := strings.NewReader("name: test_yaml")

		var target struct {
			Name string `yaml:"name"`
		}

		err := aoni.YAMLDecoder.Decode(r, &target)
		require.NoError(t, err)
		assert.Equal(t, "test_yaml", target.Name)
	})

	t.Run("error", func(t *testing.T) {
		t.Parallel()

		r := strings.NewReader("name: : :") // invalid YAML

		var target struct {
			Name string `yaml:"name"`
		}

		err := aoni.YAMLDecoder.Decode(r, &target)
		assert.Error(t, err)
	})
}

func TestDecoderFunc_Decode(t *testing.T) {
	t.Parallel()

	called := false
	df := aoni.DecoderFunc(func(r io.Reader, target any) error {
		called = true
		return nil
	})

	err := df.Decode(nil, nil)
	require.NoError(t, err)
	assert.True(t, called)
}

func TestDecoderModifiers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		modifier        aoni.RequestModifier
		expectedDecoder aoni.Decoder
	}{
		{"AsRaw", mod.WithRawDecoder(), aoni.RawDecoder},
		{"AsJSON", mod.WithJSONDecoder(), aoni.JSONDecoder},
		{"AsXML", mod.WithXMLDecoder(), aoni.XMLDecoder},
		{"AsYAML", mod.WithYAMLDecoder(), aoni.YAMLDecoder},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost", nil)
			require.NoError(t, err)

			tt.modifier(req)

			cfg := aoni.GetRequestConfig(req.Context())
			require.NotNil(t, cfg)
			d, ok := cfg.Decoder.(aoni.Decoder)
			require.True(t, ok)

			// In Go, function values (like DecoderFunc) are not comparable directly.
			// We compare their function pointers via reflection to verify reference equality.
			if _, isRaw := tt.expectedDecoder.(aoni.RawDecoderT); isRaw {
				assert.IsType(t, aoni.RawDecoderT{}, d)
			} else {
				expectedPtr := reflect.ValueOf(tt.expectedDecoder).Pointer()
				actualPtr := reflect.ValueOf(d).Pointer()
				assert.Equal(t, expectedPtr, actualPtr)
			}
		})
	}
}

func TestDecodeTo_Helpers(t *testing.T) {
	t.Parallel()

	t.Run("decode_to_generic", func(t *testing.T) {
		t.Parallel()

		r := strings.NewReader(`{"id":42}`)

		type item struct {
			ID int `json:"id"`
		}

		val, err := aoni.DecodeTo[item](r, aoni.JSONDecoder)
		require.NoError(t, err)
		assert.Equal(t, 42, val.ID)
	})

	t.Run("decode_to_error", func(t *testing.T) {
		t.Parallel()

		r := strings.NewReader(`{"id":`) // invalid json

		type item struct {
			ID int `json:"id"`
		}

		_, err := aoni.DecodeTo[item](r, aoni.JSONDecoder)
		assert.Error(t, err)
	})

	t.Run("decode_json_convenience", func(t *testing.T) {
		t.Parallel()

		r := strings.NewReader(`{"id":100}`)

		type item struct {
			ID int `json:"id"`
		}

		val, err := aoni.DecodeJSON[item](r)
		require.NoError(t, err)
		assert.Equal(t, 100, val.ID)
	})

	t.Run("decode_xml_convenience", func(t *testing.T) {
		t.Parallel()

		r := strings.NewReader(`<item><id>200</id></item>`)

		type item struct {
			ID int `xml:"id"`
		}

		val, err := aoni.DecodeXML[item](r)
		require.NoError(t, err)
		assert.Equal(t, 200, val.ID)
	})

	t.Run("decode_yaml_convenience", func(t *testing.T) {
		t.Parallel()

		r := strings.NewReader("id: 300")

		type item struct {
			ID int `yaml:"id"`
		}

		val, err := aoni.DecodeYAML[item](r)
		require.NoError(t, err)
		assert.Equal(t, 300, val.ID)
	})
}

func TestNewJSONDecoder_StrictAndUseNumber(t *testing.T) {
	t.Parallel()

	t.Run("disallow_unknown_fields", func(t *testing.T) {
		t.Parallel()

		r := strings.NewReader(`{"id":42,"extra_key":"value"}`)
		d := aoni.NewJSONDecoder(aoni.JSONDecoderConfig{DisallowUnknownFields: true})

		var target struct {
			ID int `json:"id"`
		}

		err := d.Decode(r, &target)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown field")
	})

	t.Run("use_number", func(t *testing.T) {
		t.Parallel()

		r := strings.NewReader(`{"val":12345678901234567890}`)
		d := aoni.NewJSONDecoder(aoni.JSONDecoderConfig{UseNumber: true})

		var target struct {
			Val any `json:"val"`
		}

		err := d.Decode(r, &target)
		require.NoError(t, err)
		assert.IsType(t, json.Number(""), target.Val)
		assert.Equal(t, "12345678901234567890", target.Val.(json.Number).String())
	})
}

func TestDecodeByContentType(t *testing.T) {
	t.Parallel()

	type testUser struct {
		Name string `json:"name" xml:"name" yaml:"name"`
	}

	tests := []struct {
		name        string
		contentType string
		input       string
		wantName    string
	}{
		{"json_with_charset", "application/json; charset=utf-8", `{"name":"alice"}`, "alice"},
		{"xml_standard", "application/xml", `<user><name>bob</name></user>`, "bob"},
		{"yaml_standard", "text/yaml", "name: charlie", "charlie"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := strings.NewReader(tt.input)

			var target testUser

			err := aoni.DecodeByContentType(r, tt.contentType, &target)
			require.NoError(t, err)
			assert.Equal(t, tt.wantName, target.Name)
		})
	}

	t.Run("fallback_to_raw_decoder", func(t *testing.T) {
		t.Parallel()

		r := strings.NewReader("binary_payload")

		var target []byte

		err := aoni.DecodeByContentType(r, "application/octet-stream", &target)
		require.NoError(t, err)
		assert.Equal(t, "binary_payload", string(target))
	})
}

func TestLimitDecoder_ExceedsLimit_ReturnsError(t *testing.T) {
	t.Parallel()

	r := strings.NewReader(`{"name":"extremely_long_name_exceeding_limit"}`)
	d := aoni.LimitDecoder(aoni.JSONDecoder, 15) // Constraints reader to 15 bytes, truncating the stream

	var target struct {
		Name string `json:"name"`
	}

	err := d.Decode(r, &target)
	assert.Error(t, err) // Expect syntax or parsing error due to truncated json stream
}
