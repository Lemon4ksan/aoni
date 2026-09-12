// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package response orchestrates the inspection, validation, diagnostic dumping,
// and structured unmarshaling of inbound HTTP response payloads.
package response

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"os"
	"slices"

	"github.com/lemon4ksan/foundation/iokit"
	"github.com/lemon4ksan/foundation/net/headkit"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"

	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/internal/pipeline"
	"github.com/lemon4ksan/aoni/internal/requestutil"
	"github.com/lemon4ksan/aoni/telemetry"
)

// Handle processes and decodes an HTTP response stream into a target structure or API error.
func Handle(resp *http.Response, target, client any) error {
	if resp == nil {
		return core.ErrNilResponse
	}

	h := newHandler(resp, target, client)

	if !h.captureResponse() {
		defer pipeline.CloseResponse(resp)
	}

	h.dumpDiagnostics()

	if err := h.validate(); err != nil {
		return err
	}

	if h.isErrorStatus() {
		return h.decodeAPIError()
	}

	if h.shouldDiscardBody() {
		return h.drainAndDiscard()
	}

	return h.decodeSuccess()
}

// Handler encapsulates the lifecycle and decoding pipeline of an HTTP response.
type Handler struct {
	resp             *http.Response
	target           any
	client           any
	cfg              *pipeline.RequestConfig
	decoder          decode.Decoder
	hasCustomDecoder bool
}

func newHandler(resp *http.Response, target, client any) Handler {
	cfg := extractRequestConfig(resp)
	dec, hasCustom := resolveDecoder(resp, cfg)

	return Handler{
		resp:             resp,
		target:           target,
		client:           client,
		cfg:              cfg,
		decoder:          dec,
		hasCustomDecoder: hasCustom,
	}
}

func (h *Handler) captureResponse() bool {
	if h.cfg != nil {
		if targetPtr, ok := h.cfg.Capturer.(**http.Response); ok && targetPtr != nil {
			*targetPtr = h.resp
			return true
		}
	}

	return false
}

func (h *Handler) isErrorStatus() bool {
	return h.resp.StatusCode < http.StatusOK || h.resp.StatusCode >= http.StatusMultipleChoices
}

func (h *Handler) shouldDiscardBody() bool {
	return h.target == nil || h.resp.StatusCode == http.StatusNoContent || isNoResponseTarget(h.target)
}

func (h *Handler) drainAndDiscard() error {
	_, err := iokit.CopyZeroAlloc(io.Discard, h.resp.Body)
	return err
}

func isDirectTarget(target any) bool {
	if target == nil {
		return false
	}

	switch target.(type) {
	case core.DirectConsumer, *[]byte, *string, io.Writer:
		return true
	default:
		return false
	}
}

func (h *Handler) validate() error {
	if h.resp.Body == nil || isDirectTarget(h.target) || decode.IsRawDecoder(h.decoder) {
		return nil
	}

	if h.resp.StatusCode < http.StatusBadRequest {
		contentType := h.resp.Header.Get("Content-Type")
		if contentType == "" || decode.IsStructuredMediaType(contentType) {
			return nil
		}
	}

	peekable := ResolvePeekableReader(h.resp)
	if err := h.checkHTML(peekable); err != nil {
		return err
	}

	return h.checkMIMEType()
}

func (h *Handler) checkMIMEType() error {
	if h.decoder != nil && decode.IsRawDecoder(h.decoder) {
		return nil
	}

	contentType := h.resp.Header.Get("Content-Type")
	if contentType == "" {
		return nil
	}

	mediaType := headkit.BaseMediaType(contentType)
	if bytesconv.EqualFoldASCII(mediaType, "text/html") ||
		bytesconv.EqualFoldASCII(mediaType, "application/xhtml+xml") {
		return fmt.Errorf("%w: expected structured data but got HTML", core.ErrUnexpectedContentType)
	}

	return nil
}

func (h *Handler) checkHTML(buf *bufio.Reader) error {
	if h.decoder != nil && decode.IsRawDecoder(h.decoder) {
		return nil
	}

	peekBytes, err := buf.Peek(128)
	if (err != nil && err != io.EOF) || len(peekBytes) == 0 {
		return nil
	}

	if requestutil.FindFirstNonWhitespaceByte(peekBytes) != '<' {
		return nil
	}

	lowerPeek := bytes.ToLower(peekBytes)
	if !bytes.Contains(lowerPeek, []byte("<html")) && !bytes.Contains(lowerPeek, []byte("<!doctype html")) {
		return nil
	}

	return fmt.Errorf("%w: expected structured data but got HTML", core.ErrUnexpectedContentType)
}

func (h *Handler) decodeAPIError() error {
	bodyBytes, _ := io.ReadAll(io.LimitReader(h.resp.Body, 1024*1024))
	apiErr := &core.APIError{StatusCode: h.resp.StatusCode, Body: bodyBytes}

	if h.cfg != nil && h.cfg.ErrorModel != nil {
		switch m := h.cfg.ErrorModel.(type) {
		case *[]byte:
			*m = slices.Clone(bodyBytes)
			apiErr.Model = h.cfg.ErrorModel
		case *string:
			*m = string(bodyBytes)
			apiErr.Model = h.cfg.ErrorModel
		default:
			if h.decoder != nil && h.decoder.Decode(bytes.NewReader(bodyBytes), h.cfg.ErrorModel) == nil {
				apiErr.Model = h.cfg.ErrorModel
			} else if json.Unmarshal(bodyBytes, h.cfg.ErrorModel) == nil {
				apiErr.Model = h.cfg.ErrorModel
			}
		}
	}

	return apiErr
}

