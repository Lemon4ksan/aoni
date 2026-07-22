// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license Image by BSD-style license.

package values

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestValueError_FormattingAndUnwrap(t *testing.T) {
	t.Parallel()

	var nilErr *ValueError
	assert.Equal(t, "<nil>", nilErr.Error())

	errWithIndex := &ValueError{Field: "IDs", Index: 2, Err: errors.New("invalid int")}
	assert.Equal(t, "aoni values: field IDs[2]: invalid int", errWithIndex.Error())

	errWithFieldOnly := &ValueError{Field: "Name", Index: -1, Err: errors.New("empty string")}
	assert.Equal(t, "aoni values: field Name: empty string", errWithFieldOnly.Error())

	errWithType := &ValueError{Type: "Uint64String", Err: errors.New("parse uint error")}
	assert.Equal(t, "aoni values: Uint64String: parse uint error", errWithType.Error())

	errFallback := &ValueError{Err: ErrUnsupportedType}
	assert.Equal(t, "aoni values: aoni values: unsupported type for encoding", errFallback.Error())

	assert.Equal(t, ErrUnsupportedType, errFallback.Unwrap())
}

func TestCommaSlice(t *testing.T) {
	t.Parallel()

	t.Run("marshal_text_success", func(t *testing.T) {
		t.Parallel()

		cs := CommaSlice[int]{10, 20, 30}
		bytes, err := cs.MarshalText()
		require.NoError(t, err)
		assert.Equal(t, "10,20,30", string(bytes))
	})

	t.Run("empty_slice", func(t *testing.T) {
		t.Parallel()

		var cs CommaSlice[string]

		bytes, err := cs.MarshalText()
		require.NoError(t, err)
		assert.Nil(t, bytes)
	})
}

func TestProtobufIntegration(t *testing.T) {
	t.Parallel()

	t.Run("top_level_proto_message", func(t *testing.T) {
		t.Parallel()

		msg := wrapperspb.String("proto_query_test")
		v, err := StructToValues(msg)
		require.NoError(t, err)
		assert.Equal(t, "proto_query_test", v.Get("value"))
	})

	t.Run("struct_with_nested_proto_field", func(t *testing.T) {
		t.Parallel()

		type RequestWithProto struct {
			Query string                  `url:"q"`
			Meta  *wrapperspb.StringValue `url:"meta"`
		}

		req := RequestWithProto{
			Query: "aoni",
			Meta:  wrapperspb.String("nested_proto_val"),
		}

		v, err := StructToValues(req)
		require.NoError(t, err)
		assert.Equal(t, "aoni", v.Get("q"))
		assert.JSONEq(t, `{"value":"nested_proto_val"}`, v.Get("meta"))
	})
}

func TestStructToValues_UnsupportedKind(t *testing.T) {
	t.Parallel()

	type InvalidStruct struct {
		Ch chan int `url:"ch"`
	}

	_, err := StructToValues(InvalidStruct{Ch: make(chan int)})
	assert.Error(t, err)

	var valErr *ValueError
	require.ErrorAs(t, err, &valErr)
	assert.ErrorIs(t, valErr, ErrUnsupportedType)
}
