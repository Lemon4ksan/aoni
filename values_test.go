// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license Image by BSD-style license.

package aoni

import (
	"encoding/json"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockID uint64

func (id mockID) String() string { return "id_" + strconv.FormatUint(uint64(id), 10) }

func TestBoolInt(t *testing.T) {
	t.Parallel()

	t.Run("unmarshal", func(t *testing.T) {
		tests := []struct {
			input    string
			expected bool
		}{
			{`1`, true},
			{`0`, false},
			{`"1"`, true},
			{`"0"`, false},
			{`"true"`, true},
			{`"FALSE"`, false},
			{`"2"`, true},
			{`"not-a-bool"`, false},
		}

		for _, tt := range tests {
			var v BoolInt

			err := json.Unmarshal([]byte(tt.input), &v)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, bool(v))
		}
	})

	t.Run("marshal", func(t *testing.T) {
		t.Parallel()

		bTrue, err := json.Marshal(BoolInt(true))
		require.NoError(t, err)
		assert.Equal(t, "1", string(bTrue))

		bFalse, err := json.Marshal(BoolInt(false))
		require.NoError(t, err)
		assert.Equal(t, "0", string(bFalse))
	})
}

func TestUint64String(t *testing.T) {
	t.Parallel()

	t.Run("unmarshal", func(t *testing.T) {
		tests := []struct {
			input    string
			expected uint64
			wantErr  bool
		}{
			{`"123"`, 123, false},
			{`123`, 123, false},
			{`""`, 0, false},
			{`null`, 0, false},
			{`"abc"`, 0, true},
		}

		for _, tt := range tests {
			var v Uint64String

			err := json.Unmarshal([]byte(tt.input), &v)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, uint64(v))
			}
		}
	})

	t.Run("marshal", func(t *testing.T) {
		t.Parallel()

		res, err := json.Marshal(Uint64String(456))
		require.NoError(t, err)
		assert.Equal(t, `"456"`, string(res))
	})
}

func TestInt64String(t *testing.T) {
	t.Parallel()

	t.Run("unmarshal", func(t *testing.T) {
		tests := []struct {
			input    string
			expected int64
			wantErr  bool
		}{
			{`"-123"`, -123, false},
			{`-123`, -123, false},
			{`""`, 0, false},
			{`"abc"`, 0, true},
		}

		for _, tt := range tests {
			var v Int64String

			err := json.Unmarshal([]byte(tt.input), &v)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, int64(v))
			}
		}
	})

	t.Run("marshal", func(t *testing.T) {
		t.Parallel()

		res, err := json.Marshal(Int64String(-456))
		require.NoError(t, err)
		assert.Equal(t, `"-456"`, string(res))
	})
}

func TestFloat64String(t *testing.T) {
	t.Parallel()

	t.Run("unmarshal", func(t *testing.T) {
		tests := []struct {
			input    string
			expected float64
			wantErr  bool
		}{
			{`"10.5"`, 10.5, false},
			{`10.5`, 10.5, false},
			{`""`, 0.0, false},
			{`"invalid"`, 0, true},
		}

		for _, tt := range tests {
			var v Float64String

			err := json.Unmarshal([]byte(tt.input), &v)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, float64(v))
			}
		}
	})

	t.Run("marshal", func(t *testing.T) {
		t.Parallel()

		res, err := json.Marshal(Float64String(1.23))
		require.NoError(t, err)
		assert.Equal(t, `"1.23"`, string(res))
	})
}