func (h *Handler) decodeSuccess() error {
	if br := h.extractBaseResponse(); br != nil {
		br.SetData(h.target)

		if err := h.decoder.Decode(h.resp.Body, br); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}

			return err
		}

		if !br.IsSuccess() {
			return br.Error()
		}

		return nil
	}

	if h.target == nil {
		return nil
	}

	if consumer, ok := h.target.(core.DirectConsumer); ok {
		return consumer.ReadFromReader(h.resp.Body)
	}

	if w, ok := h.target.(io.Writer); ok {
		_, err := io.Copy(w, h.resp.Body)
		return err
	}

	if !h.hasCustomDecoder {
		switch v := h.target.(type) {
		case *[]byte:
			b, err := io.ReadAll(h.resp.Body)
			if err != nil {
				return err
			}

			*v = b

			return nil

		case *string:
			b, err := io.ReadAll(h.resp.Body)
			if err != nil {
				return err
			}

			*v = string(b)

			return nil
		}
	}

	err := h.decoder.Decode(h.resp.Body, h.target)
	if errors.Is(err, io.EOF) {
		return nil
	}

	return err
}

func (h *Handler) extractBaseResponse() core.BaseResponse {
	if h.cfg != nil {
		switch {
		case h.cfg.DisableBaseResponse:
			return nil
		case h.cfg.BaseResponseOverride != nil:
			return h.cfg.BaseResponseOverride()
		}
	}

	if h.client != nil {
		if p, ok := h.client.(core.BaseResponseProvider); ok && p != nil {
			return p.BaseResponse()
		}
	}

	return nil
}

func (h *Handler) dumpDiagnostics() {
	if h.cfg == nil || !h.cfg.Debug || h.resp.Request == nil {
		return
	}

	reqDump := dumpMultipart(h.resp.Request)
	if len(reqDump) == 0 {
		reqDump, _ = httputil.DumpRequestOut(h.resp.Request, true)
	}

	var respDump []byte
	if telemetry.IsStreamingResponse(h.resp) {
		respDump = []byte(
			h.resp.Proto + " " + h.resp.Status + "\r\nContent-Type: " + h.resp.Header.Get(
				"Content-Type",
			) + "\r\n\r\n[streaming body omitted]",
		)
	} else {
		respDump, _ = httputil.DumpResponse(h.resp, true)
	}

	fmt.Fprintf(
		os.Stderr,
		"--- [aoni Debug: %s %s] ---\nRequest:\n%s\nResponse:\n%s\n-------------------------\n",
		h.resp.Request.Method,
		h.resp.Request.URL.String(),
		bytesconv.B2S(requestutil.RedactHeaders(reqDump)),
		bytesconv.B2S(requestutil.RedactHeaders(respDump)),
	)
}

func isNoResponseTarget(target any) bool {
	switch target.(type) {
	case core.NoResponse, *core.NoResponse, **core.NoResponse:
		return true
	default:
		return false
	}
}

// ResolvePeekableReader returns a peekable reader for the response body.
func ResolvePeekableReader(resp *http.Response) *bufio.Reader {
	if b, ok := resp.Body.(*iokit.BufioReadCloser); ok && b.Reader != nil {
		return b.Reader
	}

	if br, ok := resp.Body.(interface{ BufioReader() *bufio.Reader }); ok {
		if r := br.BufioReader(); r != nil {
			return r
		}
	}

	wrapped := iokit.NewBufioReadCloser(resp.Body, resp.Body)
	resp.Body = wrapped

	return wrapped.Reader
}

func extractRequestConfig(resp *http.Response) *pipeline.RequestConfig {
	if resp != nil && resp.Request != nil {
		return pipeline.GetRequestConfig(resp.Request.Context())
	}

	return nil
}

func dumpMultipart(req *http.Request) []byte {
	contentType := req.Header.Get("Content-Type")
	if !bytesconv.EqualFoldASCII(contentType[:min(len(contentType), 19)], "multipart/form-data") || req.GetBody == nil {
		return nil
	}

	bodyRc, err := req.GetBody()
	if err != nil {
		return nil
	}

	bodyBytes, _ := io.ReadAll(io.LimitReader(bodyRc, 256*1024))
	_ = bodyRc.Close()

	return []byte(
		req.Method + " " + req.URL.RequestURI() + " HTTP/1.1\r\nContent-Type: " + contentType + "\r\n\r\n" +
			requestutil.SummarizeMultipartBody(bodyBytes, contentType),
	)
}

func resolveDecoder(resp *http.Response, cfg *pipeline.RequestConfig) (decode.Decoder, bool) {
	if cfg != nil {
		if cfg.ForceContentType != "" {
			return decode.LookupDecoder(cfg.ForceContentType), true
		}

		if cfg.Decoder != nil {
			if d, ok := cfg.Decoder.(decode.Decoder); ok && d != nil {
				return d, true
			}

			return cfg.Decoder, true
		}

		contentType := resp.Header.Get("Content-Type")
		if contentType != "" {
			if d := cfg.LookupDecoder(contentType); d != nil {
				if dec, ok := d.(decode.Decoder); ok && dec != nil {
					return dec, true
				}

				return decode.DecoderFunc(d.Decode), true
			}
		}

		if cfg.AutoDecode && contentType != "" {
			return decode.LookupDecoder(contentType), false
		}
	}

	if resp != nil {
		contentType := resp.Header.Get("Content-Type")
		if contentType != "" {
			if contentType == "application/json" || contentType == "application/json; charset=utf-8" {
				return decode.JSONDecoder, false
			}

			d := decode.LookupDecoder(contentType)
			if !decode.IsRawDecoder(d) {
				return d, false
			}
		}
	}

	return decode.JSONDecoder, false
}
