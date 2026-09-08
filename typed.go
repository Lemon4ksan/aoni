// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"reflect"
	"strings"

	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/iokit"
	"github.com/lemon4ksan/foundation/net/headkit"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/internal/requestutil"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/resiliency/challenge"
	"github.com/lemon4ksan/aoni/telemetry"
)

var (
	// ErrUnexpectedContentType indicates that the response Content-Type header violates expected structured MIME formats.
	ErrUnexpectedContentType = errors.New("aoni: unexpected content-type (possible captive portal or intercept)")

	// ErrModifierAsBody is returned when a [RequestModifier] is accidentally passed as a request body payload argument.
	ErrModifierAsBody = errors.New("aoni: passed a RequestModifier as the request body payload")

	// ErrNilResponse is returned when attempting to process a nil [*http.Response].
	ErrNilResponse = errors.New("aoni: response is nil")

	nullJSONBytes = []byte("null")
)

// NoResponse is a sentinel type indicating a request that produces no unmarshaled body structure.
type NoResponse struct{}

const stackModCap = 16

// --- Raw Non-Generic HTTP Methods on *Client ---

// Get executes a raw HTTP GET request against path and returns the raw [*http.Response].
//
// # Resource Management
//
// Caller MUST close resp.Body to prevent socket leaks.
func (c *Client) Get(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return c.Request(ctx, http.MethodGet, path, mods...)
}

// Post executes a raw HTTP POST request carrying body and returns the raw [*http.Response].
//
// The body argument is automatically detected and serialized:
//   - Struct / Map / Slice -> JSON payload with "Content-Type: application/json"
//   - [proto.Message] -> Protobuf binary payload with "Content-Type: application/x-protobuf"
//   - [url.Values] -> Form payload with "Content-Type: application/x-www-form-urlencoded"
//   - `[]byte` / `string` / [io.Reader] -> Raw payload (no default Content-Type header)
func (c *Client) Post(ctx context.Context, path string, body any, mods ...RequestModifier) (*http.Response, error) {
	return c.Fetch(ctx, http.MethodPost, path, body, mods...)
}

// Put executes a raw HTTP PUT request carrying body and returns the raw [*http.Response].
//
// See [Client.Post] for automatic body detection and serialization rules.
func (c *Client) Put(ctx context.Context, path string, body any, mods ...RequestModifier) (*http.Response, error) {
	return c.Fetch(ctx, http.MethodPut, path, body, mods...)
}

// Patch executes a raw HTTP PATCH request carrying body and returns the raw [*http.Response].
//
// See [Client.Post] for automatic body detection and serialization rules.
func (c *Client) Patch(ctx context.Context, path string, body any, mods ...RequestModifier) (*http.Response, error) {
	return c.Fetch(ctx, http.MethodPatch, path, body, mods...)
}

// Delete executes a raw HTTP DELETE request and returns the raw [*http.Response].
//
// # Resource Management
//
// Caller MUST close resp.Body to prevent socket leaks.
func (c *Client) Delete(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return c.Request(ctx, http.MethodDelete, path, mods...)
}

// Options executes a raw HTTP OPTIONS request and returns the raw [*http.Response].
//
// # Resource Management
//
// Caller MUST close resp.Body to prevent socket leaks.
func (c *Client) Options(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return c.Request(ctx, http.MethodOptions, path, mods...)
}

// Fetch executes an arbitrary raw HTTP method request and returns the raw [*http.Response].
//
// See [Client.Post] for automatic body detection and serialization rules.
func (c *Client) Fetch(
	ctx context.Context,
	method, path string,
	body any,
	mods ...RequestModifier,
) (*http.Response, error) {
	var stackBuf [stackModCap]RequestModifier

	allMods, err := prepareBodyMods(body, mods, &stackBuf)
	if err != nil {
		return nil, err
	}

	return c.Request(ctx, method, path, allMods...)
}

// --- Generic Typed HTTP Methods on *Client ---

