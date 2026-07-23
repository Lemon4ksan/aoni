// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package request

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	stdio "io"
	"mime"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"
	"sync"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/internal/bytesconv"
	"github.com/lemon4ksan/aoni/internal/io"
	"github.com/lemon4ksan/aoni/resiliency/challenge"
	"github.com/lemon4ksan/aoni/telemetry"
)

var sensitiveHeaderBytes = [][]byte{
	[]byte("authorization"),
	[]byte("cookie"),
	[]byte("set-cookie"),
	[]byte("proxy-authorization"),
}

var bytePool = sync.Pool{
	New: func() any {
		b := make([]byte, 32*1024)
		return &b
	},
}

func redactHeaders(raw []byte) []byte {
	if len(raw) == 0 {
		return raw
	}

	var buf bytes.Buffer
	buf.Grow(len(raw))

	lines := bytes.Split(raw, []byte("\r\n"))
	for i, line := range lines {
		if i > 0 {
			buf.Write([]byte("\r\n"))
		}

		key, _, ok := bytes.Cut(line, []byte{':'})
		if !ok {
			buf.Write(line)
			continue
		}

		trimmedKey := bytes.TrimSpace(key)
		if isSensitiveHeader(trimmedKey) {
			buf.Write(bytes.ToLower(trimmedKey))
			buf.WriteString(": <redacted>")
		} else {
			buf.Write(line)
		}
	}

	return buf.Bytes()
}

func isSensitiveHeader(key []byte) bool {
	keyStr := bytesconv.B2S(key)
	for _, target := range sensitiveHeaderBytes {
		if bytesconv.EqualFoldASCII(keyStr, bytesconv.B2S(target)) {
			return true
		}
	}

	return false
}

type responseDecoder struct{}

func (d responseDecoder) ValidateState(resp *http.Response, decoder decode.Decoder) error {
	if resp == nil || resp.Body == nil || decode.IsRawDecoder(decoder) {
		return nil
	}

	if resp.StatusCode < http.StatusBadRequest {
		contentType := resp.Header.Get("Content-Type")
		if contentType == "" || isStructuredDataMIME(contentType) {
			return nil
		}
	}

	peekableReader := resolvePeekableReader(resp)

	if err := d.checkHTML(peekableReader); err != nil {
		return err
	}

	return d.checkMIMEType(resp)
}

func isStructuredDataMIME(contentType string) bool {
	mediaType, _, _ := strings.Cut(contentType, ";")
	mediaType = strings.TrimSpace(mediaType)

	return bytesconv.EqualFoldASCII(mediaType, "application/json") ||
		bytesconv.EqualFoldASCII(mediaType, "text/json") ||
		bytesconv.EqualFoldASCII(mediaType, "application/x-protobuf") ||
		bytesconv.EqualFoldASCII(mediaType, "application/protobuf") ||
		bytesconv.EqualFoldASCII(mediaType, "application/grpc-web+proto")
}

func resolvePeekableReader(resp *http.Response) *bufio.Reader {
	if b, ok := resp.Body.(*io.BufioReadCloser); ok {
		return b.Reader
	}

	if br, ok := resp.Body.(interface{ BufioReader() *bufio.Reader }); ok {
		return br.BufioReader()
	}

	peekable := bufio.NewReader(resp.Body)
	resp.Body = &io.BufioReadCloser{
		Reader: peekable,
		Closer: resp.Body,
	}

	return peekable
}

func (d responseDecoder) DumpDiagnostics(resp *http.Response, requester Requester) {
	if resp.Request == nil {
		return
	}

	cfg := aoni.GetRequestConfig(resp.Request.Context())
	if cfg == nil || !cfg.Debug {
		return
	}

	reqDump := d.dumpMultipart(resp.Request)
	if len(reqDump) == 0 {
		reqDump, _ = httputil.DumpRequestOut(resp.Request, true)
	}

	var respDump []byte
	if telemetry.IsStreamingResponse(resp) {
		respDump = []byte(
			resp.Proto + " " + resp.Status + "\r\nContent-Type: " + resp.Header.Get(
				"Content-Type",
			) + "\r\n\r\n[Streaming Active - Body Omitted]",
		)
	} else {
		respDump, _ = httputil.DumpResponse(resp, true)
	}

	reqDump = redactHeaders(reqDump)
	respDump = redactHeaders(respDump)

	if logger, ok := requester.(aoni.LoggerProvider); ok {
		logger.Logger().
			Debug("Aoni HTTP Diagnostic", "request", bytesconv.B2S(reqDump), "response", bytesconv.B2S(respDump))
		return
	}

	fmt.Fprintf(
		os.Stderr,
		"\n--- HTTP DEBUG ---\n%s\n\n%s\n------------------\n",
		bytesconv.B2S(reqDump),
		bytesconv.B2S(respDump),
	)
}

func (responseDecoder) SetCapturer(resp *http.Response) bool {
	if resp.Request == nil {
		return false
	}

	cfg := aoni.GetRequestConfig(resp.Request.Context())
	if cfg != nil {
		if targetPtr, ok := cfg.Capturer.(**http.Response); ok && targetPtr != nil {
			*targetPtr = resp
			return true
		}
	}

	return false
}

func (responseDecoder) DecodeAPIError(resp *http.Response) error {
	bodyBytes, _ := stdio.ReadAll(stdio.LimitReader(resp.Body, 1024*1024))
	apiErr := &aoni.APIError{StatusCode: resp.StatusCode, Body: bodyBytes}

	if resp.Request != nil {
		cfg := aoni.GetRequestConfig(resp.Request.Context())
		if cfg != nil && cfg.ErrorModel != nil {
			if err := json.Unmarshal(bodyBytes, cfg.ErrorModel); err == nil {
				apiErr.Model = cfg.ErrorModel
			}
		}
	}

	return apiErr
}

