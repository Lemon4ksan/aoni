// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license Image by BSD-style license.

package values

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"
	"google.golang.org/protobuf/types/known/typepb"
)

func TestValueError_FormattingAndUnwrap(t *testing.T) {
	t.Parallel()

	var nilErr *ValueError
	assert.Equal(t, "<nil>", nilErr.Error())

	errWithIndex := &ValueError{Field: "IDs", Index: 2, Err: errors.New("invalid int")}
	assert.Equal(t, "aoni/values: field IDs[2]: invalid int", errWithIndex.Error())

	errWithFieldOnly := &ValueError{Field: "Name", Index: -1, Err: errors.New("empty string")}
	assert.Equal(t, "aoni/values: field Name: empty string", errWithFieldOnly.Error())

	errWithType := &ValueError{Type: "Uint64String", Err: errors.New("parse uint error")}
	assert.Equal(t, "aoni/values: Uint64String: parse uint error", errWithType.Error())

	errFallback := &ValueError{Err: ErrUnsupportedType}
	assert.Equal(t, "aoni/values: aoni/values: unsupported type for encoding", errFallback.Error())

	assert.Equal(t, ErrUnsupportedType, errFallback.Unwrap())
}

func TestNumericStringTypes(t *testing.T) {
	t.Parallel()

	t.Run("Uint64String", func(t *testing.T) {
		t.Parallel()

		var u Uint64String

		// From quoted string
		err := json.Unmarshal([]byte(`"18446744073709551615"`), &u)
		require.NoError(t, err)
		assert.Equal(t, Uint64String(18446744073709551615), u)

		// From number
		err = json.Unmarshal([]byte(`12345`), &u)
		require.NoError(t, err)
		assert.Equal(t, Uint64String(12345), u)

		// From null
		err = json.Unmarshal([]byte(`null`), &u)
		require.NoError(t, err)
		assert.Equal(t, Uint64String(0), u)

		// Marshal JSON
		b, err := u.MarshalJSON()
		require.NoError(t, err)
		assert.Equal(t, `"0"`, string(b))
	})

	t.Run("Int64String", func(t *testing.T) {
		t.Parallel()

		var i Int64String

		err := json.Unmarshal([]byte(`"-9223372036854775808"`), &i)
		require.NoError(t, err)
		assert.Equal(t, Int64String(-9223372036854775808), i)

		b, err := i.MarshalJSON()
		require.NoError(t, err)
		assert.Equal(t, `"-9223372036854775808"`, string(b))
	})

	t.Run("Float64String", func(t *testing.T) {
		t.Parallel()

		var f Float64String

		err := json.Unmarshal([]byte(`"3.14159"`), &f)
		require.NoError(t, err)
		assert.InDelta(t, 3.14159, float64(f), 0.00001)

		b, err := f.MarshalJSON()
		require.NoError(t, err)
		assert.Equal(t, `"3.14159"`, string(b))
	})
}

func TestTimestampTypes(t *testing.T) {
	t.Parallel()

	t.Run("UnixTimestamp", func(t *testing.T) {
		t.Parallel()

		var ts UnixTimestamp

		err := json.Unmarshal([]byte(`"1700000000"`), &ts)
		require.NoError(t, err)
		assert.Equal(t, int64(1700000000), ts.Time().Unix())

		b, err := ts.MarshalJSON()
		require.NoError(t, err)
		assert.Equal(t, "1700000000", string(b))
	})

	t.Run("RFC3339Timestamp", func(t *testing.T) {
		t.Parallel()

		var ts RFC3339Timestamp

		err := json.Unmarshal([]byte(`"2026-08-05T12:00:00Z"`), &ts)
		require.NoError(t, err)
		assert.Equal(t, 2026, ts.Time().Year())
		assert.Equal(t, "2026-08-05T12:00:00Z", ts.String())

		b, err := ts.MarshalJSON()
		require.NoError(t, err)
		assert.Equal(t, `"2026-08-05T12:00:00Z"`, string(b))
	})
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

		msg := &typepb.Option{Name: "proto_query_test"}
		v, err := Encode(msg)
		require.NoError(t, err)
		assert.Equal(t, "proto_query_test", v.Get("name"))
	})

	t.Run("struct_with_nested_proto_field", func(t *testing.T) {
		t.Parallel()

		type RequestWithProto struct {
			Query string         `query:"q"`
			Meta  *typepb.Option `query:"meta"`
		}

		req := RequestWithProto{
			Query: "aoni",
			Meta:  &typepb.Option{Name: "nested_proto_val"},
		}

		v, err := Encode(req)
		require.NoError(t, err)
		assert.Equal(t, "aoni", v.Get("q"))
		assert.JSONEq(t, `{"name":"nested_proto_val"}`, v.Get("meta"))
	})
}

func TestStructToQueryString(t *testing.T) {
	t.Parallel()

	type SearchQuery struct {
		Term  string `query:"q"`
		Page  int    `query:"page,omitempty"`
		Sort  string `query:"sort,omitempty"`
		Group string `query:"group"          default:"public"`
	}

	t.Run("struct_encoding", func(t *testing.T) {
		t.Parallel()

		q := SearchQuery{
			Term: "aoni",
		}

		qStr, err := StructToQueryString(q)
		require.NoError(t, err)

		assert.Contains(t, qStr, "q=aoni")
		assert.Contains(t, qStr, "group=public")
		assert.NotContains(t, qStr, "sort=")
	})

	t.Run("map_direct_encoding", func(t *testing.T) {
		t.Parallel()

		m := map[string]string{
			"key":   "val",
			"query": "hello world",
		}

		qStr, err := StructToQueryString(m)
		require.NoError(t, err)

		assert.Contains(t, qStr, "key=val")
		assert.Contains(t, qStr, "query=hello+world")
	})
}

func TestEncode_TagPriority(t *testing.T) {
	t.Parallel()

	type TagPriorityStruct struct {
		FieldQuery string `json:"j_name_q" query:"q_name" url:"u_name_q"`
		FieldURL   string `json:"j_name_u" url:"u_name"`
		FieldJSON  string `json:"j_name"`
	}

	s := TagPriorityStruct{
		FieldQuery: "val1",
		FieldURL:   "val2",
		FieldJSON:  "val3",
	}

	v, err := Encode(s)
	require.NoError(t, err)
	assert.Equal(t, "val1", v.Get("q_name"))
	assert.Equal(t, "val2", v.Get("u_name"))
	assert.Equal(t, "val3", v.Get("j_name"))
}

func TestEncode_UnsupportedKind(t *testing.T) {
	t.Parallel()

	type InvalidStruct struct {
		Ch chan int `query:"ch"`
	}

	_, err := Encode(InvalidStruct{Ch: make(chan int)})
	assert.Error(t, err)

	var valErr *ValueError
	require.ErrorAs(t, err, &valErr)
	assert.ErrorIs(t, valErr, ErrUnsupportedType)
}
