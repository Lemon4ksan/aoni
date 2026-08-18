// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline_test

import (
	"errors"
	stdio "io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/pipeline"
)

func TestStreamPipeline_Execution(t *testing.T) {
	t.Parallel()

	t.Run("empty_pipeline_does_nothing", func(t *testing.T) {
		t.Parallel()

		resp := &http.Response{
			Body: stdio.NopCloser(strings.NewReader("hello")),
		}

		pipe := pipeline.NewStreamPipeline()
		err := pipe.Execute(resp)
		require.NoError(t, err)

		buf, err := stdio.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "hello", string(buf))
	})

	t.Run("chained_filters_transform_stream", func(t *testing.T) {
		t.Parallel()

		filterUpper := func(_ *http.Response, body stdio.ReadCloser) (stdio.ReadCloser, error) {
			buf, err := stdio.ReadAll(body)
			if err != nil {
				return nil, err
			}

			upper := strings.ToUpper(string(buf))

			return stdio.NopCloser(strings.NewReader(upper)), nil
		}

		filterPrefix := func(_ *http.Response, body stdio.ReadCloser) (stdio.ReadCloser, error) {
			buf, err := stdio.ReadAll(body)
			if err != nil {
				return nil, err
			}

			prefixed := "PREFIX_" + string(buf)

			return stdio.NopCloser(strings.NewReader(prefixed)), nil
		}

		pipe := pipeline.NewStreamPipeline(filterUpper, filterPrefix)

		resp := &http.Response{
			Body: stdio.NopCloser(strings.NewReader("data")),
		}

		err := pipe.Execute(resp)
		require.NoError(t, err)

		buf, err := stdio.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "PREFIX_DATA", string(buf))
	})

	t.Run("filter_error_aborts_pipeline", func(t *testing.T) {
		t.Parallel()

		expectedErr := errors.New("filter error")
		failFilter := func(_ *http.Response, _ stdio.ReadCloser) (stdio.ReadCloser, error) {
			return nil, expectedErr
		}

		pipe := pipeline.NewStreamPipeline(failFilter)

		resp := &http.Response{
			Body: stdio.NopCloser(strings.NewReader("data")),
		}

		err := pipe.Execute(resp)
		require.Error(t, err)
		assert.ErrorIs(t, err, expectedErr)
	})
}