func (responseDecoder) DecodeSuccess(
	resp *http.Response,
	target any,
	requester Requester,
	decoder decode.Decoder,
) error {
	// Fast-path: direct type check to avoid interface assertion boxing allocations
	if client, ok := requester.(*aoni.Client); ok {
		if br := client.BaseResponse(); br != nil {
			br.SetData(target)

			if err := decoder.Decode(resp.Body, br); err != nil {
				return err
			}

			if !br.IsSuccess() {
				return br.Error()
			}

			return nil
		}
	} else if p, ok := requester.(aoni.BaseResponseProvider); ok {
		br := p.BaseResponse()
		if br != nil {
			br.SetData(target)

			if err := decoder.Decode(resp.Body, br); err != nil {
				return err
			}

			if !br.IsSuccess() {
				return br.Error()
			}

			return nil
		}
	}

	err := decoder.Decode(resp.Body, target)
	if errors.Is(err, stdio.EOF) {
		return nil
	}

	return err
}

func (responseDecoder) dumpMultipart(req *http.Request) []byte {
	contentType := req.Header.Get("Content-Type")
	if !bytesconv.EqualFoldASCII(contentType[:min(len(contentType), 19)], "multipart/form-data") || req.GetBody == nil {
		return nil
	}

	bodyRc, err := req.GetBody()
	if err != nil {
		return nil
	}

	bodyBytes, _ := stdio.ReadAll(stdio.LimitReader(bodyRc, 256*1024))
	_ = bodyRc.Close()

	return []byte(
		req.Method + " " + req.URL.RequestURI() + " HTTP/1.1\r\nContent-Type: " + contentType + "\r\n\r\n" +
			telemetry.SummarizeMultipartBody(bodyBytes, contentType),
	)
}

func (responseDecoder) checkMIMEType(resp *http.Response) error {
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		return nil
	}

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil //nolint:nilerr
	}

	if bytesconv.EqualFoldASCII(mediaType, "text/html") ||
		bytesconv.EqualFoldASCII(mediaType, "application/xhtml+xml") {
		return fmt.Errorf("%w: expected structured data but got HTML", ErrUnexpectedContentType)
	}

	return nil
}

func (responseDecoder) checkHTML(buf *bufio.Reader) error {
	peekBytes, err := buf.Peek(128)
	if (err != nil && err != stdio.EOF) || len(peekBytes) == 0 {
		return nil
	}

	firstNonSpace := byte(0)
	for _, b := range peekBytes {
		if b != ' ' && b != '\t' && b != '\r' && b != '\n' {
			firstNonSpace = b
			break
		}
	}

	if firstNonSpace != '<' {
		return nil
	}

	lowerPeek := bytes.ToLower(peekBytes)
	isHTML := bytes.Contains(lowerPeek, []byte("<html")) || bytes.Contains(lowerPeek, []byte("<!doctype html"))

	if !isHTML {
		return nil
	}

	if bytes.Contains(lowerPeek, []byte("cf-challenge")) ||
		bytes.Contains(lowerPeek, []byte("ray id")) ||
		bytes.Contains(lowerPeek, []byte("cloudflare")) {
		return challenge.ErrCloudflareDetected
	}

	return fmt.Errorf("%w: expected structured data but got HTML", ErrUnexpectedContentType)
}

// HandleResponse processes and decodes an HTTP response into target structure or API error.
func HandleResponse(resp *http.Response, target any, requester Requester) error {
	if resp == nil {
		return errors.New("aoni: response is nil")
	}

	dec := responseDecoder{}

	if !dec.SetCapturer(resp) {
		defer aoni.CloseResponse(resp)
	}

	dec.DumpDiagnostics(resp, requester)

	decoder := resolveDecoder(resp)

	if err := dec.ValidateState(resp, decoder); err != nil {
		return err
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return dec.DecodeAPIError(resp)
	}

	if target == nil || resp.StatusCode == http.StatusNoContent {
		bufPtr := bytePool.Get().(*[]byte)
		_, _ = io.CopyZeroAlloc(stdio.Discard, resp.Body)

		bytePool.Put(bufPtr)

		return nil
	}

	return dec.DecodeSuccess(resp, target, requester, decoder)
}

func resolveDecoder(resp *http.Response) decode.Decoder {
	if resp.Request == nil {
		return decode.JSONDecoder
	}

	cfg := aoni.GetRequestConfig(resp.Request.Context())
	if cfg == nil {
		return decode.JSONDecoder
	}

	if cfg.ForceContentType != "" {
		mime := cfg.ForceContentType
		if bytesconv.EqualFoldASCII(mime, "application/xml") || bytesconv.EqualFoldASCII(mime, "text/xml") {
			return decode.XMLDecoder
		}

		return decode.JSONDecoder
	}

	if d, ok := cfg.Decoder.(decode.Decoder); ok {
		return d
	}

	return decode.JSONDecoder
}

func validateAndMarshal(payload any) (stdio.Reader, error) {
	if _, ok := payload.(aoni.RequestModifier); ok {
		return nil, errors.New("aoni: passed a RequestModifier as the request body. Did you forget the body argument?")
	}

	if r, ok := payload.(stdio.Reader); ok {
		return r, nil
	}

	if payload == nil {
		return nil, nil
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("aoni: failed to marshal payload: %w", err)
	}

	if bytes.Equal(bodyBytes, []byte("null")) {
		bodyBytes = nil
	}

	return bytes.NewReader(bodyBytes), nil
}
