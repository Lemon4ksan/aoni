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
	"os"
	"strings"

	fio "github.com/lemon4ksan/foundation/iokit"
	"github.com/lemon4ksan/foundation/net/headkit"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"

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

const stackModCapacity = 16

// --- Generic HTTP Methods on *Client ---

// Get executes an HTTP GET request and automatically decodes the response body into type Resp.
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
//	user, err := client.Get[User](ctx, "/users/42")
//	if err != nil {
//	    if aoni.IsNotFound(err) {
//	        // Handle HTTP 404
//	    }
//	    return err
//	}
//
// # Example: Modifiers & Authentication
//
//	user, err := client.Get[User](ctx, "/me",
//	    mod.WithBearer(token),
//	    mod.WithQuery("fields", "id,name,email"),
//	)
func (c *Client) Get[Resp any](
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
//
// # Example
//
//	var user User
//	err := client.GetInto(ctx, "/users/42", &user)
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

// Post executes an HTTP POST request carrying body and unmarshals the response into *Resp.
//
// The body argument is automatically detected and serialized:
//   - Struct / Map / Slice -> JSON payload with "Content-Type: application/json"
//   - [proto.Message] -> Protobuf binary payload with "Content-Type: application/x-protobuf"
//   - [url.Values] -> Form payload with "Content-Type: application/x-www-form-urlencoded"
//   - `[]byte` / `string` -> Raw payload
//
// # Example
//
//	created, err := client.Post[User](ctx, "/users", CreateUserReq{Name: "Bob"})
func (c *Client) Post[Resp any](
	ctx context.Context,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, error) {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return nil, err
	}

	var stackBuf [stackModCapacity]RequestModifier

	allMods := withJSONBodyMods(&stackBuf, bodyReader, mods)

	//nolint:bodyclose // body is closed inside decodeResponseTo
	resp, err := c.Request(ctx, http.MethodPost, path, allMods...)
	if err != nil {
		return nil, err
	}

	return decodeResponseTo[Resp](c, resp)
}

// PostInto executes an HTTP POST request and unmarshals the response payload directly into target.
func (c *Client) PostInto[Resp any](
	ctx context.Context,
	path string,
	body any,
	target *Resp,
	mods ...RequestModifier,
) error {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return err
	}

	var stackBuf [stackModCapacity]RequestModifier

	allMods := withJSONBodyMods(&stackBuf, bodyReader, mods)

	//nolint:bodyclose // body is closed inside HandleResponse
	resp, err := c.Request(ctx, http.MethodPost, path, allMods...)
	if err != nil {
		return err
	}

	return HandleResponse(resp, target, c)
}

// PostEx executes an HTTP POST request and returns both the unmarshaled *Resp and raw [*http.Response].
func (c *Client) PostEx[Resp any](
	ctx context.Context,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, *http.Response, error) {
	return executeToEx[Resp](ctx, c, http.MethodPost, path, body, mods)
}

// Put executes an HTTP PUT request carrying body and unmarshals the response into *Resp.
//
// # Example
//
//	updated, err := client.Put[User](ctx, "/users/42", UpdateUserReq{Name: "Robert"})
func (c *Client) Put[Resp any](
	ctx context.Context,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, error) {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return nil, err
	}

	var stackBuf [stackModCapacity]RequestModifier

	allMods := withJSONBodyMods(&stackBuf, bodyReader, mods)

	//nolint:bodyclose // body is closed inside decodeResponseTo
	resp, err := c.Request(ctx, http.MethodPut, path, allMods...)
	if err != nil {
		return nil, err
	}

	return decodeResponseTo[Resp](c, resp)
}

