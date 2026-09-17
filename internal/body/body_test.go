// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package body_test

import (
	"errors"
	"io"
	"net/url"
	"strings"
	"testing"

	"github.com/lemon4ksan/foundation/testing/assert"
	"github.com/lemon4ksan/foundation/testing/require"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/lemon4ksan/aoni/internal/body"
	"github.com/lemon4ksan/aoni/internal/core"
)

func TestValidateAndMarshal_Nil(t *testing.T) {
	p, err := body.ValidateAndMarshal(nil)
	require.NoError(t, err)
	assert.True(t, p.IsEmpty())
}

func TestValidateAndMarshal_ModifierError(t *testing.T) {
	mod := core.RequestModifier{Kind: core.ModHeader, Key: "X-Test", Value: "1"}
	_, err := body.ValidateAndMarshal(mod)
	require.Error(t, err)
	assert.True(t, errors.Is(err, core.ErrModifierAsBody))
}

func TestValidateAndMarshal_String(t *testing.T) {
	p, err := body.ValidateAndMarshal("hello world")
	require.NoError(t, err)
	assert.False(t, p.IsEmpty())
	assert.False(t, p.HasContentType())

	b, readErr := io.ReadAll(p.Reader)
	require.NoError(t, readErr)
	assert.Equal(t, "hello world", string(b))

	require.NotNil(t, p.GetBody)

	rc, rcErr := p.GetBody()
	require.NoError(t, rcErr)

	rcBytes, _ := io.ReadAll(rc)
	_ = rc.Close()

	assert.Equal(t, "hello world", string(rcBytes))
}

func TestValidateAndMarshal_Bytes(t *testing.T) {
	p, err := body.ValidateAndMarshal([]byte("raw bytes"))
	require.NoError(t, err)
	assert.False(t, p.IsEmpty())
	assert.False(t, p.HasContentType())

	b, readErr := io.ReadAll(p.Reader)
	require.NoError(t, readErr)
	assert.Equal(t, "raw bytes", string(b))

	require.NotNil(t, p.GetBody)

	rc, rcErr := p.GetBody()
	require.NoError(t, rcErr)

	rcBytes, _ := io.ReadAll(rc)
	_ = rc.Close()

	assert.Equal(t, "raw bytes", string(rcBytes))
}

type opaqueReader struct {
	io.Reader
}

func TestValidateAndMarshal_OpaqueReader(t *testing.T) {
	r := opaqueReader{Reader: strings.NewReader("stream data")}
	p, err := body.ValidateAndMarshal(r)
	require.NoError(t, err)
	assert.False(t, p.IsEmpty())
	assert.Nil(t, p.GetBody)

	data, readErr := io.ReadAll(p.Reader)
	require.NoError(t, readErr)
	assert.Equal(t, "stream data", string(data))
}

func TestValidateAndMarshal_StringsReader(t *testing.T) {
	r := strings.NewReader("rewindable stream")
	p, err := body.ValidateAndMarshal(r)
	require.NoError(t, err)
	assert.False(t, p.IsEmpty())
	require.NotNil(t, p.GetBody)

	rc, rcErr := p.GetBody()
	require.NoError(t, rcErr)

	data, _ := io.ReadAll(rc)
	_ = rc.Close()

	assert.Equal(t, "rewindable stream", string(data))
}

func TestValidateAndMarshal_URLValues(t *testing.T) {
	vals := url.Values{"key": []string{"value1", "value2"}}
	p, err := body.ValidateAndMarshal(vals)
	require.NoError(t, err)
	assert.Equal(t, "application/x-www-form-urlencoded", p.ContentType)

	b, readErr := io.ReadAll(p.Reader)
	require.NoError(t, readErr)
	assert.Equal(t, vals.Encode(), string(b))
}

func TestValidateAndMarshal_Proto(t *testing.T) {
	msg := wrapperspb.String("protobuf string")
	p, err := body.ValidateAndMarshal(msg)
	require.NoError(t, err)
	assert.Equal(t, "application/x-protobuf", p.ContentType)
}

func TestValidateAndMarshal_JSON(t *testing.T) {
	type user struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	p, err := body.ValidateAndMarshal(user{Name: "Alice", Age: 30})
	require.NoError(t, err)
	assert.Equal(t, "application/json", p.ContentType)

	b, readErr := io.ReadAll(p.Reader)
	require.NoError(t, readErr)
	assert.Equal(t, `{"name":"Alice","age":30}`, string(b))
}

type customBodyProvider struct {
	content string
}

func (c customBodyProvider) Body() (io.Reader, error) {
	return strings.NewReader(c.content), nil
}

type customContentTyper struct {
	mime string
}

func (c customContentTyper) ContentType() string {
	return c.mime
}

type customCompositePayload struct {
	customBodyProvider
	customContentTyper
}

type customWriterTo struct {
	content string
}

func (w customWriterTo) WriteTo(out io.Writer) (int64, error) {
	n, err := out.Write([]byte(w.content))
	return int64(n), err
}

func TestValidateAndMarshal_CapabilityProtocols(t *testing.T) {
	t.Run("BodyProvider and ContentTyper", func(t *testing.T) {
		payload := customCompositePayload{
			content: "custom xml content",
			mime:    "application/xml",
		}

		p, err := body.ValidateAndMarshal(payload)
		require.NoError(t, err)
		assert.Equal(t, "application/xml", p.ContentType)

		data, readErr := io.ReadAll(p.Reader)
		require.NoError(t, readErr)
		assert.Equal(t, "custom xml content", string(data))
	})

	t.Run("WriterTo", func(t *testing.T) {
		payload := customWriterTo{content: "writer to payload"}
		p, err := body.ValidateAndMarshal(payload)
		require.NoError(t, err)
		assert.False(t, p.IsEmpty())

		data, readErr := io.ReadAll(p.Reader)
		require.NoError(t, readErr)
		assert.Equal(t, "writer to payload", string(data))
	})
}
