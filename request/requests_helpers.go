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
	"github.com/lemon4ksan/aoni/internal/io"
	"github.com/lemon4ksan/aoni/resiliency/challenge"
	"github.com/lemon4ksan/aoni/telemetry"
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

func (d responseDecoder) ValidateState(resp *http.Response, decoder decode.Decoder) error {
	if resp == nil || resp.Body == nil {
		return nil
	}

	if decode.IsRawDecoder(decoder) {
		return nil
	}

	contentType := resp.Header.Get("Content-Type")

	if resp.StatusCode < 400 && contentType != "" {
		mediaType, _, _ := strings.Cut(contentType, ";")
		mediaType = strings.TrimSpace(strings.ToLower(mediaType))

		if mediaType == "application/json" ||
			mediaType == "application/x-protobuf" ||
			mediaType == "application/protobuf" ||
			mediaType == "application/grpc-web+proto" {
			return nil
		}
	}

	var peekableReader *bufio.Reader
	if b, ok := resp.Body.(*io.BufioReadCloser); ok {
		peekableReader = b.Reader
	} else if br, ok := resp.Body.(interface{ BufioReader() *bufio.Reader }); ok {
		peekableReader = br.BufioReader()
	} else {
		peekableReader = bufio.NewReader(resp.Body)
		resp.Body = &io.BufioReadCloser{
			Reader: peekableReader,
			Closer: resp.Body,
		}
	}

	if err := d.checkHTML(peekableReader); err != nil {
		return err
	}

	return d.checkMIMEType(resp)
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

	reqDump, respDump = redactHeaders(reqDump), redactHeaders(respDump)

	logger, ok := requester.(interface{ Logger() aoni.Logger })
	if ok {
		logger.Logger().Debug("Aoni HTTP Diagnostic", "request", string(reqDump), "response", string(respDump))
		return
	}

	fmt.Fprintf(
		os.Stderr,
		"\n--- HTTP DEBUG ---\n%s\n\n%s\n------------------\n",
		string(reqDump),
		string(respDump),
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

		if cfg == nil || cfg.ErrorModel == nil {
			return apiErr
		}

		if err := json.Unmarshal(bodyBytes, cfg.ErrorModel); err == nil {
			apiErr.Model = cfg.ErrorModel
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
	var br aoni.BaseResponse
	if p, ok := requester.(aoni.BaseResponseProvider); ok {
		br = p.BaseResponse()
	} else if provider, ok := requester.(interface{ BaseResponse() aoni.BaseResponse }); ok {
		br = provider.BaseResponse()
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
	if errors.Is(err, stdio.EOF) {
		return nil
	}

	return err
}

func (responseDecoder) dumpMultipart(req *http.Request) []byte {
	contentType := req.Header.Get("Content-Type")

	if !strings.HasPrefix(strings.ToLower(contentType), "multipart/form-data") || req.GetBody == nil {
		return nil
	}

	bodyRc, err := req.GetBody()
	if err != nil {
		return nil
	}

	bodyBytes, _ := stdio.ReadAll(stdio.LimitReader(bodyRc, 256*1024))
	_ = bodyRc.Close()

	summary := telemetry.SummarizeMultipartBody(bodyBytes, contentType)

	return []byte(
		req.Method + " " + req.URL.RequestURI() + " HTTP/1.1\r\nContent-Type: " + contentType + "\r\n\r\n" + summary,
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

	if mediaType == "text/html" || mediaType == "application/xhtml+xml" {
		return fmt.Errorf("%w: expected structured data but got HTML", aoni.ErrUnexpectedContentType)
	}

	return nil
}

func (responseDecoder) checkHTML(buf *bufio.Reader) error {
	peekBytes, err := buf.Peek(128)

	if err != nil && err != stdio.EOF {
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

var bytePool = sync.Pool{
	New: func() any {
		b := make([]byte, 32*1024)
		return &b
	},
}

// HandleResponse processes and decodes an HTTP response into target struct or API error model.
func HandleResponse(resp *http.Response, target any, requester Requester) error {
	if resp == nil {
		return errors.New("aoni: response is nil")
	}

	dec := responseDecoder{}

	if !dec.SetCapturer(resp) {
		defer aoni.CloseResponse(resp)
	}

	dec.DumpDiagnostics(resp, requester)

	decoder := decode.JSONDecoder
	if resp.Request != nil {
		cfg := aoni.GetRequestConfig(resp.Request.Context())

		if cfg != nil {
			if cfg.ForceContentType != "" {
				mime := strings.ToLower(cfg.ForceContentType)
				switch {
				case strings.Contains(mime, "xml"):
					decoder = decode.XMLDecoder
				case strings.Contains(mime, "json"):
					decoder = decode.JSONDecoder
				}
			}

			if cfg.Decoder != nil {
				if d, ok := cfg.Decoder.(decode.Decoder); ok {
					decoder = d
				}
			}
		}
	}

	if err := dec.ValidateState(resp, decoder); err != nil {
		return err
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return dec.DecodeAPIError(resp)
	}

	if target == nil || resp.StatusCode == http.StatusNoContent {
		bufPtr := bytePool.Get().(*[]byte)
		_, _ = stdio.CopyBuffer(stdio.Discard, resp.Body, *bufPtr)
		bytePool.Put(bufPtr)

		return nil
	}

	return dec.DecodeSuccess(resp, target, requester, decoder)
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

	if string(bodyBytes) == "null" {
		bodyBytes = nil
	}

	return bytes.NewReader(bodyBytes), nil
}