func TestUnixTimestamp(t *testing.T) {
	t.Parallel()

	t.Run("unmarshal", func(t *testing.T) {
		tests := []struct {
			input    string
			expected int64
			wantErr  bool
		}{
			{`"1704153600"`, 1704153600, false}, // 2024-01-02
			{`1704153600`, 1704153600, false},
			{`""`, 0, false},
			{`0`, 0, false},
			{`"not-a-date"`, 0, true},
		}

		for _, tt := range tests {
			var v UnixTimestamp

			err := json.Unmarshal([]byte(tt.input), &v)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)

				if tt.expected != 0 {
					assert.Equal(t, tt.expected, v.Time().Unix())
				} else {
					assert.True(t, v.Time().IsZero())
				}
			}
		}
	})

	t.Run("marshal", func(t *testing.T) {
		t.Parallel()

		t0 := UnixTimestamp(time.Unix(1704153600, 0))
		res, err := json.Marshal(t0)
		require.NoError(t, err)
		assert.Equal(t, "1704153600", string(res))

		tZero := UnixTimestamp(time.Time{})
		resZero, err := json.Marshal(tZero)
		require.NoError(t, err)
		assert.Equal(t, "0", string(resZero))
	})
}

func TestStructToValues(t *testing.T) {
	t.Parallel()

	t.Run("nil_input", func(t *testing.T) {
		t.Parallel()

		res, err := StructToValues(nil)
		assert.NoError(t, err)
		assert.Nil(t, res)
	})

	t.Run("pass_through_url_values", func(t *testing.T) {
		t.Parallel()

		input := url.Values{"test": {"1"}}
		res, err := StructToValues(input)
		assert.NoError(t, err)
		assert.Equal(t, "1", res.Get("test"))
	})

	t.Run("basic_types_and_pointers", func(t *testing.T) {
		t.Parallel()

		type Params struct {
			Str   string  `url:"s"`
			Int   int32   `url:"i"`
			Uint  uint64  `url:"u"`
			Bool  bool    `url:"b"`
			Float float64 `url:"f"`
			Skip  string  `url:"-"`
			NoTag string
		}

		p := &Params{
			Str:   "hello",
			Int:   -42,
			Uint:  100,
			Bool:  true,
			Float: 3.14,
			Skip:  "ignore",
			NoTag: "ignore",
		}

		v, err := StructToValues(p)
		require.NoError(t, err)

		assert.Equal(t, "hello", v.Get("s"))
		assert.Equal(t, "-42", v.Get("i"))
		assert.Equal(t, "100", v.Get("u"))
		assert.Equal(t, "true", v.Get("b"))
		assert.Equal(t, "3.14", v.Get("f"))
		assert.Empty(t, v.Get("-"))
		assert.Empty(t, v.Get("NoTag"))
	})

	t.Run("default_tags_handling", func(t *testing.T) {
		t.Parallel()

		type DefaultParams struct {
			Active    string `url:"act"  default:"yes"`
			Port      *int   `url:"port" default:"80"`
			ZeroField int    `url:"zero" default:"99"` // IsZero is true, default is used
		}

		p := DefaultParams{}
		v, err := StructToValues(p)
		require.NoError(t, err)

		assert.Equal(t, "yes", v.Get("act"))
		assert.Equal(t, "80", v.Get("port"))
		assert.Equal(t, "99", v.Get("zero"))
	})

	t.Run("slice_support", func(t *testing.T) {
		t.Parallel()

		type SliceParams struct {
			IDs   []int    `url:"ids"`
			Tags  []string `url:"tags"`
			Empty []string `url:"empty,omitempty"`
		}

		p := SliceParams{
			IDs:   []int{1, 2, 3},
			Tags:  []string{"go", "aoni"},
			Empty: nil,
		}

		v, err := StructToValues(p)
		require.NoError(t, err)

		assert.Equal(t, []string{"1", "2", "3"}, v["ids"])
		assert.Equal(t, []string{"go", "aoni"}, v["tags"])
		assert.False(t, v.Has("empty"))
	})

	t.Run("omitempty_logic", func(t *testing.T) {
		t.Parallel()

		type OmitParams struct {
			Show    string `url:"show,omitempty"`
			Hide    int    `url:"hide,omitempty"`
			Normal  string `url:"normal"`
			ZeroInt int    `url:"zero"`
		}

		p := OmitParams{
			Show:   "present",
			Hide:   0,
			Normal: "",
		}

		v, err := StructToValues(p)
		require.NoError(t, err)

		assert.Equal(t, "present", v.Get("show"))
		assert.False(t, v.Has("hide"))
		assert.True(t, v.Has("normal"))
		assert.Equal(t, "0", v.Get("zero"))
	})

	t.Run("error_not_a_struct", func(t *testing.T) {
		t.Parallel()

		_, err := StructToValues("string is not a struct")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must be a struct")
	})

	t.Run("error_unsupported_field_type", func(t *testing.T) {
		t.Parallel()

		type BadParams struct {
			Map map[string]string `url:"map"`
		}

		_, err := StructToValues(BadParams{Map: map[string]string{"a": "b"}})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported type")
	})

	t.Run("tag_with_only_key", func(t *testing.T) {
		t.Parallel()

		type Simple struct {
			A string `url:"only_key,"`
		}

		v, err := StructToValues(Simple{A: "val"})
		assert.NoError(t, err)
		assert.Equal(t, "val", v.Get("only_key"))
	})
}

