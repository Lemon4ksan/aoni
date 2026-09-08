// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package download_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/internal/download"
)

type mockRequester struct {
	handler http.Handler
}

func (m *mockRequester) Request(
	ctx context.Context,
	method, path string,
	mods ...core.RequestModifier,
) (*http.Response, error) {
	req := httptest.NewRequestWithContext(ctx, method, path, nil)
	rec := httptest.NewRecorder()
	m.handler.ServeHTTP(rec, req)

	return rec.Result(), nil
}

func (m *mockRequester) Do(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	m.handler.ServeHTTP(rec, req)

	return rec.Result(), nil
}

func TestDownloader_Success(t *testing.T) {
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "sample.txt")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("downloaded content successfully"))
	})

	client := &mockRequester{handler: handler}
	d := download.Downloader{OutputFile: outFile}

	resp, err := d.Execute(context.Background(), client, http.MethodGet, "/sample.txt", nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	data, readErr := os.ReadFile(outFile)
	require.NoError(t, readErr)
	assert.Equal(t, "downloaded content successfully", string(data))
}

func TestDownloader_ErrorStatus(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	client := &mockRequester{handler: handler}
	d := download.Downloader{OutputFile: "irrelevant"}

	resp, err := d.Execute(context.Background(), client, http.MethodGet, "/notfound", nil)
	require.Error(t, err)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.ErrorIs(t, err, download.ErrDownloadFailed)
}
