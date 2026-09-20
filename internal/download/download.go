// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package download provides resilient file download operations with exponential backoff retries.
package download

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/lemon4ksan/foundation/iokit"
	"github.com/lemon4ksan/foundation/net/http/contentdisposition"
	"github.com/lemon4ksan/foundation/net/http/header"

	"github.com/lemon4ksan/aoni/internal/core"
)

// ErrDownloadFailed indicates a download request failure due to an HTTP error status code.
var ErrDownloadFailed = errors.New("aoni: download failed")

// Downloader orchestrates resilient file download operations.
type Downloader struct {
	OutputFile      string
	OutputDirectory string
	RetryOverride   *core.RetryOverride
}

// Execute manages multi-attempt resumable file downloads with exponential backoff retries.
func (d Downloader) Execute(
	ctx context.Context,
	client core.HTTPRequester,
	method, path string,
	mods []core.RequestModifier,
) (*http.Response, error) {
	maxAttempts := d.resolveMaxDownloadAttempts()

	var (
		lastResp *http.Response
		lastErr  error
	)

	for attempt := range maxAttempts {
		if attempt > 0 {
			if err := sleepWithContext(ctx, calculateDownloadBackoff(attempt, d.RetryOverride)); err != nil {
				if lastResp != nil && lastResp.Body != nil {
					_ = lastResp.Body.Close()
				}

				return nil, err
			}
		}

		resp, err := client.Request(ctx, method, path, mods...)
		if err != nil {
			lastErr = err
			continue
		}

		if isRetryableDownloadStatus(resp.StatusCode) {
			lastResp = resp
			lastErr = fmt.Errorf("server error: %d", resp.StatusCode)
			_ = resp.Body.Close()

			continue
		}

		if resp.StatusCode >= http.StatusBadRequest {
			return resp, &core.Error{
				Op:   "download",
				Path: path,
				Code: resp.StatusCode,
				Err:  ErrDownloadFailed,
			}
		}

		targetFile := resolveDownloadTarget(resp, path, d.OutputFile, d.OutputDirectory)
		if targetFile != "" {
			if err := saveResponseBodyToFile(resp, targetFile); err != nil {
				lastErr = err
				continue
			}
		}

		return resp, nil
	}

	if lastErr != nil {
		return lastResp, lastErr
	}

	return lastResp, nil
}

func (d Downloader) resolveMaxDownloadAttempts() int {
	if d.RetryOverride != nil && d.RetryOverride.MaxAttempts > 0 {
		return d.RetryOverride.MaxAttempts
	}

	return 5
}

func calculateDownloadBackoff(attempt int, override *core.RetryOverride) time.Duration {
	if attempt <= 0 {
		return 0
	}

	if override != nil && override.Backoff > 0 {
		return override.Backoff * time.Duration(attempt)
	}

	return time.Duration(1<<attempt) * 100 * time.Millisecond
}

func isRetryableDownloadStatus(statusCode int) bool {
	return statusCode >= http.StatusInternalServerError
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func resolveDownloadTarget(resp *http.Response, targetPath, outputFile, outputDirectory string) string {
	if outputFile != "" {
		return outputFile
	}

	if outputDirectory == "" {
		return ""
	}

	var filename string
	if resp != nil && resp.Header != nil {
		if cd := resp.Header.Get(header.ContentDisposition); cd != "" {
			filename = contentdisposition.ExtractFilename(cd)
		}
	}

	if filename == "" {
		p := targetPath
		if u, err := url.Parse(targetPath); err == nil && u.Path != "" {
			p = u.Path
		}

		filename = path.Base(p)
		if filename == "." || filename == "/" || filename == "" {
			filename = "downloaded_file"
		}
	}

	return filepath.Join(outputDirectory, filename)
}

func saveResponseBodyToFile(resp *http.Response, targetFile string) error {
	if targetFile == "" || resp == nil || resp.Body == nil {
		return nil
	}

	defer resp.Body.Close()

	if err := os.MkdirAll(filepath.Dir(targetFile), 0o750); err != nil && !os.IsExist(err) {
		return err
	}

	out, err := os.Create(targetFile)
	if err != nil {
		return err
	}
	defer out.Close()

	_, copyErr := iokit.CopyZeroAlloc(out, resp.Body)

	return copyErr
}