func TestStructToValues_Inline(t *testing.T) {
	t.Parallel()

	type baseParams struct {
		DeviceID string `url:"p"`
		SteamID  mockID `url:"a"`
		Mode     string `url:"m"`
	}

	type multiRequest struct {
		baseParams
		ConfIDs []uint64 `url:"cid[]"`
		Extra   struct {
			Internal string `url:"internal"`
		} `url:",inline"`
	}

	req := multiRequest{
		baseParams: baseParams{
			DeviceID: "dev123",
			SteamID:  7656119,
			Mode:     "active",
		},
		ConfIDs: []uint64{10, 20},
	}
	req.Extra.Internal = "secret"

	v, err := StructToValues(req)
	require.NoError(t, err)

	assert.Equal(t, "dev123", v.Get("p"))
	assert.Equal(t, "id_7656119", v.Get("a"))
	assert.Equal(t, "active", v.Get("m"))
	assert.Equal(t, []string{"10", "20"}, v["cid[]"])
	assert.Equal(t, "secret", v.Get("internal"))
}

func TestValidate(t *testing.T) {
	t.Parallel()

	t.Run("valid_struct", func(t *testing.T) {
		t.Parallel()

		type Payload struct {
			Key string `validate:"required"`
		}

		err := Validate(&Payload{Key: "val"})
		assert.NoError(t, err)
	})

	t.Run("missing_required_field", func(t *testing.T) {
		t.Parallel()

		type Payload struct {
			Key string `validate:"required"`
		}

		err := Validate(&Payload{})
		assert.Error(t, err)

		var valErr *ValidationError
		require.ErrorAs(t, err, &valErr)
		assert.Equal(t, "Key", valErr.Field)
		assert.Equal(t, "aoni: missing required field: Key", valErr.Error())
	})

	t.Run("nested_validation_failure", func(t *testing.T) {
		t.Parallel()

		type Child struct {
			Target string `validate:"required"`
		}

		type Parent struct {
			Info Child
		}

		err := Validate(Parent{})
		assert.Error(t, err)

		var valErr *ValidationError
		require.ErrorAs(t, err, &valErr)
		assert.Equal(t, "Info.Target", valErr.Field)
	})

	t.Run("nested_pointer_validation_failure", func(t *testing.T) {
		t.Parallel()

		type Child struct {
			Target string `validate:"required"`
		}

		type Parent struct {
			Info *Child
		}

		child := &Child{}
		err := Validate(Parent{Info: child})
		assert.Error(t, err)

		var valErr *ValidationError
		require.ErrorAs(t, err, &valErr)
		assert.Equal(t, "Info.Target", valErr.Field)
	})

	t.Run("nil_input_or_non_struct", func(t *testing.T) {
		t.Parallel()

		assert.NoError(t, Validate(nil))
		assert.NoError(t, Validate("not a struct"))
	})
}
