// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package stream_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/realtime/stream"
)

func TestDownloadTask_CompleteDownload(t *testing.T) {
	t.Parallel()

	payload := strings.Repeat("AONI RESUMABLE STREAM PAYLOAD CHUNKS ", 1000)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("ETag", `"v1.0"`)

		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}

		_, _ = w.Write([]byte(payload))
	}))
	defer ts.Close()

	destFile := filepath.Join(t.TempDir(), "downloaded.txt")
	checkpointFile := filepath.Join(t.TempDir(), "download.ckpt")

	client := aoni.NewClient(nil)

	var progressCalls atomic.Int64

	task := stream.NewDownload(client, ts.URL).
		ToFile(destFile).
		WithCheckpoint(checkpointFile).
		OnProgress(func(downloaded, total int64, _ float64) {
			progressCalls.Add(1)
			assert.Equal(t, int64(len(payload)), total)
			assert.Greater(t, downloaded, int64(0))
		})

	err := task.Execute(t.Context())
	require.NoError(t, err)

	// Verify file content
	content, err := os.ReadFile(destFile)
	require.NoError(t, err)
	assert.Equal(t, payload, string(content))

	// Verify checkpoint was removed upon completion
	_, err = os.Stat(checkpointFile)
	assert.True(t, os.IsNotExist(err))
	assert.Greater(t, progressCalls.Load(), int64(0))
}

func TestDownloadTask_ResumptionFromCheckpoint(t *testing.T) {
	t.Parallel()

	payload := "PART1_INITIAL_BYTES_PART2_RESUMED_BYTES"
	midpoint := int64(20) // "PART1_INITIAL_BYTES_"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("ETag", `"v1.0"`)

		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			w.WriteHeader(http.StatusOK)

			return
		}

		rangeHeader := r.Header.Get("Range")
		if rangeHeader != "" {
			var start int64

			_, _ = fmt.Sscanf(rangeHeader, "bytes=%d-", &start)

			sub := payload[start:]
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(payload)-1, len(payload)))
			w.Header().Set("Content-Length", strconv.Itoa(len(sub)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte(sub))

			return
		}

		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write([]byte(payload))
	}))
	defer ts.Close()

	destFile := filepath.Join(t.TempDir(), "resumed.txt")
	checkpointFile := filepath.Join(t.TempDir(), "resumed.ckpt")

	// Pre-populate partial file and checkpoint
	require.NoError(t, os.WriteFile(destFile, []byte(payload[:midpoint]), 0o600))

	checkpointJSON := fmt.Sprintf(`{
		"url": "%s",
		"etag": "\"v1.0\"",
		"total_bytes": %d,
		"downloaded_bytes": %d
	}`, ts.URL, len(payload), midpoint)
	require.NoError(t, os.WriteFile(checkpointFile, []byte(checkpointJSON), 0o600))

	client := aoni.NewClient(nil)

	task := stream.NewDownload(client, ts.URL).
		ToFile(destFile).
		WithCheckpoint(checkpointFile)

	err := task.Execute(t.Context())
	require.NoError(t, err)

	content, err := os.ReadFile(destFile)
	require.NoError(t, err)
	assert.Equal(t, payload, string(content))
}

func TestDownloadTask_DestinationRequired(t *testing.T) {
	t.Parallel()

	client := aoni.NewClient(nil)
	task := stream.NewDownload(client, "http://example.com")
	err := task.Execute(t.Context())
	assert.ErrorIs(t, err, stream.ErrDestinationRequired)
}

func TestDownloadTask_ContextCancellation(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000000")
		w.Header().Set("Accept-Ranges", "bytes")

		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)

			return
		}

		// Write initial chunk and flush immediately
		_, _ = w.Write([]byte(strings.Repeat("Z", 1024)))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		// Slow drip
		for range 50 {
			time.Sleep(20 * time.Millisecond)

			_, _ = w.Write([]byte(strings.Repeat("Z", 1024)))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	defer ts.Close()

	destFile := filepath.Join(t.TempDir(), "canceled.txt")
	checkpointFile := filepath.Join(t.TempDir(), "canceled.ckpt")

	client := aoni.NewClient(nil)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	task := stream.NewDownload(client, ts.URL).
		ToFile(destFile).
		WithCheckpoint(checkpointFile).
		OnProgress(func(downloaded, _ int64, _ float64) {
			if downloaded > 0 {
				cancel()
			}
		})

	err := task.Execute(ctx)
	assert.Error(t, err)

	// Checkpoint should exist after interruption
	_, statErr := os.Stat(checkpointFile)
	assert.NoError(t, statErr)
}
