// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"bufio"
	"errors"
	stdio "io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	"github.com/lemon4ksan/foundation/text/encoding/htmlindex"
	"github.com/lemon4ksan/foundation/text/transform"

	"github.com/lemon4ksan/aoni/internal/io"
)

var (
	ErrConflictingContentLength  = errors.New("aoni: conflicting Content-Length headers detected")
	ErrConflictingLocationHeader = errors.New("aoni: conflicting Location headers detected in response")
	ErrHeaderInjectionDetected   = errors.New("aoni: CRLF control characters detected in response headers")
)

func (p *Pipeline[Req, Resp]) postProcessResponse(
	stdReq *http.Request,
	resp *http.Response,
	tx *Tx,
) (*http.Response, error) {
	stages := []PostStage[Req, Resp]{
		stageValidateSmuggling[Req, Resp],
		stageDecompressAndTranscode[Req, Resp],
		stageCacheStorage[Req, Resp],
		stageSizeLimit[Req, Resp],
		stageWAFChallenge[Req, Resp],
		stageValidateResponse[Req, Resp],
		stageRefererStateUpdate[Req, Resp],
		stageMultiReadBuffering[Req, Resp],
	}

	var err error
	for _, stage := range stages {
		resp, err = stage(p, stdReq, resp, tx)
		if err != nil {
			return nil, err
		}
	}

	return resp, nil
}

func stageValidateSmuggling[Req, Resp any](
	_ *Pipeline[Req, Resp],
	_ *http.Request,
	resp *http.Response,
	tx *Tx,
) (*http.Response, error) {
	if tx.Flags&FlagValidate == 0 {
		return resp, nil
	}

	if err := validateResponseSmugglingGuards(resp); err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}

		return nil, err
	}

	return resp, nil
}

func stageDecompressAndTranscode[Req, Resp any](
	p *Pipeline[Req, Resp],
	stdReq *http.Request,
	resp *http.Response,
	tx *Tx,
) (*http.Response, error) {
	if tx.Flags&FlagDecompress != 0 {
		return p.handleDecompressionAndTranscoding(stdReq, resp), nil
	}

	if resp != nil && resp.Body != nil {
		resp.Body = applyCharsetTranscoding(resp, resp.Body)
	}

	return resp, nil
}

func stageCacheStorage[Req, Resp any](
	p *Pipeline[Req, Resp],
	stdReq *http.Request,
	resp *http.Response,
	tx *Tx,
) (*http.Response, error) {
	if tx.Flags&FlagCache == 0 || tx.Cache == nil {
		return resp, nil
	}

	if stdReq.Method == http.MethodGet {
		p.saveToCache(stdReq, resp, tx.Cache)
	} else {
		p.invalidateCache(stdReq, resp, tx.Cache)
	}

	return resp, nil
}

func stageSizeLimit[Req, Resp any](
	p *Pipeline[Req, Resp],
	_ *http.Request,
	resp *http.Response,
	tx *Tx,
) (*http.Response, error) {
	if tx.SizeLimit <= 0 {
		return resp, nil
	}

	if limitErr := p.limitResponseSize(resp, tx.SizeLimit); limitErr != nil {
		return nil, limitErr
	}

	return resp, nil
}

func stageWAFChallenge[Req, Resp any](
	p *Pipeline[Req, Resp],
	stdReq *http.Request,
	resp *http.Response,
	tx *Tx,
) (*http.Response, error) {
	if tx.Flags&FlagChallenge == 0 {
		return resp, nil
	}

	return p.handleWAFChallenge(stdReq, resp)
}

func stageValidateResponse[Req, Resp any](
	p *Pipeline[Req, Resp],
	_ *http.Request,
	resp *http.Response,
	tx *Tx,
) (*http.Response, error) {
	if tx.Flags&FlagValidate == 0 {
		return resp, nil
	}

	if valErr := p.validateResponse(resp, tx); valErr != nil {
		return nil, valErr
	}

	return resp, nil
}

func stageRefererStateUpdate[Req, Resp any](
	p *Pipeline[Req, Resp],
	stdReq *http.Request,
	resp *http.Response,
	_ *Tx,
) (*http.Response, error) {
	if p.defaults.RefererAutomaton && p.defaults.RefererState != nil && stdReq != nil && stdReq.URL != nil {
		p.defaults.RefererState.LastURL.Set(stdReq.URL.String())
	}

	return resp, nil
}

func stageMultiReadBuffering[Req, Resp any](
	p *Pipeline[Req, Resp],
	_ *http.Request,
	resp *http.Response,
	tx *Tx,
) (*http.Response, error) {
	if resp == nil || resp.Body == nil {
		return resp, nil
	}

	if bufErr := p.applyMultiReadBuffering(resp, tx); bufErr != nil {
		return nil, bufErr
	}

	return resp, nil
}

