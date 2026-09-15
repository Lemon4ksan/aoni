// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lemon4ksan/foundation/codec/compress"
	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/iokit"
	"github.com/lemon4ksan/foundation/net/http/header"
	"github.com/lemon4ksan/foundation/text/encoding/htmlindex"
	"github.com/lemon4ksan/foundation/text/transform"

	"github.com/lemon4ksan/aoni/netutil/dict"
)

var (
	// ErrConflictingContentLength signals multiple conflicting Content-Length headers (RFC 9112 §6.3).
	ErrConflictingContentLength = errors.New("aoni: conflicting Content-Length headers detected")
	// ErrConflictingLocationHeader signals conflicting Location headers in redirect responses.
	ErrConflictingLocationHeader = errors.New("aoni: conflicting Location headers detected in response")
	// ErrHeaderInjectionDetected signals CRLF or null control characters in response headers (RFC 9112 §2.2 & §11.1).
	ErrHeaderInjectionDetected = errors.New("aoni: CRLF control characters detected in response headers")
)

func (h *StdHandler) PostProcess(
	stdReq *http.Request,
	resp *http.Response,
	tx *Tx,
	inErr error,
	startTime time.Time,
) (*http.Response, error) {
	if inErr != nil {
		return nil, h.enrichError(stdReq, inErr, nil, time.Since(startTime))
	}

	var err error

	resp, err = stageValidateSmuggling(h, stdReq, resp, tx)
	if err != nil {
		return nil, err
	}

	resp, err = stageDecompressAndTranscode(h, stdReq, resp, tx)
	if err != nil {
		return nil, err
	}

	resp, err = stageDictionaryCapture(h, stdReq, resp, tx)
	if err != nil {
		return nil, err
	}

	resp, err = stageCacheStorage(h, stdReq, resp, tx)
	if err != nil {
		return nil, err
	}

	resp, err = stageSizeLimit(h, stdReq, resp, tx)
	if err != nil {
		return nil, err
	}

	resp, err = stageValidateResponse(h, stdReq, resp, tx)
	if err != nil {
		return nil, err
	}

	resp, err = stageRefererStateUpdate(h, stdReq, resp, tx)
	if err != nil {
		return nil, err
	}

	resp, err = stageMultiReadBuffering(h, stdReq, resp, tx)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func stageValidateSmuggling(
	h *StdHandler,
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

func stageDecompressAndTranscode(
	h *StdHandler,
	stdReq *http.Request,
	resp *http.Response,
	tx *Tx,
) (*http.Response, error) {
	if tx.Flags&FlagDecompress != 0 {
		return h.handleDecompressionAndTranscoding(stdReq, resp), nil
	}

	if resp != nil && resp.Body != nil {
		resp.Body = applyCharsetTranscoding(resp, resp.Body)
	}

	return resp, nil
}

func stageCacheStorage(
	h *StdHandler,
	stdReq *http.Request,
	resp *http.Response,
	tx *Tx,
) (*http.Response, error) {
	if tx.Flags&FlagCache == 0 || tx.Cache == nil {
		return resp, nil
	}

	if stdReq.Method == http.MethodGet {
		saveToCache(stdReq, resp, tx.Cache)
	} else {
		h.invalidateCache(stdReq, resp, tx.Cache)
	}

	return resp, nil
}

func stageSizeLimit(
	h *StdHandler,
	_ *http.Request,
	resp *http.Response,
	tx *Tx,
) (*http.Response, error) {
	if tx.SizeLimit <= 0 {
		return resp, nil
	}

	if limitErr := h.limitResponseSize(resp, tx.SizeLimit); limitErr != nil {
		return nil, limitErr
	}

	return resp, nil
}

func stageValidateResponse(
	h *StdHandler,
	_ *http.Request,
	resp *http.Response,
	tx *Tx,
) (*http.Response, error) {
	if tx.Flags&FlagValidate == 0 {
		return resp, nil
	}

	if valErr := h.validateResponse(resp, tx); valErr != nil {
		return nil, valErr
	}

	return resp, nil
}

func stageRefererStateUpdate(
	h *StdHandler,
	stdReq *http.Request,
	resp *http.Response,
	_ *Tx,
) (*http.Response, error) {
	if h.defaults.RefererAutomaton && h.defaults.RefererState != nil && stdReq != nil && stdReq.URL != nil {
		h.defaults.RefererState.LastURL.Set(stdReq.URL.String())
	}

	return resp, nil
}

func stageMultiReadBuffering(
	h *StdHandler,
	_ *http.Request,
	resp *http.Response,
	tx *Tx,
) (*http.Response, error) {
	if resp == nil || resp.Body == nil {
		return resp, nil
	}

	if bufErr := h.applyMultiReadBuffering(resp, tx); bufErr != nil {
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

// validateHeaderInjections scans header keys and values for illegal CRLF and null control characters (RFC 9112 §2.2 & §11.1).
func validateHeaderInjections(resp *http.Response) error {
	for k, vv := range resp.Header {
		if containsControlChars(k) {
			return ErrHeaderInjectionDetected
		}

		if slices.ContainsFunc(vv, containsControlChars) {
			return ErrHeaderInjectionDetected
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

func (h *StdHandler) limitResponseSize(resp *http.Response, maxSize int64) error {
	if resp == nil || resp.Body == nil || maxSize <= 0 {
		return nil
	}

	if resp.ContentLength > 0 && resp.ContentLength <= maxSize {
		return nil
	}

	if resp.ContentLength > maxSize {
		_ = resp.Body.Close()
		return iokit.ErrResponseTooLarge
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
		return iokit.ErrResponseTooLarge
	}

	resp.Body = &iokit.LimitCheckingReadCloser{
		ReadCloser: resp.Body,
		Limit:      maxSize,
	}

	return nil
}

func (h *StdHandler) validateResponse(resp *http.Response, tx *Tx) error {
	if resp == nil {
		return nil
	}

	validators := tx.ResponseValidators
	if len(validators) == 0 {
		validators = h.defaults.ResponseValidators
	}

	for _, validator := range validators {
		if validator != nil {
			if err := validator(resp); err != nil {
				if resp.Body != nil {
					_ = resp.Body.Close()
				}

				return err
			}
		}
	}

	detectors := tx.SoftErrorDetectors
	if len(detectors) == 0 {
		detectors = h.defaults.SoftErrorDetectors
	}

	if len(detectors) > 0 && resp.Body != nil {
		peekBytes, err := PeekResponseBody(resp, 4096)
		if err != nil && !errors.Is(err, io.EOF) {
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

	if b, ok := resp.Body.(*iokit.BufioReadCloser); ok {
		return b.Peek(n)
	}

	if br, ok := resp.Body.(interface{ BufioReader() *bufio.Reader }); ok {
		return br.BufioReader().Peek(n)
	}

	peekable := bufio.NewReader(resp.Body)
	resp.Body = &iokit.BufioReadCloser{
		Reader: peekable,
		Closer: resp.Body,
	}

	return peekable.Peek(n)
}

func (h *StdHandler) applyMultiReadBuffering(resp *http.Response, tx *Tx) error {
	threshold := h.defaults.MultiReadThreshold
	disableDisk := h.defaults.MultiReadDisableDisk

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

	mBody, err := iokit.NewMultiReadBody(resp.Body, threshold, disableDisk)
	if err != nil {
		_ = resp.Body.Close()
		return err
	}

	resp.Body = &iokit.ResponseBodyReadCloser{ReadCloser: mBody}

	return nil
}

func (h *StdHandler) handleDecompressionAndTranscoding(req *http.Request, resp *http.Response) *http.Response {
	if resp == nil || resp.Body == nil {
		return resp
	}

	var filters []StreamFilter

	if !hasExplicitAcceptEncoding(req) {
		filters = append(filters, func(r *http.Response, body io.ReadCloser) (io.ReadCloser, error) {
			decompressedBody, decompressed := h.applyContentDecompression(req, r, body)
			if decompressed {
				r.Uncompressed = true
			}

			return decompressedBody, nil
		})
	}

	filters = append(filters, func(r *http.Response, body io.ReadCloser) (io.ReadCloser, error) {
		return applyCharsetTranscoding(r, body), nil
	})

	if cfg := GetRequestConfig(req); cfg != nil && cfg.DownloadProgress != nil {
		progress := cfg.DownloadProgress

		filters = append(filters, func(r *http.Response, body io.ReadCloser) (io.ReadCloser, error) {
			return &iokit.ProgressReader{
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

	cfg := GetRequestConfig(req)

	return cfg != nil && cfg.HasExplicitAcceptEncoding
}

func (h *StdHandler) applyContentDecompression(
	req *http.Request,
	resp *http.Response,
	body io.ReadCloser,
) (io.ReadCloser, bool) {
	encoding := resp.Header.Get("Content-Encoding")
	if encoding == "" || strings.EqualFold(encoding, "identity") {
		return body, false
	}

	normEnc := strings.ToLower(strings.TrimSpace(encoding))
	if normEnc == dict.ContentEncodingDCZ || normEnc == dict.ContentEncodingDCB {
		var dictData []byte
		if req != nil && req.Context() != nil {
			cfg := GetRequestConfig(req)
			if cfg != nil && cfg.AvailableDictionary != nil {
				dictData = cfg.AvailableDictionary.Data
			} else if h.defaults.DictionaryStore != nil && req.URL != nil {
				dest := req.Header.Get("Sec-Fetch-Dest")
				if d, ok := h.defaults.DictionaryStore.Match(req.URL, dest); ok && d != nil {
					dictData = d.Data
				}
			}
		}

		if len(dictData) > 0 {
			reader, err := compress.NewDictionaryReader(normEnc, body, dictData)
			if err == nil {
				resetDecompressedHeader(resp)
				return reader, true
			}
		}
	}

	reader, err := compress.NewReader(encoding, body)
	if err != nil {
		return body, false
	}

	resetDecompressedHeader(resp)

	return reader, true
}

func stageDictionaryCapture(
	h *StdHandler,
	stdReq *http.Request,
	resp *http.Response,
	_ *Tx,
) (*http.Response, error) {
	if resp == nil || resp.StatusCode != http.StatusOK || resp.Body == nil || stdReq == nil || stdReq.URL == nil {
		return resp, nil
	}

	if !strings.EqualFold(stdReq.URL.Scheme, "https") {
		// RFC 9842 §8: secure contexts only
		return resp, nil
	}

	useAsDict := resp.Header.Get(dict.HeaderUseAsDictionary)
	if useAsDict == "" {
		return resp, nil
	}

	cfg := GetRequestConfig(stdReq)
	if cfg != nil && cfg.DisableDictionaryCompression {
		return resp, nil
	}

	if h.defaults.DisableDictionaryCompression {
		return resp, nil
	}

	store := h.defaults.DictionaryStore
	if cfg != nil && cfg.DictionaryStore != nil {
		store = cfg.DictionaryStore
	}

	if store == nil {
		return resp, nil
	}

	// Capture dictionary body up to DefaultMaxDictionarySize
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, dict.DefaultMaxDictionarySize))
	_ = resp.Body.Close()

	if err != nil && !errors.Is(err, io.EOF) {
		resp.Body = io.NopCloser(bytes.NewReader(nil))
		return resp, nil
	}

	resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	if len(bodyBytes) > 0 {
		_, _ = store.Store(stdReq.URL, useAsDict, bodyBytes)
	}

	return resp, nil
}

func resetDecompressedHeader(resp *http.Response) {
	resp.Header.Del(header.ContentEncoding)
	resp.Header.Del(header.ContentLength)
	resp.ContentLength = -1
}

func applyCharsetTranscoding(resp *http.Response, body io.ReadCloser) io.ReadCloser {
	contentType := resp.Header.Get(header.ContentType)
	if contentType == "" {
		return body
	}

	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return body
	}

	charset, ok := params["charset"]
	if !ok || strings.EqualFold(charset, "utf-8") || strings.EqualFold(charset, "us-ascii") {
		return body
	}

	enc, err := htmlindex.Get(charset)
	if err != nil {
		return body
	}

	params["charset"] = "utf-8"

	keys := generic.Keys(params)
	slices.Sort(keys)

	var totalLen int
	for _, k := range keys {
		totalLen += len(k) + len(params[k]) + 3 // 3 for "; ", "="
	}

	if totalLen < 128 {
		var (
			buf [128]byte
			off int
		)

		off += copy(buf[off:], mediaType)
		for _, k := range keys {
			off += copy(buf[off:], "; ")
			off += copy(buf[off:], k)
			off += copy(buf[off:], "=")
			off += copy(buf[off:], params[k])
		}

		resp.Header.Set(header.ContentType, string(buf[:off]))
	} else {
		var b strings.Builder
		b.Grow(len(mediaType) + totalLen)
		b.WriteString(mediaType)

		for _, k := range keys {
			b.WriteString("; ")
			b.WriteString(k)
			b.WriteString("=")
			b.WriteString(params[k])
		}

		resp.Header.Set(header.ContentType, b.String())
	}

	return &iokit.DecompressReadCloser{
		Reader: transform.NewReader(body, enc.NewDecoder()),
		Closer: body,
	}
}