// PutInto executes an HTTP PUT request and unmarshals the response payload directly into target.
func (c *Client) PutInto[Resp any](
	ctx context.Context,
	path string,
	body any,
	target *Resp,
	mods ...RequestModifier,
) error {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return err
	}

	var stackBuf [stackModCapacity]RequestModifier

	allMods := withJSONBodyMods(&stackBuf, bodyReader, mods)

	//nolint:bodyclose // body is closed inside HandleResponse
	resp, err := c.Request(ctx, http.MethodPut, path, allMods...)
	if err != nil {
		return err
	}

	return HandleResponse(resp, target, c)
}

// PutEx executes an HTTP PUT request and returns both the unmarshaled *Resp and raw [*http.Response].
func (c *Client) PutEx[Resp any](
	ctx context.Context,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, *http.Response, error) {
	return executeToEx[Resp](ctx, c, http.MethodPut, path, body, mods)
}

// Patch executes an HTTP PATCH request carrying body and unmarshals the response into *Resp.
//
// # Example
//
//	patched, err := client.Patch[User](ctx, "/users/42", map[string]any{"status": "active"})
func (c *Client) Patch[Resp any](
	ctx context.Context,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, error) {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return nil, err
	}

	var stackBuf [stackModCapacity]RequestModifier

	allMods := withJSONBodyMods(&stackBuf, bodyReader, mods)

	//nolint:bodyclose // body is closed inside decodeResponseTo
	resp, err := c.Request(ctx, http.MethodPatch, path, allMods...)
	if err != nil {
		return nil, err
	}

	return decodeResponseTo[Resp](c, resp)
}

// PatchInto executes an HTTP PATCH request and unmarshals the response payload directly into target.
func (c *Client) PatchInto[Resp any](
	ctx context.Context,
	path string,
	body any,
	target *Resp,
	mods ...RequestModifier,
) error {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return err
	}

	var stackBuf [stackModCapacity]RequestModifier

	allMods := withJSONBodyMods(&stackBuf, bodyReader, mods)

	//nolint:bodyclose // body is closed inside HandleResponse
	resp, err := c.Request(ctx, http.MethodPatch, path, allMods...)
	if err != nil {
		return err
	}

	return HandleResponse(resp, target, c)
}

// PatchEx executes an HTTP PATCH request and returns both the unmarshaled *Resp and raw [*http.Response].
func (c *Client) PatchEx[Resp any](
	ctx context.Context,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, *http.Response, error) {
	return executeToEx[Resp](ctx, c, http.MethodPatch, path, body, mods)
}

// Delete executes an HTTP DELETE request and unmarshals any returned response payload into *Resp.
//
// # Example
//
//	status, err := client.Delete[DeleteStatus](ctx, "/users/42")
func (c *Client) Delete[Resp any](
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

// Options executes an HTTP OPTIONS request and unmarshals the response into *Resp.
func (c *Client) Options[Resp any](
	ctx context.Context,
	path string,
	mods ...RequestModifier,
) (*Resp, error) {
	//nolint:bodyclose // body is closed inside decodeResponseTo
	resp, err := c.Request(ctx, http.MethodOptions, path, mods...)
	if err != nil {
		return nil, err
	}

	return decodeResponseTo[Resp](c, resp)
}

// Fetch executes an arbitrary HTTP method request, marshaling body if provided, and unmarshals the response into *Resp.
func (c *Client) Fetch[Resp any](
	ctx context.Context,
	method, path string,
	body any,
	mods ...RequestModifier,
) (*Resp, error) {
	if body != nil {
		bodyReader, err := validateAndMarshal(body)
		if err != nil {
			return nil, err
		}

		var stackBuf [stackModCapacity]RequestModifier

		mods = withJSONBodyMods(&stackBuf, bodyReader, mods)
	}

	//nolint:bodyclose // body is closed inside decodeResponseTo
	resp, err := c.Request(ctx, method, path, mods...)
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
	if body != nil {
		bodyReader, err := validateAndMarshal(body)
		if err != nil {
			return err
		}

		var stackBuf [stackModCapacity]RequestModifier

		mods = withJSONBodyMods(&stackBuf, bodyReader, mods)
	}

	//nolint:bodyclose // body is closed inside HandleResponse
	resp, err := c.Request(ctx, method, path, mods...)
	if err != nil {
		return err
	}

	return HandleResponse(resp, target, c)
}

// DoInto is an alias for FetchInto.
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

// DoEx is an alias for FetchEx.
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
		stackBuf [stackModCapacity]RequestModifier
	)

	reqMods := withCaptureMod(&stackBuf, &raw, mods)

	result, err := c.Fetch[Resp](ctx, method, path, body, reqMods...)
	if err != nil {
		if raw != nil && raw.Body != nil {
			_ = raw.Body.Close()
		}

		return nil, raw, err
	}

	return result, raw, nil
}

