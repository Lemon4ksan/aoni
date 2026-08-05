// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"errors"
	stdio "io"
	"mime"
	"net/http"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/transform"

	"github.com/lemon4ksan/aoni/internal/io"
)

var ErrConflictingContentLength = errors.New("aoni: conflicting Content-Length headers detected")

func (p *Pipeline) postProcessResponse(
	stdReq *http.Request,
	resp *http.Response,
	tx *Tx,
) (*http.Response, error) {
	if err := validateAndNormalizeContentLength(resp); err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}

		return nil, err
	}

	// ⚡ Проверка флагов через 1 такт CPU на битовую маску:
	if tx.Flags&FlagDecompress != 0 {
		resp = p.handleDecompressionAndTranscoding(stdReq, resp)
	}

	if tx.SizeLimit > 0 {
		if limitErr := p.limitResponseSize(resp, tx.SizeLimit); limitErr != nil {
			return nil, limitErr
		}
	}

	if tx.Flags&FlagChallenge != 0 {
		var err error

		resp, err = p.handleWAFChallenge(stdReq, resp)
		if err != nil {
			return nil, err
		}
	}

	if tx.Flags&FlagValidate != 0 {
		if valErr := p.validateResponse(resp, tx); valErr != nil {
			return nil, valErr
		}
	}

	if p.defaults.RefererAutomaton && p.defaults.RefererState != nil && stdReq != nil && stdReq.URL != nil {
		p.defaults.RefererState.Mu.Lock()
		p.defaults.RefererState.LastURL = stdReq.URL.String()
		p.defaults.RefererState.Mu.Unlock()
	}

	if resp != nil && resp.Body != nil {
		if bufErr := p.applyMultiReadBuffering(resp, tx); bufErr != nil {
			return nil, bufErr
		}
	}

	if tx.Flags&FlagCache != 0 && tx.Cache != nil {
		if stdReq.Method == http.MethodGet {
			p.saveToCache(stdReq, resp, tx.Cache)
		} else {
			p.invalidateCache(stdReq, resp, tx.Cache)
		}
	}

	return resp, nil
}

func validateAndNormalizeContentLength(resp *http.Response) error {
	if resp == nil || len(resp.Header["Content-Length"]) <= 1 {
		return nil
	}

	clValues := resp.Header["Content-Length"]
	firstVal := strings.TrimSpace(clValues[0])

	for _, val := range clValues[1:] {
		if strings.TrimSpace(val) != firstVal {
			return ErrConflictingContentLength
		}
	}

	resp.Header["Content-Length"] = []string{firstVal}

	return nil
}

func (p *Pipeline) limitResponseSize(resp *http.Response, maxSize int64) error {
	if resp == nil || resp.Body == nil || maxSize <= 0 {
		return nil
	}

	if resp.ContentLength > 0 && resp.ContentLength <= maxSize {
		return nil
	}

	if resp.ContentLength > maxSize {
		_ = resp.Body.Close()
		return io.ErrResponseTooLarge
	}

	resp.Body = &io.LimitCheckingReadCloser{
		ReadCloser: resp.Body,
		Limit:      maxSize,
	}

	return nil
}

func (p *Pipeline) validateResponse(resp *http.Response, tx *Tx) error {
	if resp == nil {
		return nil
	}

	validator := tx.ResponseValidator
	if validator == nil {
		validator = p.defaults.ResponseValidator
	}

	if validator != nil {
		if err := validator(resp); err != nil {
			if resp.Body != nil {
				_ = resp.Body.Close()
			}

			return err
		}
	}

	return nil
}

func (p *Pipeline) applyMultiReadBuffering(resp *http.Response, tx *Tx) error {
	threshold := p.defaults.MultiReadThreshold
	disableDisk := p.defaults.MultiReadDisableDisk

	if tx.MultiReadThreshold > 0 {
		threshold = tx.MultiReadThreshold
	}

	if tx.MultiReadDisableDisk {
		disableDisk = tx.MultiReadDisableDisk
	}

	if threshold <= 0 || resp == nil || resp.Body == nil || resp.Body == http.NoBody {
		return nil
	}

	mBody, err := io.NewMultiReadBody(resp.Body, threshold, disableDisk)
	if err != nil {
		_ = resp.Body.Close()
		return err
	}

	resp.Body = &io.ResponseBodyReadCloser{ReadCloser: mBody}

	return nil
}