// GetTo executes an HTTP GET request and automatically decodes the response body into type Resp.
//
// Selects the optimal unmarshaling strategy based on the response Content-Type (JSON, XML, or Protobuf).
// On non-2xx HTTP responses, returns an [*APIError] containing the status code, response headers, and error body.
//
// # Resource Management
//
// The underlying response body stream is automatically drained and closed. Callers do NOT need to call Body.Close().
//
// # Example: Simple Typed Fetch
//
//	type User struct {
//	    ID   int    `json:"id"`
//	    Name string `json:"name"`
//	}
//
//	user, err := client.GetTo[User](ctx, "/users/42")
//	if err != nil {
//	    if aoni.IsNotFound(err) {
//	        // Handle HTTP 404
//	    }
//	    return err
//	}
//
// # Example: Modifiers & Authentication
//
//	user, err := client.GetTo[User](ctx, "/me",
//	    mod.WithBearer(token),
//	    mod.WithQuery("fields", "id,name,email"),
//	)
func (c *Client) GetTo[Resp any](
	ctx context.Context,
	path string,
	mods ...RequestModifier,
) (*Resp, error) {
	//nolint:bodyclose // body is closed inside decodeResponseTo
	resp, err := c.Request(ctx, http.MethodGet, path, mods...)
	if err != nil {
		return nil, err
	}

	return decodeResponseTo[Resp](c, resp)
}

// GetInto executes an HTTP GET request and decodes the response directly into target without heap allocations.
func (c *Client) GetInto[Resp any](
	ctx context.Context,
	path string,
	target *Resp,
	mods ...RequestModifier,
) error {
	//nolint:bodyclose // body is closed inside HandleResponse
	resp, err := c.Request(ctx, http.MethodGet, path, mods...)
	if err != nil {
		return err
	}

	return HandleResponse(resp, target, c)
}

// GetEx executes an HTTP GET request and returns both the unmarshaled *Resp and the raw [*http.Response] metadata.
func (c *Client) GetEx[Resp any](
	ctx context.Context,
	path string,
	mods ...RequestModifier,
) (*Resp, *http.Response, error) {
	return executeToEx[Resp](ctx, c, http.MethodGet, path, nil, mods)
}

// PostTo executes an HTTP POST request carrying body and unmarshals the response into *Resp.
func (c *Client) PostTo[Resp any](
	ctx context.Context,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, error) {
	return c.FetchTo[Resp](ctx, http.MethodPost, path, body, mods...)
}

// PostInto executes an HTTP POST request carrying body and unmarshals the response payload directly into target.
func (c *Client) PostInto[Resp any](
	ctx context.Context,
	path string,
	body any,
	target *Resp,
	mods ...RequestModifier,
) error {
	return c.FetchInto[Resp](ctx, http.MethodPost, path, body, target, mods...)
}

// PostEx executes an HTTP POST request carrying body and returns both the unmarshaled *Resp and raw [*http.Response].
func (c *Client) PostEx[Resp any](
	ctx context.Context,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, *http.Response, error) {
	return executeToEx[Resp](ctx, c, http.MethodPost, path, body, mods)
}

// PutTo executes an HTTP PUT request carrying body and unmarshals the response into *Resp.
func (c *Client) PutTo[Resp any](
	ctx context.Context,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, error) {
	return c.FetchTo[Resp](ctx, http.MethodPut, path, body, mods...)
}

// PutInto executes an HTTP PUT request carrying body and unmarshals the response payload directly into target.
func (c *Client) PutInto[Resp any](
	ctx context.Context,
	path string,
	body any,
	target *Resp,
	mods ...RequestModifier,
) error {
	return c.FetchInto[Resp](ctx, http.MethodPut, path, body, target, mods...)
}

// PutEx executes an HTTP PUT request carrying body and returns both the unmarshaled *Resp and raw [*http.Response].
func (c *Client) PutEx[Resp any](
	ctx context.Context,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, *http.Response, error) {
	return executeToEx[Resp](ctx, c, http.MethodPut, path, body, mods)
}

// PatchTo executes an HTTP PATCH request carrying body and unmarshals the response into *Resp.
func (c *Client) PatchTo[Resp any](
	ctx context.Context,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, error) {
	return c.FetchTo[Resp](ctx, http.MethodPatch, path, body, mods...)
}

// PatchInto executes an HTTP PATCH request carrying body and unmarshals the response payload directly into target.
func (c *Client) PatchInto[Resp any](
	ctx context.Context,
	path string,
	body any,
	target *Resp,
	mods ...RequestModifier,
) error {
	return c.FetchInto[Resp](ctx, http.MethodPatch, path, body, target, mods...)
}

// PatchEx executes an HTTP PATCH request carrying body and returns both the unmarshaled *Resp and raw [*http.Response].
func (c *Client) PatchEx[Resp any](
	ctx context.Context,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, *http.Response, error) {
	return executeToEx[Resp](ctx, c, http.MethodPatch, path, body, mods)
}

