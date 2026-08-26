// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package codec_test

import (
	"strings"
	"testing"

	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

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
		res := codec.DecodeToResult[TestUser](r, codec.JSONDecoder)
		require.True(t, res.IsSuccess())
		val, err := res.Unwrap()
		require.NoError(t, err)
		assert.Equal(t, "Dave", val.Name)
	})

	t.Run("Result with generic.FromResult", func(t *testing.T) {
		t.Parallel()

		jsonRes := generic.ToResult(codec.JSON[TestUser](strings.NewReader(`{"name":"Alice","age":30}`)))
		require.True(t, jsonRes.IsSuccess())
		assert.Equal(t, "Alice", jsonRes.MustValue().Name)

		xmlRes := generic.ToResult(
			codec.XML[TestUser](strings.NewReader(`<TestUser><Name>Bob</Name><Age>25</Age></TestUser>`)),
		)
		require.True(t, xmlRes.IsSuccess())
		assert.Equal(t, "Bob", xmlRes.MustValue().Name)

		yamlRes := generic.ToResult(codec.YAML[TestUser](strings.NewReader("name: Charlie\nage: 40\n")))
		require.True(t, yamlRes.IsSuccess())
		assert.Equal(t, "Charlie", yamlRes.MustValue().Name)

		rawRes := generic.ToResult(codec.Raw(strings.NewReader("binary stream")))
		require.True(t, rawRes.IsSuccess())
		assert.Equal(t, []byte("binary stream"), rawRes.MustValue())
	})

	t.Run("Values encoding", func(t *testing.T) {
		t.Parallel()

		type Filter struct {
			Query string `query:"q"`
			Page  int    `query:"page"`
		}

		f := Filter{Query: "test", Page: 2}
		encoded, err := codec.Encode(f)
		require.NoError(t, err)
		assert.Equal(t, "test", encoded.Get("q"))
		assert.Equal(t, "2", encoded.Get("page"))

		qs, err := codec.StructToQueryString(f)
		require.NoError(t, err)
		assert.Equal(t, "q=test&page=2", qs)
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
