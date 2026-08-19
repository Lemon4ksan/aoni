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

	"github.com/lemon4ksan/foundation/silicon/bytesconv"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/internal/io"
	"github.com/lemon4ksan/aoni/internal/requestutil"
	"github.com/lemon4ksan/aoni/resiliency/challenge"
	"github.com/lemon4ksan/aoni/telemetry"
)

var nullJSONBytes = []byte("null")

func redactHeaders(raw []byte) []byte {
	return requestutil.RedactHeaders(raw)
}

// responseDecoder orchestrates response decoding, diagnostic dumps, and validation.
type responseDecoder struct{}

// ValidateState checks that non-2xx responses or unexpected HTML payloads conform to decoder requirements.
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

	peekableReader := ResolvePeekableReader(resp)

	if err := d.checkHTML(peekableReader); err != nil {
		return err
	}

	return d.checkMIMEType(resp)
}

// isStructuredDataMIME reports whether contentType matches common structured payload MIME types (JSON, Protobuf, gRPC-Web).
func isStructuredDataMIME(contentType string) bool {
	if len(contentType) >= 16 && bytesconv.EqualFoldASCII(contentType[:16], "application/json") {
		return true
	}

	if len(contentType) >= 9 && bytesconv.EqualFoldASCII(contentType[:9], "text/json") {
		return true
	}

	if len(contentType) >= 20 && bytesconv.EqualFoldASCII(contentType[:20], "application/x-protobuf") {
		return true
	}

	if len(contentType) >= 20 && bytesconv.EqualFoldASCII(contentType[:20], "application/protobuf") {
		return true
	}

	if len(contentType) >= 24 && bytesconv.EqualFoldASCII(contentType[:24], "application/grpc-web+proto") {
		return true
	}

	mediaType, _, _ := strings.Cut(contentType, ";")
	mediaType = strings.TrimSpace(mediaType)

	return bytesconv.EqualFoldASCII(mediaType, "application/json") ||
		bytesconv.EqualFoldASCII(mediaType, "text/json") ||
		bytesconv.EqualFoldASCII(mediaType, "application/x-protobuf") ||
		bytesconv.EqualFoldASCII(mediaType, "application/protobuf") ||
		bytesconv.EqualFoldASCII(mediaType, "application/grpc-web+proto") ||
		bytesconv.EqualFoldASCII(mediaType, "application/xml") ||
		bytesconv.EqualFoldASCII(mediaType, "text/xml") ||
		bytesconv.EqualFoldASCII(mediaType, "application/x-yaml") ||
		bytesconv.EqualFoldASCII(mediaType, "application/yaml") ||
		bytesconv.EqualFoldASCII(mediaType, "text/x-yaml") ||
		bytesconv.EqualFoldASCII(mediaType, "text/yaml")
}

// ResolvePeekableReader returns a peekable reader for the response body.
func ResolvePeekableReader(resp *http.Response) *bufio.Reader {
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

// DumpDiagnostics prints HTTP request and response diagnostic payloads to stderr or configured logger when debug mode is enabled.
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

	if logger, ok := requester.(core.LoggerProvider); ok {
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

// SetCapturer inspects if a response capturer pointer was configured, preserving the response object from auto-closure.
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

// DecodeAPIError converts non-2xx HTTP responses into structured [*aoni.APIError] instances.
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

// DecodeSuccess unmarshals successful response payloads into target or configured BaseResponse wrappers.
func (responseDecoder) DecodeSuccess(
	resp *http.Response,
	target any,
	requester Requester,
	decoder decode.Decoder,
) error {
	if br := extractBaseResponse(requester, resp); br != nil {
		br.SetData(target)

		if err := decoder.Decode(resp.Body, br); err != nil {
			if errors.Is(err, stdio.EOF) {
				return nil
			}

			return err
		}

		if !br.IsSuccess() {
			return br.Error()
		}

		return nil
	}

	err := decoder.Decode(resp.Body, target)
	if errors.Is(err, stdio.EOF) {
		return nil
	}

	return err
}

// extractBaseResponse retrieves configured [aoni.BaseResponse] models from request context or client defaults.
func extractBaseResponse(requester Requester, resp *http.Response) aoni.BaseResponse {
	if resp != nil && resp.Request != nil {
		if cfg := aoni.GetRequestConfig(resp.Request.Context()); cfg != nil {
			if cfg.DisableBaseResponse {
				return nil
			}

			if cfg.BaseResponseOverride != nil {
				return cfg.BaseResponseOverride()
			}
		}
	}

	if client, ok := requester.(*aoni.Client); ok {
		return client.BaseResponse()
	}

	if provider, ok := requester.(aoni.BaseResponseProvider); ok {
		return provider.BaseResponse()
	}

	return nil
}

// dumpMultipart generates a summarized diagnostic dump for multipart/form-data requests.
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
			requestutil.SummarizeMultipartBody(bodyBytes, contentType),
	)
}