// HandleResponse processes and decodes an HTTP response stream into a target structure or API error.
func HandleResponse(resp *http.Response, target, c any) error {
	if resp == nil {
		return ErrNilResponse
	}

	dec := responseDecoder{}

	if !dec.SetCapturer(resp) {
		defer CloseResponse(resp)
	}

	dec.DumpDiagnostics(resp, c)

	decoder := resolveDecoder(resp)

	if err := dec.ValidateState(resp, decoder); err != nil {
		return err
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return dec.DecodeAPIError(resp)
	}

	if target == nil || resp.StatusCode == http.StatusNoContent || isNoResponseTarget(target) {
		_, _ = fio.CopyZeroAlloc(io.Discard, resp.Body)
		return nil
	}

	return dec.DecodeSuccess(resp, target, c, decoder)
}

func isNoResponseTarget(target any) bool {
	switch target.(type) {
	case NoResponse, *NoResponse, **NoResponse:
		return true
	default:
		return false
	}
}

type responseDecoder struct{}

func (responseDecoder) SetCapturer(resp *http.Response) bool {
	if resp.Request == nil {
		return false
	}

	cfg := GetRequestConfig(resp.Request.Context())
	if cfg != nil {
		if targetPtr, ok := cfg.Capturer.(**http.Response); ok && targetPtr != nil {
			*targetPtr = resp
			return true
		}
	}

	return false
}

func (responseDecoder) DumpDiagnostics(resp *http.Response, c any) {
	if resp.Request == nil {
		return
	}

	cfg := GetRequestConfig(resp.Request.Context())
	if cfg == nil || !cfg.Debug {
		return
	}

	reqDump := dumpMultipart(resp.Request)
	if len(reqDump) == 0 {
		reqDump, _ = httputil.DumpRequestOut(resp.Request, true)
	}

	var respDump []byte
	if telemetry.IsStreamingResponse(resp) {
		respDump = []byte(
			resp.Proto + " " + resp.Status + "\r\nContent-Type: " + resp.Header.Get(
				"Content-Type",
			) + "\r\n\r\n[streaming body omitted]",
		)
	} else {
		respDump, _ = httputil.DumpResponse(resp, true)
	}

	fmt.Fprintf(
		os.Stderr,
		"--- [aoni Debug: %s %s] ---\nRequest:\n%s\nResponse:\n%s\n-------------------------\n",
		resp.Request.Method,
		resp.Request.URL.String(),
		bytesconv.B2S(requestutil.RedactHeaders(reqDump)),
		bytesconv.B2S(requestutil.RedactHeaders(respDump)),
	)
}

func (d responseDecoder) ValidateState(resp *http.Response, decoder decode.Decoder) error {
	if resp == nil || resp.Body == nil || decode.IsRawDecoder(decoder) {
		return nil
	}

	if resp.StatusCode < http.StatusBadRequest {
		contentType := resp.Header.Get("Content-Type")
		if contentType == "" || decode.IsStructuredMediaType(contentType) {
			return nil
		}
	}

	peekableReader := ResolvePeekableReader(resp)

	if err := d.checkHTML(peekableReader); err != nil {
		return err
	}

	return d.checkMIMEType(resp)
}