// validateResponseSmugglingGuards applies RFC HTTP request/response smuggling and desynchronization protections.
func validateResponseSmugglingGuards(resp *http.Response) error {
	if resp == nil || len(resp.Header) == 0 {
		return nil
	}

	if err := validateContentLengthHeaders(resp); err != nil {
		return err
	}

	if err := validateLocationHeaders(resp); err != nil {
		return err
	}

	if err := validateTransferEncodingAndContentLength(resp); err != nil {
		return err
	}

	return validateHeaderInjections(resp)
}

// validateContentLengthHeaders ensures Content-Length duplicates carry identical values per RFC 9112 §6.3.
func validateContentLengthHeaders(resp *http.Response) error {
	clValues := resp.Header["Content-Length"]
	if len(clValues) <= 1 {
		return nil
	}

	firstVal := strings.TrimSpace(clValues[0])
	for _, val := range clValues[1:] {
		if strings.TrimSpace(val) != firstVal {
			return ErrConflictingContentLength
		}
	}

	resp.Header["Content-Length"] = []string{firstVal}

	return nil
}

// validateLocationHeaders detects and deduplicates multiple conflicting Location headers in redirect responses.
func validateLocationHeaders(resp *http.Response) error {
	locValues := resp.Header["Location"]
	if len(locValues) <= 1 {
		return nil
	}

	firstLoc := strings.TrimSpace(locValues[0])
	for _, loc := range locValues[1:] {
		if strings.TrimSpace(loc) != firstLoc {
			return ErrConflictingLocationHeader
		}
	}

	resp.Header["Location"] = []string{firstLoc}

	return nil
}

// validateTransferEncodingAndContentLength strips Content-Length if Transfer-Encoding is chunked per RFC 9112 §6.3.
func validateTransferEncodingAndContentLength(resp *http.Response) error {
	te := resp.Header.Get("Transfer-Encoding")
	if te == "" || !strings.Contains(strings.ToLower(te), "chunked") {
		return nil
	}

	// RFC 9112 Section 6.3: If Transfer-Encoding is chunked, Content-Length MUST be stripped to prevent desync
	resp.Header.Del("Content-Length")
	resp.ContentLength = -1

	return nil
}

// validateHeaderInjections scans header keys and values for illegal CRLF and null control characters.
func validateHeaderInjections(resp *http.Response) error {
	for k, vv := range resp.Header {
		if containsControlChars(k) {
			return ErrHeaderInjectionDetected
		}

		for i := 0; i < len(vv); i++ {
			if containsControlChars(vv[i]) {
				return ErrHeaderInjectionDetected
			}
		}
	}

	return nil
}

// containsControlChars checks whether s contains CRLF or null bytes.
func containsControlChars(s string) bool {
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b == '\r' || b == '\n' || b == 0 {
			return true
		}
	}

	return false
}

func (p *Pipeline[Req, Resp]) limitResponseSize(resp *http.Response, maxSize int64) error {
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

	cl := resp.ContentLength
	if cl <= 0 {
		if clStr := resp.Header.Get("Content-Length"); clStr != "" {
			if parsed, err := strconv.ParseInt(strings.TrimSpace(clStr), 10, 64); err == nil {
				cl = parsed
				resp.ContentLength = parsed
			}
		}
	}

	if cl > 0 && cl <= maxSize {
		return nil
	}

	if cl > maxSize {
		_ = resp.Body.Close()
		return io.ErrResponseTooLarge
	}

	resp.Body = &io.LimitCheckingReadCloser{
		ReadCloser: resp.Body,
		Limit:      maxSize,
	}

	return nil
}

func (p *Pipeline[Req, Resp]) validateResponse(resp *http.Response, tx *Tx) error {
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

	detectors := tx.SoftErrorDetectors
	if len(detectors) == 0 {
		detectors = p.defaults.SoftErrorDetectors
	}

	if len(detectors) > 0 && resp.Body != nil {
		peekBytes, err := PeekResponseBody(resp, 4096)
		if err != nil && !errors.Is(err, stdio.EOF) {
			_ = resp.Body.Close()
			return err
		}

		for _, detector := range detectors {
			if detector != nil {
				if dErr := detector(resp, peekBytes); dErr != nil {
					_ = resp.Body.Close()
					return dErr
				}
			}
		}
	}

	return nil
}

