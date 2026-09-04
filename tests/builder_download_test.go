// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni"
)

func TestRequestBuilder_Download_SetOutputFile(t *testing.T) {
	t.Parallel()

	expectedData := "downloaded-content-bytes-12345"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(expectedData))
	}))
	defer ts.Close()

	tempDir := t.TempDir()
	outFile := filepath.Join(tempDir, "downloads", "my_file.bin")

	client := aoni.New()
	resp, err := client.R().
		SetOutputFile(outFile).
		Execute(http.MethodGet, ts.URL)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	data, err := os.ReadFile(outFile)
	require.NoError(t, err)
	assert.Equal(t, expectedData, string(data))
}

func TestRequestBuilder_Download_SetOutputDirectory_ContentDisposition(t *testing.T) {
	t.Parallel()

	expectedData := "archive-payload"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="custom_report.tar.gz"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(expectedData))
	}))
	defer ts.Close()

	tempDir := t.TempDir()

	client := aoni.New()
	resp, err := client.R().
		SetOutputDirectory(tempDir).
		Execute(http.MethodGet, ts.URL+"/some/random/path")
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	targetFile := filepath.Join(tempDir, "custom_report.tar.gz")
	data, err := os.ReadFile(targetFile)
	require.NoError(t, err)
	assert.Equal(t, expectedData, string(data))
}

func TestRequestBuilder_Download_SetOutputDirectory_PathFallback(t *testing.T) {
	t.Parallel()

	expectedData := "image-payload"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(expectedData))
	}))
	defer ts.Close()

	tempDir := t.TempDir()

	client := aoni.New()
	resp, err := client.R().
		SetOutputDirectory(tempDir).
		Execute(http.MethodGet, ts.URL+"/assets/logo.png")
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	targetFile := filepath.Join(tempDir, "logo.png")
	data, err := os.ReadFile(targetFile)
	require.NoError(t, err)
	assert.Equal(t, expectedData, string(data))
}

func TestRequestBuilder_Download_RetryOn500(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	expectedData := "recovered-after-500"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := attempts.Add(1)
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("error"))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(expectedData))
	}))
	defer ts.Close()

	tempDir := t.TempDir()
	outFile := filepath.Join(tempDir, "retry_out.txt")

	client := aoni.New()
	resp, err := client.R().
		SetOutputFile(outFile).
		Execute(http.MethodGet, ts.URL)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int32(3), attempts.Load())

	data, err := os.ReadFile(outFile)
	require.NoError(t, err)
	assert.Equal(t, expectedData, string(data))
}

func TestRequestBuilder_Download_NonRetryable400(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, "not found")
	}))
	defer ts.Close()

	tempDir := t.TempDir()
	outFile := filepath.Join(tempDir, "not_found.txt")

	client := aoni.New()
	resp, err := client.R().
		SetOutputFile(outFile).
		Execute(http.MethodGet, ts.URL)

	require.Error(t, err)

	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}

	assert.Equal(t, int32(1), attempts.Load())
	assert.ErrorIs(t, err, aoni.ErrDownloadFailed)
}

func TestRequestBuilder_Download_ContextCancelDuringBackoff(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	tempDir := t.TempDir()
	outFile := filepath.Join(tempDir, "canceled.txt")

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	client := aoni.New()
	resp, err := client.R().
		SetContext(ctx).
		SetOutputFile(outFile).
		Execute(http.MethodGet, ts.URL)

	require.Error(t, err)

	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}

	assert.ErrorIs(t, err, context.DeadlineExceeded)
}
