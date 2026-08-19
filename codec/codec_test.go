// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package codec_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/codec"
)

type TestUser struct {
	Name string `json:"name" xml:"Name" yaml:"name"`
	Age  int    `json:"age"  xml:"Age"  yaml:"age"`
}

func TestCodec_GenericHelpers(t *testing.T) {
	t.Parallel()

	t.Run("JSON", func(t *testing.T) {
		t.Parallel()

		r := strings.NewReader(`{"name":"Alice","age":30}`)
		u, err := codec.JSON[TestUser](r)
		require.NoError(t, err)
		assert.Equal(t, "Alice", u.Name)
		assert.Equal(t, 30, u.Age)
	})

	t.Run("XML", func(t *testing.T) {
		t.Parallel()

		r := strings.NewReader(`<TestUser><Name>Bob</Name><Age>25</Age></TestUser>`)
		u, err := codec.XML[TestUser](r)
		require.NoError(t, err)
		assert.Equal(t, "Bob", u.Name)
		assert.Equal(t, 25, u.Age)
	})

	t.Run("YAML", func(t *testing.T) {
		t.Parallel()

		r := strings.NewReader("name: Charlie\nage: 40\n")
		u, err := codec.YAML[TestUser](r)
		require.NoError(t, err)
		assert.Equal(t, "Charlie", u.Name)
		assert.Equal(t, 40, u.Age)
	})

	t.Run("Raw", func(t *testing.T) {
		t.Parallel()

		r := strings.NewReader("raw byte payload")
		b, err := codec.Raw(r)
		require.NoError(t, err)
		assert.Equal(t, "raw byte payload", string(b))
	})

	t.Run("To and Result", func(t *testing.T) {
		t.Parallel()

		r := strings.NewReader(`{"name":"Dave","age":20}`)
		res := codec.Result[TestUser](r, codec.JSONDecoder)
		require.True(t, res.IsSuccess())
		val, err := res.Unwrap()
		require.NoError(t, err)
		assert.Equal(t, "Dave", val.Name)
	})
}

func TestCodec_Strategies(t *testing.T) {
	t.Parallel()

	assert.NotNil(t, codec.JSONCodec)
	assert.NotNil(t, codec.XMLCodec)
	assert.NotNil(t, codec.YAMLCodec)
	assert.NotNil(t, codec.ProtoCodec)
	assert.NotNil(t, codec.GRPCWebCodec)
}