// DeleteTo executes an HTTP DELETE request and unmarshals any returned response payload into *Resp.
func (c *Client) DeleteTo[Resp any](
	ctx context.Context,
	path string,
	mods ...RequestModifier,
) (*Resp, error) {
	//nolint:bodyclose // body is closed inside decodeResponseTo
	resp, err := c.Request(ctx, http.MethodDelete, path, mods...)
	if err != nil {
		return nil, err
	}

	return decodeResponseTo[Resp](c, resp)
}

// DeleteInto executes an HTTP DELETE request and unmarshals the response directly into target.
func (c *Client) DeleteInto[Resp any](
	ctx context.Context,
	path string,
	target *Resp,
	mods ...RequestModifier,
) error {
	//nolint:bodyclose // body is closed inside HandleResponse
	resp, err := c.Request(ctx, http.MethodDelete, path, mods...)
	if err != nil {
		return err
	}

	return HandleResponse(resp, target, c)
}

// DeleteEx executes an HTTP DELETE request and returns both the unmarshaled *Resp and raw [*http.Response].
func (c *Client) DeleteEx[Resp any](
	ctx context.Context,
	path string,
	mods ...RequestModifier,
) (*Resp, *http.Response, error) {
	return executeToEx[Resp](ctx, c, http.MethodDelete, path, nil, mods)
}

// FetchTo executes an arbitrary HTTP method request, marshaling body if provided, and unmarshals the response into *Resp.
func (c *Client) FetchTo[Resp any](
	ctx context.Context,
	method, path string,
	body any,
	mods ...RequestModifier,
) (*Resp, error) {
	var stackBuf [stackModCap]RequestModifier

	allMods, err := prepareBodyMods(body, mods, &stackBuf)
	if err != nil {
		return nil, err
	}

	//nolint:bodyclose // body is closed inside decodeResponseTo
	resp, err := c.Request(ctx, method, path, allMods...)
	if err != nil {
		return nil, err
	}

	return decodeResponseTo[Resp](c, resp)
}

// FetchInto executes an arbitrary HTTP method request, marshaling body if provided, and unmarshals response into target.
func (c *Client) FetchInto[Resp any](
	ctx context.Context,
	method, path string,
	body any,
	target *Resp,
	mods ...RequestModifier,
) error {
	var stackBuf [stackModCap]RequestModifier

	allMods, err := prepareBodyMods(body, mods, &stackBuf)
	if err != nil {
		return err
	}

	//nolint:bodyclose // body is closed inside HandleResponse
	resp, err := c.Request(ctx, method, path, allMods...)
	if err != nil {
		return err
	}

	return HandleResponse(resp, target, c)
}

// DoInto is an alias for [Client.FetchInto].
func (c *Client) DoInto[Resp any](
	ctx context.Context,
	method, path string,
	body any,
	target *Resp,
	mods ...RequestModifier,
) error {
	return c.FetchInto[Resp](ctx, method, path, body, target, mods...)
}

// FetchEx performs an arbitrary HTTP request and returns both the unmarshaled *Resp and raw [*http.Response].
func (c *Client) FetchEx[Resp any](
	ctx context.Context,
	method, path string,
	body any,
	mods ...RequestModifier,
) (*Resp, *http.Response, error) {
	return executeToEx[Resp](ctx, c, method, path, body, mods)
}

// DoEx is an alias for [Client.FetchEx].
func (c *Client) DoEx[Resp any](
	ctx context.Context,
	method, path string,
	body any,
	mods ...RequestModifier,
) (*Resp, *http.Response, error) {
	return c.FetchEx[Resp](ctx, method, path, body, mods...)
}

// UnwrapClient peels away decorator layers and returns the innermost [*Client].
func UnwrapClient(target any) *Client {
	if c, ok := target.(*Client); ok {
		return c
	}

	if unwrapped, ok := UnwrapAs[*Client](target); ok {
		return unwrapped
	}

	return nil
}

// --- Internal Decoding & Response Handlers ---