// checkMIMEType rejects responses whose Content-Type explicitly indicates HTML where structured data was expected.
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

// checkHTML peeks into the response body stream to detect HTML error or Cloudflare challenge pages.
func (responseDecoder) checkHTML(buf *bufio.Reader) error {
	peekBytes, err := buf.Peek(128)
	if (err != nil && err != stdio.EOF) || len(peekBytes) == 0 {
		return nil
	}

	firstNonSpace := findFirstNonWhitespaceByte(peekBytes)
	if firstNonSpace != '<' {
		return nil
	}

	lowerPeek := bytes.ToLower(peekBytes)
	if !bytes.Contains(lowerPeek, []byte("<html")) && !bytes.Contains(lowerPeek, []byte("<!doctype html")) {
		return nil
	}

	if isCloudflareChallengeBytes(lowerPeek) {
		return challenge.ErrCloudflareDetected
	}

	return fmt.Errorf("%w: expected structured data but got HTML", ErrUnexpectedContentType)
}

func findFirstNonWhitespaceByte(b []byte) byte {
	return requestutil.FindFirstNonWhitespaceByte(b)
}

func isCloudflareChallengeBytes(lower []byte) bool {
	return requestutil.IsCloudflareChallengeBytes(lower)
}

// HandleResponse processes and decodes an HTTP response stream into a target structure or API error.
func HandleResponse(resp *http.Response, target any, requester Requester) error {
	if resp == nil {
		return ErrNilResponse
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
		_, _ = io.CopyZeroAlloc(stdio.Discard, resp.Body)
		return nil
	}

	return dec.DecodeSuccess(resp, target, requester, decoder)
}

// resolveDecoder selects the appropriate [decode.Decoder] for the given response Content-Type.
func resolveDecoder(resp *http.Response) decode.Decoder {
	if resp != nil && resp.Request != nil {
		cfg := aoni.GetRequestConfig(resp.Request.Context())
		if cfg != nil {
			if cfg.ForceContentType != "" {
				return decode.LookupDecoder(cfg.ForceContentType)
			}

			if cfg.Decoder != nil {
				if d, ok := cfg.Decoder.(decode.Decoder); ok && d != nil {
					return d
				}

				return cfg.Decoder
			}

			contentType := resp.Header.Get("Content-Type")
			if contentType != "" {
				if d := cfg.LookupDecoder(contentType); d != nil {
					if dec, ok := d.(decode.Decoder); ok && dec != nil {
						return dec
					}

					return decode.DecoderFunc(d.Decode)
				}
			}

			if cfg.AutoDecode && contentType != "" {
				if customDec := decode.GetDecoder(contentType); customDec != nil {
					return customDec
				}

				return decode.LookupDecoder(contentType)
			}
		}
	}

	if resp != nil {
		contentType := resp.Header.Get("Content-Type")
		if contentType != "" {
			if contentType == "application/json" || contentType == "application/json; charset=utf-8" {
				return decode.JSONDecoder
			}

			d := decode.LookupDecoder(contentType)
			if !decode.IsRawDecoder(d) {
				return d
			}

			mediaType, _, _ := strings.Cut(contentType, ";")

			mediaType = strings.TrimSpace(mediaType)
			switch {
			case bytesconv.EqualFoldASCII(mediaType, "application/xml"),
				bytesconv.EqualFoldASCII(mediaType, "text/xml"):
				return decode.XMLDecoder
			case bytesconv.EqualFoldASCII(mediaType, "application/x-protobuf"),
				bytesconv.EqualFoldASCII(mediaType, "application/protobuf"):
				return decode.ProtoDecoder
			case bytesconv.EqualFoldASCII(mediaType, "application/grpc-web+proto"),
				bytesconv.EqualFoldASCII(mediaType, "application/grpc-web"),
				bytesconv.EqualFoldASCII(mediaType, "application/grpc-web-text"):
				return decode.GRPCWebDecoder
			}
		}
	}

	return decode.JSONDecoder
}

// validateAndMarshal validates that payload is not a modifier and encodes it to JSON if required.
func validateAndMarshal(payload any) (stdio.Reader, error) {
	if _, ok := payload.(aoni.RequestModifier); ok {
		return nil, ErrModifierAsBody
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

	if bytes.Equal(bodyBytes, nullJSONBytes) {
		bodyBytes = nil
	}

	return bytes.NewReader(bodyBytes), nil
}
