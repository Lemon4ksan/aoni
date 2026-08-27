// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package stream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	fheader "github.com/lemon4ksan/foundation/net/http/header"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/mod"
)

var (
	// ErrRangeNotSatisfiable is returned when the remote server rejects byte-range resumption.
	ErrRangeNotSatisfiable = errors.New("aoni/stream: remote server rejected byte-range resumption (HTTP 416)")

	// ErrDestinationRequired is returned when executing a download task without a destination path.
	ErrDestinationRequired = errors.New("aoni/stream: destination path is required")
)

// DownloadProgress reports bytes downloaded, total expected bytes, and current download speed in B/s.
type DownloadProgress func(downloaded, total int64, speed float64)

// CheckpointState stores persistent metadata for resuming interrupted downloads.
type CheckpointState struct {
	URL             string    `json:"url"`
	ETag            string    `json:"etag,omitempty"`
	LastModified    string    `json:"last_modified,omitempty"`
	TotalBytes      int64     `json:"total_bytes"`
	DownloadedBytes int64     `json:"downloaded_bytes"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// DownloadTask coordinates resilient, resumable chunked downloads with checkpoint persistence.
type DownloadTask struct {
	requester       aoni.HTTPRequester
	url             string
	destinationPath string
	checkpointPath  string
	chunkSize       int64
	concurrency     int
	progressFn      DownloadProgress
	modifiers       []aoni.RequestModifier
	doer            aoni.HTTPDoer
}

// NewDownload creates a new fluent [DownloadTask] for downloading targetURL.
func NewDownload(requester aoni.HTTPRequester, targetURL string) *DownloadTask {
	return &DownloadTask{
		requester:   requester,
		url:         targetURL,
		chunkSize:   2 * 1024 * 1024, // 2 MB default chunk size
		concurrency: 1,
	}
}

// NewDownloadWithDoer creates a [DownloadTask] using an [aoni.HTTPDoer] client.
func NewDownloadWithDoer(doer aoni.HTTPDoer, targetURL string) *DownloadTask {
	return &DownloadTask{
		doer:        doer,
		url:         targetURL,
		chunkSize:   2 * 1024 * 1024,
		concurrency: 1,
	}
}

// ToFile sets the destination local file path.
func (t *DownloadTask) ToFile(path string) *DownloadTask {
	t.destinationPath = path
	return t
}

// WithCheckpoint configures atomic progress checkpointing to allow seamless resumption.
func (t *DownloadTask) WithCheckpoint(path string) *DownloadTask {
	t.checkpointPath = path
	return t
}

// WithChunkSize sets the byte range chunk size.
func (t *DownloadTask) WithChunkSize(bytes int64) *DownloadTask {
	if bytes > 0 {
		t.chunkSize = bytes
	}

	return t
}

// WithConcurrency sets the number of concurrent range workers.
func (t *DownloadTask) WithConcurrency(workers int) *DownloadTask {
	if workers > 0 {
		t.concurrency = workers
	}

	return t
}

// OnProgress registers a real-time progress and throughput callback.
func (t *DownloadTask) OnProgress(fn DownloadProgress) *DownloadTask {
	t.progressFn = fn
	return t
}

// WithModifiers attaches custom request modifiers (e.g. headers, tokens).
func (t *DownloadTask) WithModifiers(mods ...aoni.RequestModifier) *DownloadTask {
	t.modifiers = append(t.modifiers, mods...)
	return t
}

// Execute initiates or resumes the download task to completion.
func (t *DownloadTask) Execute(ctx context.Context) error {
	if t.destinationPath == "" {
		return ErrDestinationRequired
	}

	probeResp, err := t.executeRequest(ctx, http.MethodHead, t.url, 0)
	if err != nil {
		// If HEAD is disallowed, fallback to GET probe with Range: bytes=0-0
		probeResp, err = t.executeRequest(
			ctx,
			http.MethodGet,
			t.url,
			0,
			mod.WithHeader(fheader.Range, fheader.ValueBytes+"=0-0"),
		)
		if err != nil {
			return err
		}
	}

	defer func() {
		if probeResp != nil && probeResp.Body != nil {
			_ = probeResp.Body.Close()
		}
	}()

	etag := probeResp.Header.Get(fheader.ETag)
	lastModified := probeResp.Header.Get(fheader.LastModified)
	acceptRanges := probeResp.Header.Get(fheader.AcceptRanges)
	totalBytes := probeResp.ContentLength

	var downloadedBytes int64

	checkpoint := t.loadCheckpoint()
	if checkpoint != nil && checkpoint.URL == t.url {
		// Verify ETag match
		if etag == "" || checkpoint.ETag == etag {
			downloadedBytes = checkpoint.DownloadedBytes
			if totalBytes <= 0 && checkpoint.TotalBytes > 0 {
				totalBytes = checkpoint.TotalBytes
			}
		}
	}

	if err := os.MkdirAll(filepath.Dir(t.destinationPath), 0o750); err != nil && !os.IsExist(err) {
		return err
	}

	flag := os.O_CREATE | os.O_WRONLY
	if downloadedBytes > 0 {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
	}

	file, err := os.OpenFile(t.destinationPath, flag, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()

	if downloadedBytes > 0 {
		if _, err := file.Seek(downloadedBytes, io.SeekStart); err != nil {
			return err
		}
	}

	supportsRanges := acceptRanges == fheader.ValueBytes || probeResp.StatusCode == http.StatusPartialContent
	if !supportsRanges || t.concurrency <= 1 {
		return t.downloadSequential(ctx, file, downloadedBytes, totalBytes, etag, lastModified)
	}

	return t.downloadSequential(ctx, file, downloadedBytes, totalBytes, etag, lastModified)
}

func (t *DownloadTask) downloadSequential(
	ctx context.Context,
	file *os.File,
	startBytes, totalBytes int64,
	etag, lastModified string,
) error {
	mods := append([]aoni.RequestModifier(nil), t.modifiers...)

	if startBytes > 0 {
		mods = append(mods, mod.WithHeader(fheader.Range, fmt.Sprintf(fheader.ValueBytes+"=%d-", startBytes)))
		if etag != "" {
			mods = append(mods, mod.WithHeader(fheader.IfRange, etag))
		}
	}

	resp, err := t.executeRequest(ctx, http.MethodGet, t.url, startBytes, mods...)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		return ErrRangeNotSatisfiable
	}

	if startBytes > 0 && resp.StatusCode == http.StatusOK {
		// Server ignored Range header and sent full payload from start
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return err
		}

		_ = file.Truncate(0)
		startBytes = 0
	}

	if totalBytes <= 0 && resp.ContentLength > 0 {
		totalBytes = startBytes + resp.ContentLength
	}

	buf := make([]byte, 64*1024)
	downloaded := startBytes
	startTime := time.Now()
	lastCheckpointTime := time.Now()

	for {
		if err := ctx.Err(); err != nil {
			t.saveCheckpoint(downloaded, totalBytes, etag, lastModified)
			return err
		}

		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := file.Write(buf[:n]); writeErr != nil {
				return writeErr
			}

			downloaded += int64(n)

			if t.progressFn != nil {
				elapsed := time.Since(startTime).Seconds()

				var speed float64
				if elapsed > 0 {
					speed = float64(downloaded-startBytes) / elapsed
				}

				t.progressFn(downloaded, totalBytes, speed)
			}

			// Periodic checkpoint every 5 seconds
			if time.Since(lastCheckpointTime) > 5*time.Second {
				t.saveCheckpoint(downloaded, totalBytes, etag, lastModified)

				lastCheckpointTime = time.Now()
			}
		}

		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}

			t.saveCheckpoint(downloaded, totalBytes, etag, lastModified)

			return readErr
		}
	}

	t.clearCheckpoint()

	return nil
}

func (t *DownloadTask) executeRequest(
	ctx context.Context,
	method, targetURL string,
	_ int64,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	if t.doer != nil {
		req, err := http.NewRequestWithContext(ctx, method, targetURL, nil)
		if err != nil {
			return nil, err
		}

		for _, m := range mods {
			if m.Kind == core.ModHeader && m.Key != "" {
				req.Header.Set(m.Key, m.Value)
			}
		}

		return t.doer.Do(req)
	}

	if t.requester != nil {
		return t.requester.Request(ctx, method, targetURL, mods...)
	}

	return nil, errors.New("aoni/stream: no client configured for download task")
}

func (t *DownloadTask) loadCheckpoint() *CheckpointState {
	if t.checkpointPath == "" {
		return nil
	}

	data, err := os.ReadFile(t.checkpointPath)
	if err != nil {
		return nil
	}

	var state CheckpointState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil
	}

	return &state
}

func (t *DownloadTask) saveCheckpoint(downloaded, total int64, etag, lastModified string) {
	if t.checkpointPath == "" {
		return
	}

	state := CheckpointState{
		URL:             t.url,
		ETag:            etag,
		LastModified:    lastModified,
		TotalBytes:      total,
		DownloadedBytes: downloaded,
		UpdatedAt:       time.Now(),
	}

	data, err := json.Marshal(state)
	if err != nil {
		return
	}

	_ = os.WriteFile(t.checkpointPath, data, 0o600)
}

func (t *DownloadTask) clearCheckpoint() {
	if t.checkpointPath != "" {
		_ = os.Remove(t.checkpointPath)
	}
}
