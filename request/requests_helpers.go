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
	"io"
	"mime"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"
	"sync"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/resiliency/challenge"
)

var sensitiveHeaders = map[string]bool{
	"authorization":       true,
	"cookie":              true,
	"set-cookie":          true,
	"proxy-authorization": true,
}

func redactHeaders(raw []byte) []byte {
	lines := strings.Split(string(raw), "\r\n")
	for i, line := range lines {
		for header := range sensitiveHeaders {
			prefix := header + ":"
			if strings.HasPrefix(strings.ToLower(line), prefix) {
				lines[i] = header + ": <redacted>"
				break
			}
		}
	}

	return []byte(strings.Join(lines, "\r\n"))
}

type responseDecoder struct{}

func (d responseDecoder) validateState(resp *http.Response, decoder decode.Decoder) error {
	peekableReader := bufio.NewReader(resp.Body)

	resp.Body = struct {
		io.Reader
		io.Closer
	}{
		Reader: peekableReader,
		Closer: resp.Body,
	}

	if decode.IsRawDecoder(decoder) {
		return nil
	}

	if err := d.checkHTML(peekableReader); err != nil {
		return err
	}

	return d.checkMIMEType(resp)
}

func (responseDecoder) setCapturer(resp *http.Response) bool {
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

func (responseDecoder) dumpDiagnostics(resp *http.Response, requester aoni.Requester) {
	if resp.Request == nil {
		return
	}

	cfg := aoni.GetRequestConfig(resp.Request.Context())

	if cfg != nil && cfg.Debug {
		reqDump, _ := httputil.DumpRequestOut(resp.Request, true)
		respDump, _ := httputil.DumpResponse(resp, true)

		reqDump = redactHeaders(reqDump)
		respDump = redactHeaders(respDump)

		if logger, ok := requester.(interface{ Logger() aoni.Logger }); ok {
			logger.Logger().Debug("Aoni HTTP Diagnostic", "request", string(reqDump), "response", string(respDump))
		} else {
			fmt.Fprintf(
				os.Stderr,
				"\n--- HTTP DEBUG ---\n%s\n%s\n------------------\n",
				string(reqDump),
				string(respDump),
			)
		}
	}
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

	if mediaType == "text/html" || mediaType == "application/xhtml+xml" {
		return fmt.Errorf("%w: expected structured data but got HTML", aoni.ErrUnexpectedContentType)
	}

	return nil
}

func (responseDecoder) checkHTML(buf *bufio.Reader) error {
	peekBytes, err := buf.Peek(128)

	if err != nil && err != io.EOF {
		return nil
	}

	if len(peekBytes) == 0 {
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

	bodyStr := strings.ToLower(string(peekBytes))
	isHTML := strings.Contains(bodyStr, "<html") || strings.Contains(bodyStr, "<!doctype html")

	if !isHTML {
		return nil
	}

	if strings.Contains(bodyStr, "cf-challenge") || strings.Contains(bodyStr, "ray id") ||
		strings.Contains(bodyStr, "cloudflare") {
		return challenge.ErrCloudflareDetected
	}

	return fmt.Errorf("%w: expected structured data but got HTML", aoni.ErrUnexpectedContentType)
}

func (responseDecoder) decodeAPIError(resp *http.Response) error {
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))

	apiErr := &aoni.APIError{StatusCode: resp.StatusCode, Body: bodyBytes}

	if resp.Request != nil {
		cfg := aoni.GetRequestConfig(resp.Request.Context())

		if cfg == nil || cfg.ErrorModel == nil {
			return apiErr
		}

		if err := json.Unmarshal(bodyBytes, cfg.ErrorModel); err == nil {
			apiErr.Model = cfg.ErrorModel
		}
	}

	return apiErr
}

func (responseDecoder) decodeSuccess(
	resp *http.Response,
	target any,
	requester aoni.Requester,
	decoder decode.Decoder,
) error {
	var br aoni.BaseResponse
	if p, ok := requester.(aoni.BaseResponseProvider); ok {
		br = p.BaseResponse()
	} else if provider, ok := requester.(interface{ Defaults() aoni.ClientDefaults }); ok {
		if brFn := provider.Defaults().BaseResponse; brFn != nil {
			br = brFn()
		}
	}

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

	err := decoder.Decode(resp.Body, target)
	if errors.Is(err, io.EOF) {
		return nil
	}

	return err
}

var bytePool = sync.Pool{
	New: func() any {
		b := make([]byte, 32*1024)
		return &b
	},
}

// HandleResponse processes and decodes an HTTP response into target struct or API error model.
func HandleResponse(resp *http.Response, target any, requester aoni.Requester) error {
	if resp == nil {
		return errors.New("aoni: response is nil")
	}

	dec := responseDecoder{}

	if !dec.setCapturer(resp) {
		defer aoni.CloseResponse(resp)
	}

	dec.dumpDiagnostics(resp, requester)

	decoder := decode.JSONDecoder
	if resp.Request != nil {
		cfg := aoni.GetRequestConfig(resp.Request.Context())

		if cfg != nil && cfg.Decoder != nil {
			if d, ok := cfg.Decoder.(decode.Decoder); ok {
				decoder = d
			}
		}
	}

	if err := dec.validateState(resp, decoder); err != nil {
		return err
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return dec.decodeAPIError(resp)
	}

	if target == nil || resp.StatusCode == http.StatusNoContent {
		bufPtr := bytePool.Get().(*[]byte)
		_, _ = io.CopyBuffer(io.Discard, resp.Body, *bufPtr)
		bytePool.Put(bufPtr)

		return nil
	}

	return dec.decodeSuccess(resp, target, requester, decoder)
}

func validateAndMarshal(payload any) (io.Reader, error) {
	if _, ok := payload.(aoni.RequestModifier); ok {
		return nil, errors.New("aoni: passed a RequestModifier as the request body. Did you forget the body argument?")
	}

	if r, ok := payload.(io.Reader); ok {
		return r, nil
	}

	if payload == nil {
		return nil, nil
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("aoni: failed to marshal payload: %w", err)
	}

	if string(bodyBytes) == "null" {
		bodyBytes = nil
	}

	return bytes.NewReader(bodyBytes), nil
}