// PeekResponseBody reads up to n bytes from resp.Body without consuming or draining the stream.
func PeekResponseBody(resp *http.Response, n int) ([]byte, error) {
	if resp == nil || resp.Body == nil || n <= 0 {
		return nil, nil
	}

	if b, ok := resp.Body.(*io.BufioReadCloser); ok {
		return b.Peek(n)
	}

	if br, ok := resp.Body.(interface{ BufioReader() *bufio.Reader }); ok {
		return br.BufioReader().Peek(n)
	}

	peekable := bufio.NewReader(resp.Body)
	resp.Body = &io.BufioReadCloser{
		Reader: peekable,
		Closer: resp.Body,
	}

	return peekable.Peek(n)
}

func (p *Pipeline[Req, Resp]) applyMultiReadBuffering(resp *http.Response, tx *Tx) error {
	threshold := p.defaults.MultiReadThreshold
	disableDisk := p.defaults.MultiReadDisableDisk

	if tx.MultiReadThreshold > 0 {
		threshold = tx.MultiReadThreshold
	} else if tx.MultiReadThreshold < 0 || tx.Flags&FlagMultiRead == 0 {
		threshold = -1
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

func (p *Pipeline[Req, Resp]) handleDecompressionAndTranscoding(req *http.Request, resp *http.Response) *http.Response {
	if resp == nil || resp.Body == nil {
		return resp
	}

	var filters []StreamFilter

	if !hasExplicitAcceptEncoding(req) {
		filters = append(filters, func(r *http.Response, body stdio.ReadCloser) (stdio.ReadCloser, error) {
			decompressedBody, decompressed := applyContentDecompression(r, body)
			if decompressed {
				r.Uncompressed = true
			}

			return decompressedBody, nil
		})
	}

	filters = append(filters, func(r *http.Response, body stdio.ReadCloser) (stdio.ReadCloser, error) {
		return applyCharsetTranscoding(r, body), nil
	})

	if cfg := GetRequestConfig(req.Context()); cfg != nil && cfg.DownloadProgress != nil {
		progress := cfg.DownloadProgress

		filters = append(filters, func(r *http.Response, body stdio.ReadCloser) (stdio.ReadCloser, error) {
			return &io.ProgressReader{
				Reader:     body,
				Total:      r.ContentLength,
				OnProgress: progress,
			}, nil
		})
	}

	_ = ExecuteStreamPipeline(resp, filters)

	return resp
}

func hasExplicitAcceptEncoding(req *http.Request) bool {
	if req == nil || req.Context() == nil {
		return false
	}

	cfg := GetRequestConfig(req.Context())

	return cfg != nil && cfg.HasExplicitAcceptEncoding
}

func applyContentDecompression(resp *http.Response, body stdio.ReadCloser) (stdio.ReadCloser, bool) {
	encoding := resp.Header.Get("Content-Encoding")
	switch encoding {
	case "br":
		resetDecompressedHeader(resp)

		return &io.DecompressReadCloser{
			Reader: brotli.NewReader(body),
			Closer: body,
		}, true

	case "zstd":
		if zstdDec, err := zstd.NewReader(body); err == nil {
			resetDecompressedHeader(resp)

			return &io.DecompressReadCloser{
				Reader: zstdDec,
				Closer: body,
			}, true
		}

	case "gzip":
		if gzReader, err := io.NewPooledGzipReader(body); err == nil {
			resetDecompressedHeader(resp)
			return gzReader, true
		}
	}

	return body, false
}

func resetDecompressedHeader(resp *http.Response) {
	resp.Header.Del("Content-Encoding")
	resp.Header.Del("Content-Length")
	resp.ContentLength = -1
}

func applyCharsetTranscoding(resp *http.Response, body stdio.ReadCloser) stdio.ReadCloser {
	if resp == nil || body == nil {
		return body
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		return body
	}

	lower := strings.ToLower(contentType)
	if !strings.Contains(lower, "charset=") || strings.Contains(lower, "charset=utf-8") {
		return body
	}

	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return body
	}

	charset := strings.ToLower(params["charset"])
	if charset == "" || charset == "utf-8" || charset == "utf8" {
		return body
	}

	enc, err := htmlindex.Get(charset)
	if err != nil {
		return body
	}

	const suffix = "; charset=utf-8"

	totalLen := len(mediaType) + len(suffix)
	if totalLen <= 64 {
		var buf [64]byte

		n := copy(buf[:], mediaType)
		copy(buf[n:], suffix)

		resp.Header.Set("Content-Type", string(buf[:totalLen]))
	} else {
		resp.Header.Set("Content-Type", mediaType+suffix)
	}

	return &io.DecompressReadCloser{
		Reader: transform.NewReader(body, enc.NewDecoder()),
		Closer: body,
	}
}

func (p *Pipeline[Req, Resp]) handleWAFChallenge(req *http.Request, resp *http.Response) (*http.Response, error) {
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