func decodeResponseTo[Resp any](c *Client, resp *http.Response) (*Resp, error) {
	var zero *Resp
	if _, ok := any(zero).(*NoResponse); ok {
		return nil, HandleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := HandleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

func executeToEx[Resp any](
	ctx context.Context,
	c *Client,
	method, path string,
	body any,
	mods []RequestModifier,
) (*Resp, *http.Response, error) {
	var (
		raw      *http.Response
		stackBuf [stackModCap]RequestModifier
	)

	reqMods := withCaptureMod(&stackBuf, &raw, mods)

	result, err := c.FetchTo[Resp](ctx, method, path, body, reqMods...)
	if err != nil {
		if raw != nil && raw.Body != nil {
			_ = raw.Body.Close()
		}

		return nil, raw, err
	}

	return result, raw, nil
}

// HandleResponse processes and decodes an HTTP response stream into a target structure or API error.
func HandleResponse(resp *http.Response, target, client any) error {
	if resp == nil {
		return ErrNilResponse
	}

	h := newResponseHandler(resp, target, client)

	if !h.captureResponse() {
		defer CloseResponse(resp)
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

// responseHandler encapsulates the lifecycle and decoding pipeline of an HTTP response.
type responseHandler struct {
	resp    *http.Response
	target  any
	client  any
	cfg     *RequestConfig
	decoder decode.Decoder
}

func newResponseHandler(resp *http.Response, target, client any) responseHandler {
	cfg := extractRequestConfig(resp)

	return responseHandler{
		resp:    resp,
		target:  target,
		client:  client,
		cfg:     cfg,
		decoder: resolveDecoder(resp, cfg),
	}
}

func (h *responseHandler) captureResponse() bool {
	if h.cfg != nil {
		if targetPtr, ok := h.cfg.Capturer.(**http.Response); ok && targetPtr != nil {
			*targetPtr = h.resp
			return true
		}
	}

	return false
}

func (h *responseHandler) isErrorStatus() bool {
	return h.resp.StatusCode < http.StatusOK || h.resp.StatusCode >= http.StatusMultipleChoices
}

func (h *responseHandler) shouldDiscardBody() bool {
	return h.target == nil || h.resp.StatusCode == http.StatusNoContent || isNoResponseTarget(h.target)
}

func (h *responseHandler) drainAndDiscard() error {
	_, err := iokit.CopyZeroAlloc(io.Discard, h.resp.Body)
	return err
}

func (h *responseHandler) validate() error {
	if h.resp.Body == nil || decode.IsRawDecoder(h.decoder) {
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

func (h *responseHandler) checkMIMEType() error {
	contentType := h.resp.Header.Get("Content-Type")
	if contentType == "" {
		return nil
	}

	mediaType := headkit.BaseMediaType(contentType)
	if bytesconv.EqualFoldASCII(mediaType, "text/html") ||
		bytesconv.EqualFoldASCII(mediaType, "application/xhtml+xml") {
		return fmt.Errorf("%w: expected structured data but got HTML", ErrUnexpectedContentType)
	}

	return nil
}

func (h *responseHandler) checkHTML(buf *bufio.Reader) error {
	peekBytes, err := buf.Peek(128)
	if (err != nil && err != io.EOF) || len(peekBytes) == 0 {
		return nil
	}

	firstNonSpace := requestutil.FindFirstNonWhitespaceByte(peekBytes)
	if firstNonSpace != '<' {
		return nil
	}

	lowerPeek := bytes.ToLower(peekBytes)
	if !bytes.Contains(lowerPeek, []byte("<html")) && !bytes.Contains(lowerPeek, []byte("<!doctype html")) {
		return nil
	}

	if requestutil.IsCloudflareChallengeBytes(lowerPeek) {
		return challenge.ErrCloudflareDetected
	}

	return fmt.Errorf("%w: expected structured data but got HTML", ErrUnexpectedContentType)
}

func (h *responseHandler) decodeAPIError() error {
	bodyBytes, _ := io.ReadAll(io.LimitReader(h.resp.Body, 1024*1024))
	apiErr := &APIError{StatusCode: h.resp.StatusCode, Body: bodyBytes}

	if h.cfg != nil && h.cfg.ErrorModel != nil {
		if err := json.Unmarshal(bodyBytes, h.cfg.ErrorModel); err == nil {
			apiErr.Model = h.cfg.ErrorModel
		}
	}

	return apiErr
}

func (h *responseHandler) decodeSuccess() error {
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

	err := h.decoder.Decode(h.resp.Body, h.target)
	if errors.Is(err, io.EOF) {
		return nil
	}

	return err
}

func (h *responseHandler) extractBaseResponse() BaseResponse {
	if h.cfg != nil {
		switch {
		case h.cfg.DisableBaseResponse:
			return nil
		case h.cfg.BaseResponseOverride != nil:
			return h.cfg.BaseResponseOverride()
		}
	}

	if h.client != nil {
		if cli, ok := h.client.(*Client); ok {
			if cli == nil {
				return nil
			}

			return cli.BaseResponse()
		}

		if p, ok := h.client.(BaseResponseProvider); ok && p != nil {
			return p.BaseResponse()
		}
	}

	return nil
}

func (h *responseHandler) dumpDiagnostics() {
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
	case NoResponse, *NoResponse, **NoResponse:
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

func extractRequestConfig(resp *http.Response) *RequestConfig {
	if resp != nil && resp.Request != nil {
		return GetRequestConfig(resp.Request.Context())
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

func resolveDecoder(resp *http.Response, cfg *RequestConfig) decode.Decoder {
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
			return decode.LookupDecoder(contentType)
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
		}
	}

	return decode.JSONDecoder
}

// --- Payload Serialization ---

type preparedPayload struct {
	bodyReader    io.Reader
	contentType   string
	contentLength int64
	getBody       func() (io.ReadCloser, error)
}

func (p preparedPayload) IsEmpty() bool {
	return p.bodyReader == nil
}

func (p preparedPayload) HasContentType() bool {
	return p.contentType != ""
}

// PrependTo prepends the serialized body and content-type headers to the modifiers slice.
func (p preparedPayload) PrependTo(
	mods []RequestModifier,
	stackBuf *[stackModCap]RequestModifier,
) []RequestModifier {
	if p.IsEmpty() {
		return mods
	}

	totalLen := len(mods) + generic.Ternary(p.HasContentType(), 2, 1)

	var allMods []RequestModifier
	if totalLen <= stackModCap && stackBuf != nil {
		allMods = stackBuf[:0]
	} else {
		allMods = make([]RequestModifier, 0, totalLen)
	}

	allMods = append(allMods, mod.WithBody(p.bodyReader))
	if p.HasContentType() {
		allMods = append(allMods, mod.WithHeader("Content-Type", p.contentType))
	}

	allMods = append(allMods, mods...)

	return allMods
}

func payloadFromBytes(b []byte, contentType string) preparedPayload {
	return preparedPayload{
		bodyReader:    bytes.NewReader(b),
		contentType:   contentType,
		contentLength: int64(len(b)),
		getBody: func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(b)), nil
		},
	}
}

func payloadFromString(s, contentType string) preparedPayload {
	return preparedPayload{
		bodyReader:    strings.NewReader(s),
		contentType:   contentType,
		contentLength: int64(len(s)),
		getBody: func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(s)), nil
		},
	}
}

func validateAndMarshal(payload any) (preparedPayload, error) {
	if payload == nil {
		return preparedPayload{}, nil
	}

	switch v := payload.(type) {
	case RequestModifier:
		return preparedPayload{}, ErrModifierAsBody

	case io.Reader:
		return preparedPayload{bodyReader: v, contentLength: -1}, nil

	case []byte:
		return payloadFromBytes(v, ""), nil

	case string:
		return payloadFromString(v, ""), nil

	case url.Values:
		return payloadFromString(v.Encode(), "application/x-www-form-urlencoded"), nil

	case *url.Values:
		if v == nil {
			return preparedPayload{}, nil
		}

		return payloadFromString(v.Encode(), "application/x-www-form-urlencoded"), nil

	case proto.Message:
		if v == nil || (reflect.ValueOf(v).Kind() == reflect.Pointer && reflect.ValueOf(v).IsNil()) {
			return preparedPayload{}, nil
		}

		bodyBytes, err := proto.Marshal(v)
		if err != nil {
			return preparedPayload{}, fmt.Errorf("aoni: failed to marshal protobuf payload: %w", err)
		}

		return payloadFromBytes(bodyBytes, "application/x-protobuf"), nil

	default:
		bodyBytes, err := json.Marshal(v)
		if err != nil {
			return preparedPayload{}, fmt.Errorf("aoni: failed to marshal JSON payload: %w", err)
		}

		if bytes.Equal(bodyBytes, nullJSONBytes) {
			return preparedPayload{}, nil
		}

		return payloadFromBytes(bodyBytes, "application/json"), nil
	}
}

func prepareBodyMods(
	body any,
	mods []RequestModifier,
	stackBuf *[stackModCap]RequestModifier,
) ([]RequestModifier, error) {
	if body == nil {
		return mods, nil
	}

	payload, err := validateAndMarshal(body)
	if err != nil {
		return nil, err
	}

	return payload.PrependTo(mods, stackBuf), nil
}

func withCaptureMod(
	stackBuf *[stackModCap]RequestModifier,
	target **http.Response,
	mods []RequestModifier,
) []RequestModifier {
	totalLen := len(mods) + 1

	var allMods []RequestModifier
	if totalLen <= stackModCap && stackBuf != nil {
		allMods = stackBuf[:0]
	} else {
		allMods = make([]RequestModifier, 0, totalLen)
	}

	allMods = append(allMods, mod.WithCaptureResponse(target))
	allMods = append(allMods, mods...)

	return allMods
}
