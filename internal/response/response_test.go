// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package response_test

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lemon4ksan/foundation/testing/assert"
	"github.com/lemon4ksan/foundation/testing/require"

	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/internal/response"
)

type userDTO struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func TestHandle_NilResponse(t *testing.T) {
	err := response.Handle(nil, nil, nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, core.ErrNilResponse))
}

func TestHandle_SuccessJSON(t *testing.T) {
	httpResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"name":"Bob","age":25}`)),
	}

	var u userDTO

	err := response.Handle(httpResp, &u, nil)
	require.NoError(t, err)
	assert.Equal(t, "Bob", u.Name)
	assert.Equal(t, 25, u.Age)
}

func TestHandle_ErrorStatus(t *testing.T) {
	httpResp := &http.Response{
		StatusCode: http.StatusNotFound,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":"not found"}`)),
	}

	var u userDTO

	err := response.Handle(httpResp, &u, nil)
	require.Error(t, err)

	var apiErr *core.APIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	assert.True(t, apiErr.IsNotFound())
	assert.True(t, errors.Is(err, core.ErrNotFound))
}

func TestHandle_HTMLWhenStructuredExpected(t *testing.T) {
	httpResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><body>Login</body></html>`)),
	}

	var u userDTO

	err := response.Handle(httpResp, &u, nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, core.ErrUnexpectedContentType))
}

func TestHandle_NoResponseTarget(t *testing.T) {
	httpResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"ignored": true}`)),
	}

	var noResp core.NoResponse

	err := response.Handle(httpResp, &noResp, nil)
	require.NoError(t, err)
}

type directSink struct {
	data string
}

func (s *directSink) ReadFromReader(r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	s.data = string(b)

	return nil
}

func TestHandle_RawTargetsBypassHTMLCheck(t *testing.T) {
	htmlContent := `<!doctype html><html><body>Login Page</body></html>`

	t.Run("ByteSliceTarget", func(t *testing.T) {
		httpResp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(htmlContent)),
		}

		var rawBytes []byte

		err := response.Handle(httpResp, &rawBytes, nil)
		require.NoError(t, err)
		assert.Equal(t, htmlContent, string(rawBytes))
	})

	t.Run("StringTarget", func(t *testing.T) {
		httpResp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(htmlContent)),
		}

		var rawStr string

		err := response.Handle(httpResp, &rawStr, nil)
		require.NoError(t, err)
		assert.Equal(t, htmlContent, rawStr)
	})

	t.Run("WriterTarget", func(t *testing.T) {
		httpResp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(htmlContent)),
		}

		var buf strings.Builder

		err := response.Handle(httpResp, &buf, nil)
		require.NoError(t, err)
		assert.Equal(t, htmlContent, buf.String())
	})

	t.Run("DirectConsumerTarget", func(t *testing.T) {
		httpResp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(htmlContent)),
		}

		var sink directSink

		err := response.Handle(httpResp, &sink, nil)
		require.NoError(t, err)
		assert.Equal(t, htmlContent, sink.data)
	})
}