func (p *Pipeline) handleDecompressionAndTranscoding(req *http.Request, resp *http.Response) *http.Response {
	if resp == nil || resp.Body == nil {
		return resp
	}

	if cfg := GetRequestConfig(req.Context()); cfg != nil && cfg.DownloadProgress != nil {
		applyDownloadProgress(resp, cfg.DownloadProgress)
	}

	if !hasExplicitAcceptEncoding(req) {
		if decompressed := applyContentDecompression(resp); decompressed {
			resp.Uncompressed = true
		}
	}

	applyCharsetTranscoding(resp)

	return resp
}

func hasExplicitAcceptEncoding(req *http.Request) bool {
	if req == nil || req.Context() == nil {
		return false
	}

	cfg := GetRequestConfig(req.Context())

	return cfg != nil && cfg.HasExplicitAcceptEncoding
}

func applyDownloadProgress(resp *http.Response, progress io.ProgressFunc) {
	if resp == nil || resp.Body == nil || progress == nil {
		return
	}

	resp.Body = &io.ProgressReader{
		Reader:     resp.Body,
		Total:      resp.ContentLength,
		OnProgress: progress,
	}
}

func applyContentDecompression(resp *http.Response) bool {
	encoding := resp.Header.Get("Content-Encoding")
	switch encoding {
	case "br":
		resp.Body = &io.DecompressReadCloser{
			Reader: brotli.NewReader(resp.Body),
			Closer: resp.Body,
		}
		resetDecompressedHeader(resp)

		return true

	case "zstd":
		if zstdDec, err := zstd.NewReader(resp.Body); err == nil {
			resp.Body = &io.DecompressReadCloser{
				Reader: zstdDec,
				Closer: resp.Body,
			}
			resetDecompressedHeader(resp)

			return true
		}

	case "gzip":
		if gzReader, err := io.NewPooledGzipReader(resp.Body); err == nil {
			resp.Body = gzReader
			resetDecompressedHeader(resp)
			return true
		}
	}

	return false
}

func resetDecompressedHeader(resp *http.Response) {
	resp.Header.Del("Content-Encoding")
	resp.Header.Del("Content-Length")
	resp.ContentLength = -1
}

func applyCharsetTranscoding(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		return
	}

	lower := strings.ToLower(contentType)
	if !strings.Contains(lower, "charset=") || strings.Contains(lower, "charset=utf-8") {
		return
	}

	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return
	}

	charset := strings.ToLower(params["charset"])
	if charset == "" || charset == "utf-8" || charset == "utf8" {
		return
	}

	enc, err := htmlindex.Get(charset)
	if err != nil {
		return
	}

	type transcodeReadCloser struct {
		stdio.Reader
		stdio.Closer
	}

	resp.Body = &transcodeReadCloser{
		Reader: transform.NewReader(resp.Body, enc.NewDecoder()),
		Closer: resp.Body,
	}
}

func (p *Pipeline) handleWAFChallenge(req *http.Request, resp *http.Response) (*http.Response, error) {
	if p.defaults.ChallengeDetector == nil || p.defaults.ChallengeSolver == nil || resp == nil || resp.Body == nil {
		return resp, nil
	}

	bodyBytes, err := stdio.ReadAll(stdio.LimitReader(resp.Body, 100*1024))
	if err != nil {
		return resp, nil //nolint:nilerr
	}

	buffered := &io.ExplicitBufferedBody{
		Prefix: bodyBytes,
		Stream: resp.Body,
	}
	resp.Body = buffered

	isChallenge, challengeErr := p.defaults.ChallengeDetector(resp)
	if !isChallenge {
		buffered.Rewind()
		return resp, nil
	}

	_ = resp.Body.Close()

	solvedResp, solveErr := p.defaults.ChallengeSolver.Solve(req.Context(), challengeErr, req)
	if solveErr != nil {
		return nil, solveErr
	}

	if solvedResp != nil {
		return solvedResp, nil
	}

	return resp, nil
}