func (responseDecoder) checkMIMEType(resp *http.Response) error {
	contentType := resp.Header.Get("Content-Type")
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

func (responseDecoder) checkHTML(buf *bufio.Reader) error {
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

// ResolvePeekableReader returns a peekable reader for the response body.
func ResolvePeekableReader(resp *http.Response) *bufio.Reader {
	if b, ok := resp.Body.(*fio.BufioReadCloser); ok && b.Reader != nil {
		return b.Reader
	}

	if br, ok := resp.Body.(interface{ BufioReader() *bufio.Reader }); ok {
		if r := br.BufioReader(); r != nil {
			return r
		}
	}

	wrapped := fio.NewBufioReadCloser(resp.Body, resp.Body)
	resp.Body = wrapped

	return wrapped.Reader
}

func (responseDecoder) DecodeAPIError(resp *http.Response) error {
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	apiErr := &APIError{StatusCode: resp.StatusCode, Body: bodyBytes}

	if resp.Request != nil {
		cfg := GetRequestConfig(resp.Request.Context())
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
	c any,
	decoder decode.Decoder,
) error {
	if br := extractBaseResponse(c, resp); br != nil {
		br.SetData(target)

		if err := decoder.Decode(resp.Body, br); err != nil {
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

	if target == nil {
		return nil
	}

	err := decoder.Decode(resp.Body, target)
	if errors.Is(err, io.EOF) {
		return nil
	}

	return err
}

func extractBaseResponse(c any, resp *http.Response) BaseResponse {
	if resp != nil && resp.Request != nil {
		if cfg := GetRequestConfig(resp.Request.Context()); cfg != nil {
			switch {
			case cfg.DisableBaseResponse:
				return nil
			case cfg.BaseResponseOverride != nil:
				return cfg.BaseResponseOverride()
			}
		}
	}

	if c != nil {
		if cli, ok := c.(*Client); ok {
			if cli == nil {
				return nil
			}

			return cli.BaseResponse()
		}

		if p, ok := c.(BaseResponseProvider); ok && p != nil {
			return p.BaseResponse()
		}
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

func resolveDecoder(resp *http.Response) decode.Decoder {
	if resp != nil && resp.Request != nil {
		cfg := GetRequestConfig(resp.Request.Context())
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

func validateAndMarshal(payload any) (io.Reader, error) {
	if _, ok := payload.(RequestModifier); ok {
		return nil, ErrModifierAsBody
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

	if bytes.Equal(bodyBytes, nullJSONBytes) {
		bodyBytes = nil
	}

	return bytes.NewReader(bodyBytes), nil
}

func withJSONBodyMods(
	stackBuf *[stackModCapacity]RequestModifier,
	bodyReader io.Reader,
	mods []RequestModifier,
) []RequestModifier {
	if bodyReader == nil {
		return mods
	}

	var allMods []RequestModifier

	totalLen := len(mods) + 2

	if totalLen <= stackModCapacity && stackBuf != nil {
		allMods = stackBuf[:0]
	} else {
		allMods = make([]RequestModifier, 0, totalLen)
	}

	allMods = append(allMods, mod.WithBody(bodyReader), mod.WithHeader("Content-Type", "application/json"))
	allMods = append(allMods, mods...)

	return allMods
}

func withCaptureMod(
	stackBuf *[stackModCapacity]RequestModifier,
	target **http.Response,
	mods []RequestModifier,
) []RequestModifier {
	totalLen := len(mods) + 1

	var allMods []RequestModifier

	if totalLen <= stackModCapacity && stackBuf != nil {
		allMods = stackBuf[:0]
	} else {
		allMods = make([]RequestModifier, 0, totalLen)
	}

	allMods = append(allMods, mod.WithCaptureResponse(target))
	allMods = append(allMods, mods...)

	return allMods
}
