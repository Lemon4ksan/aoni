// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"errors"
	"io"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/net/http/header"
	"github.com/lemon4ksan/foundation/net/urlkit"
	"github.com/lemon4ksan/foundation/silicon/pool"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni/x/codec"
	"github.com/lemon4ksan/aoni/x/codec/decode"
	"github.com/lemon4ksan/aoni/x/telemetry"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/internal/download"
	"github.com/lemon4ksan/aoni/mod"
)

var (
	// ErrUnexpectedStatus is returned when the response status code does not match expected codes.
	ErrUnexpectedStatus = errors.New("aoni: unexpected HTTP status code")

	// ErrDownloadFailed indicates a download request failure due to an HTTP error status code.
	ErrDownloadFailed = download.ErrDownloadFailed

	// ErrRangeNotSatisfiable is returned when the requested byte range exceeds remote file size (HTTP 416).
	ErrRangeNotSatisfiable = errors.New("aoni: requested byte range not satisfiable by server")

	// ErrBuilderConsumed is returned when attempting to execute a RequestBuilder that has already been executed.
	ErrBuilderConsumed = errors.New("aoni: RequestBuilder already executed and consumed")
)

type requestPool struct {
	storage *pool.PerPStorage[*RequestBuilder]
}

func newRequestPool() *requestPool {
	return &requestPool{
		storage: pool.NewPerPStorage(func() *RequestBuilder {
			return &RequestBuilder{
				appliedMods:      make([]RequestModifier, 0, 8),
				expectedStatuses: make([]int, 0, 4),
				validators:       make([]func(*http.Response) error, 0, 2),
				multipartFields:  make([]mod.MultipartField, 0, 4),
				pathParams:       make(map[string]string, 4),
			}
		}),
	}
}

// Get retrieves a pooled [RequestBuilder] instance bound to any engine or client.
func (p *requestPool) Get(doer HTTPRequester) *RequestBuilder {
	r := p.storage.Get()
	r.client = generic.CoalesceNil[HTTPRequester](doer, DefaultClient)
	r.consumed = false

	return r
}

// Put recycles a [RequestBuilder] instance back to the core-pinned storage after resetting fields.
func (p *requestPool) Put(r *RequestBuilder) {
	if r == nil {
		return
	}

	r.Reset()
	p.storage.Put(r)
}

var requestBuilderPool = newRequestPool()

func acquireRequestBuilder(doer HTTPRequester) *RequestBuilder {
	return requestBuilderPool.Get(doer)
}

// RequestBuilder provides a chainable API for configuring and executing HTTP requests.
//
// DANGER: RequestBuilder instances are acquired from a thread-local object pool ([pool.PerPStorage]).
// They are NOT safe for concurrent use across multiple goroutines.
// The instance is automatically recycled upon execution (e.g. Execute) or explicit Release.
// Do not retain or mutate references to a RequestBuilder after it has been executed or released.
type RequestBuilder struct {
	client           HTTPRequester
	ctx              context.Context
	pathParams       map[string]string
	multipartFields  []mod.MultipartField
	appliedMods      []RequestModifier
	result           any
	expectedStatuses []int
	outputFile       string
	outputDirectory  string
	authMod          RequestModifier
	signer           func(*http.Request) error
	sink             func(*http.Response) error
	validators       []func(*http.Response) error
	pluginErr        error
	retryOverride    *core.RetryOverride
	consumed         bool
}

// R acquires a pooled [RequestBuilder] bound to this [Client] instance.
// The builder is automatically recycled upon request execution or explicit [RequestBuilder.Release].
//
// # Example
//
//	var user User
//	resp, err := client.R().
//	    SetHeader("Accept", "application/json").
//	    SetQueryParam("version", "2").
//	    SetResult(&user).
//	    Get("/users/42")
func (c *Client) R() *RequestBuilder {
	return acquireRequestBuilder(c)
}

// NewRequest returns a pooled [RequestBuilder] bound to this [Client] instance (alias for [Client.R]).
func (c *Client) NewRequest() *RequestBuilder {
	return acquireRequestBuilder(c)
}

// R acquires a pooled [RequestBuilder] bound to the shared [DefaultClient].
//
// # Example
//
//	var profile Profile
//	resp, err := aoni.R().
//	    SetBearerToken(token).
//	    SetResult(&profile).
//	    Get("https://api.example.com/me")
func R() *RequestBuilder {
	return acquireRequestBuilder(DefaultClient)
}

// NewRequest returns a pooled [RequestBuilder] bound to the shared [DefaultClient] (alias for [aoni.R]).
func NewRequest() *RequestBuilder {
	return acquireRequestBuilder(DefaultClient)
}

// Reset clears all request builder fields to prepare the instance for pool recycling.
func (r *RequestBuilder) Reset() {
	r.client = nil
	r.ctx = nil
	r.result = nil
	r.authMod = RequestModifier{}
	r.signer = nil
	r.sink = nil
	r.validators = r.validators[:0]
	r.pluginErr = nil
	r.outputFile = ""
	r.outputDirectory = ""
	r.appliedMods = r.appliedMods[:0]
	r.multipartFields = r.multipartFields[:0]
	r.expectedStatuses = r.expectedStatuses[:0]
	r.retryOverride = nil

	clear(r.pathParams)
}

// Release resets the request builder and returns it to the free-list pool.
func (r *RequestBuilder) Release() {
	r.consumed = true
	r.Reset()
	requestBuilderPool.Put(r)
}

// SetContext associates execution context with the request.
func (r *RequestBuilder) SetContext(ctx context.Context) *RequestBuilder {
	r.ctx = ctx
	return r
}

// SetHeader sets an HTTP header key-value pair.
func (r *RequestBuilder) SetHeader(header, value string) *RequestBuilder {
	r.appliedMods = append(r.appliedMods, mod.WithHeader(header, value))
	return r
}

// SetHeaders bulk-sets HTTP headers from a map.
func (r *RequestBuilder) SetHeaders(headers map[string]string) *RequestBuilder {
	r.appliedMods = append(r.appliedMods, mod.WithHeaders(headers))
	return r
}

// SetQueryParam appends a URL query parameter key-value pair.
func (r *RequestBuilder) SetQueryParam(param, value string) *RequestBuilder {
	r.appliedMods = append(r.appliedMods, mod.WithQuery(param, value))
	return r
}

// SetQueryParams bulk-sets URL query parameters from a map.
func (r *RequestBuilder) SetQueryParams(params map[string]string) *RequestBuilder {
	r.appliedMods = append(r.appliedMods, mod.WithQueryParams(params))
	return r
}

// SetQueryStruct assigns a structure to be marshaled into query parameters.
func (r *RequestBuilder) SetQueryStruct(v any) *RequestBuilder {
	r.appliedMods = append(r.appliedMods, mod.WithQuery(v))
	return r
}

// SetPathParam sets a URL path template parameter (e.g. /users/{id}).
func (r *RequestBuilder) SetPathParam(param, value string) *RequestBuilder {
	if r.pathParams == nil {
		r.pathParams = make(map[string]string, 4)
	}

	r.pathParams[param] = value

	return r
}

// SetPathParams bulk-sets URL path parameters from a map.
func (r *RequestBuilder) SetPathParams(params map[string]string) *RequestBuilder {
	if r.pathParams == nil {
		r.pathParams = make(map[string]string, len(params))
	}

	maps.Copy(r.pathParams, params)

	return r
}

// SetFormField adds a form key-value field for multipart/form-data requests.
func (r *RequestBuilder) SetFormField(key, value string) *RequestBuilder {
	r.multipartFields = append(r.multipartFields, mod.MultipartField{
		Name:  key,
		Value: value,
	})

	return r
}

// SetFormFile attaches a stream reader as a file part in multipart/form-data requests.
func (r *RequestBuilder) SetFormFile(fieldname string, reader io.Reader) *RequestBuilder {
	r.multipartFields = append(r.multipartFields, mod.MultipartField{
		Name:     fieldname,
		Filename: fieldname,
		Reader:   reader,
	})

	return r
}

// SetProxy routes this request through a target proxy URL.
func (r *RequestBuilder) SetProxy(proxyURL string) *RequestBuilder {
	r.appliedMods = append(r.appliedMods, mod.WithProxyOverride(proxyURL))
	return r
}

// RetryPolicyProvider represents any type capable of exporting a [core.RetryOverride].
type RetryPolicyProvider interface {
	ToOverride() core.RetryOverride
}

// Retry sets the request retry policy via a [RetryPolicyProvider].
func (r *RequestBuilder) Retry(builder RetryPolicyProvider) *RequestBuilder {
	if builder != nil {
		override := builder.ToOverride()
		r.retryOverride = &override
		r.appliedMods = append(r.appliedMods, mod.WithRetryPolicy(override))
	}

	return r
}

// SetRetry configures custom retry parameters for this request attempt.
func (r *RequestBuilder) SetRetry(maxAttempts int, backoff time.Duration) *RequestBuilder {
	override := core.RetryOverride{
		MaxAttempts: maxAttempts,
		Backoff:     backoff,
	}
	r.retryOverride = &override
	r.appliedMods = append(r.appliedMods, mod.WithRetryPolicy(override))

	return r
}

// WithCodec applies request encoding and response decoding strategies defined by codec.
func (r *RequestBuilder) WithCodec(c codec.Codec, body any) *RequestBuilder {
	if c == nil {
		return r
	}

	if encMod := c.Encode(body); !encMod.IsZero() {
		r.appliedMods = append(r.appliedMods, encMod)
	}

	if decMod := c.Decode(); !decMod.IsZero() {
		r.appliedMods = append(r.appliedMods, decMod)
	}

	return r
}

// ExpectStatus asserts that the response status code matches one of the expected HTTP status codes.
func (r *RequestBuilder) ExpectStatus(codes ...int) *RequestBuilder {
	r.expectedStatuses = append(r.expectedStatuses, codes...)
	return r
}

// Use registers one or more builder modifier functions to configure the request.
func (r *RequestBuilder) Use(plugins ...func(*RequestBuilder) error) *RequestBuilder {
	for _, p := range plugins {
		if p == nil {
			continue
		}

		if err := p(r); err != nil && r.pluginErr == nil {
			r.pluginErr = err
		}
	}

	return r
}

// SetSigner sets a pluggable function for cryptographic request signing.
func (r *RequestBuilder) SetSigner(signer func(*http.Request) error) *RequestBuilder {
	r.signer = signer
	return r
}

// SetSink sets a pluggable callback function for consuming the response payload.
func (r *RequestBuilder) SetSink(sink func(*http.Response) error) *RequestBuilder {
	r.sink = sink
	return r
}

// AddValidator registers response validators to inspect HTTP responses before decoding.
//
//nolint:bodyclose // Validators inspect responses without taking ownership of response lifecycle.
func (r *RequestBuilder) AddValidator(validators ...func(*http.Response) error) *RequestBuilder {
	for _, v := range validators {
		if v != nil {
			r.validators = append(r.validators, v)
		}
	}

	return r
}

// SetAuth assigns an authentication modifier to the request, replacing any previous auth configuration.
func (r *RequestBuilder) SetAuth(auth RequestModifier) *RequestBuilder {
	r.authMod = auth
	return r
}

// SetBearerToken sets the Authorization header to "Bearer <token>", replacing any previous auth configuration.
func (r *RequestBuilder) SetBearerToken(token string) *RequestBuilder {
	return r.SetAuth(mod.WithBearer(token))
}

// SetBasicAuth sets the Authorization header to "Basic <base64>", replacing any previous auth configuration.
func (r *RequestBuilder) SetBasicAuth(username, password string) *RequestBuilder {
	return r.SetAuth(mod.WithBasicAuth(username, password))
}

// SetOutputFromHeader instructs the request to stream the downloaded file to targetDir using Content-Disposition filenames.
func (r *RequestBuilder) SetOutputFromHeader(targetDir string) *RequestBuilder {
	r.outputDirectory = targetDir
	return r
}

// SetOutputDirectory sets the target directory for streamed file downloads.
func (r *RequestBuilder) SetOutputDirectory(targetDir string) *RequestBuilder {
	return r.SetOutputFromHeader(targetDir)
}

// SetBody sets the payload body to be serialized into the request.
//
// Automatically detects the appropriate serialization:
//   - Struct / Map / Slice -> JSON
//   - [proto.Message] -> Protobuf
//   - [url.Values] -> Form urlencoded
//   - `[]byte` / `string` / `io.Reader` -> Raw stream
func (r *RequestBuilder) SetBody(body any) *RequestBuilder {
	r.appliedMods = append(r.appliedMods, mod.WithSmartBody(body))
	return r
}

// SetXMLBody serializes payload into XML request bytes and sets 'Content-Type: application/xml'.
func (r *RequestBuilder) SetXMLBody(body any) *RequestBuilder {
	r.appliedMods = append(r.appliedMods, mod.WithXMLBody(body))
	return r
}

// SetYAMLBody serializes payload into YAML request bytes and sets 'Content-Type: application/yaml'.
func (r *RequestBuilder) SetYAMLBody(body any) *RequestBuilder {
	r.appliedMods = append(r.appliedMods, mod.WithYAMLBody(body))
	return r
}

// SetProtoBody serializes a [proto.Message] into binary request bytes.
func (r *RequestBuilder) SetProtoBody(msg proto.Message) *RequestBuilder {
	r.appliedMods = append(r.appliedMods, mod.WithProtoBody(msg))
	return r
}

// SetGRPCWebBody serializes a [proto.Message] into a gRPC-Web framed request payload.
func (r *RequestBuilder) SetGRPCWebBody(msg proto.Message) *RequestBuilder {
	r.appliedMods = append(r.appliedMods, mod.WithGRPCWebBody(msg))
	return r
}

// SetResult sets the target structure pointer for unmarshaling 2xx response bodies.
func (r *RequestBuilder) SetResult(result any) *RequestBuilder {
	r.result = result
	return r
}

// SetXMLResult configures response target unmarshaling via [decode.XMLDecoder].
func (r *RequestBuilder) SetXMLResult(result any) *RequestBuilder {
	r.result = result
	r.appliedMods = append(r.appliedMods, decode.WithXML())
	return r
}

// SetYAMLResult configures response target unmarshaling via [decode.YAMLDecoder].
func (r *RequestBuilder) SetYAMLResult(result any) *RequestBuilder {
	r.result = result
	r.appliedMods = append(r.appliedMods, decode.WithYAML())
	return r
}

// SetProtoResult configures response target unmarshaling via [decode.ProtoDecoder].
func (r *RequestBuilder) SetProtoResult(result any) *RequestBuilder {
	r.result = result
	r.appliedMods = append(r.appliedMods, decode.WithProto())
	return r
}

// SetGRPCWebResult configures response target unmarshaling via [decode.GRPCWebDecoder].
func (r *RequestBuilder) SetGRPCWebResult(result any) *RequestBuilder {
	r.result = result
	r.appliedMods = append(r.appliedMods, decode.WithGRPCWeb())
	return r
}

// SetError sets the target structure pointer for unmarshaling non-2xx error response bodies.
func (r *RequestBuilder) SetError(errResult any) *RequestBuilder {
	r.appliedMods = append(r.appliedMods, mod.WithErrorModel(errResult))
	return r
}

// SetOutput sets the local disk file path where the response body stream is saved directly.
func (r *RequestBuilder) SetOutput(filePath string) *RequestBuilder {
	r.outputFile = filePath
	return r
}

// SetOutputFile is an alias for [RequestBuilder.SetOutput].
func (r *RequestBuilder) SetOutputFile(filePath string) *RequestBuilder {
	return r.SetOutput(filePath)
}

// SetDownloadProgress registers a [ProgressFunc] callback monitoring response stream reads.
func (r *RequestBuilder) SetDownloadProgress(progress ProgressFunc) *RequestBuilder {
	r.appliedMods = append(r.appliedMods, mod.WithDownloadProgress(progress))
	return r
}

// SetUploadProgress registers a [ProgressFunc] callback monitoring request body uploads.
func (r *RequestBuilder) SetUploadProgress(progress ProgressFunc) *RequestBuilder {
	r.appliedMods = append(r.appliedMods, mod.WithUploadProgress(progress))
	return r
}

// SetTrace associates a [telemetry.TraceInfo] container to capture fine-grained network timings.
func (r *RequestBuilder) SetTrace(info *telemetry.TraceInfo) *RequestBuilder {
	r.appliedMods = append(r.appliedMods, mod.WithTrace(info))
	return r
}

// SetCorrelationID assigns an end-to-end tracing Correlation ID to the request.
func (r *RequestBuilder) SetCorrelationID(id string) *RequestBuilder {
	r.appliedMods = append(r.appliedMods, mod.WithCorrelationID(id))
	return r
}

// SetForceContentType forces response parsing using the specified MIME type.
func (r *RequestBuilder) SetForceContentType(mime string) *RequestBuilder {
	r.appliedMods = append(r.appliedMods, mod.WithForceContentType(mime))
	return r
}

// SetForceJSON forces response parsing as JSON regardless of Content-Type headers.
func (r *RequestBuilder) SetForceJSON() *RequestBuilder {
	return r.SetForceContentType(header.MIMEApplicationJSON)
}

// SetLabel attaches a human-readable metric or route label.
func (r *RequestBuilder) SetLabel(label string) *RequestBuilder {
	r.appliedMods = append(r.appliedMods, mod.WithLabel(label))
	return r
}

// Apply injects custom [RequestModifier] options into the builder chain.
func (r *RequestBuilder) Apply(mods ...RequestModifier) *RequestBuilder {
	r.appliedMods = append(r.appliedMods, mods...)
	return r
}

// SetTimeout sets a per-request context deadline timeout.
func (r *RequestBuilder) SetTimeout(timeout time.Duration) *RequestBuilder {
	r.appliedMods = append(r.appliedMods, mod.WithTimeout(timeout))
	return r
}

// Download is a convenience method executing a GET request and streaming response bytes to filePath.
func (r *RequestBuilder) Download(url, filePath string) (*http.Response, error) {
	return r.SetOutput(filePath).Get(url)
}

// Get executes a GET request using the builder configuration.
func (r *RequestBuilder) Get(path string) (*http.Response, error) {
	return r.Execute(http.MethodGet, path)
}

// Post executes a POST request using the builder configuration.
func (r *RequestBuilder) Post(path string) (*http.Response, error) {
	return r.Execute(http.MethodPost, path)
}

// Put executes a PUT request using the builder configuration.
func (r *RequestBuilder) Put(path string) (*http.Response, error) {
	return r.Execute(http.MethodPut, path)
}

// Patch executes a PATCH request using the builder configuration.
func (r *RequestBuilder) Patch(path string) (*http.Response, error) {
	return r.Execute(http.MethodPatch, path)
}

// Delete executes a DELETE request using the builder configuration.
func (r *RequestBuilder) Delete(path string) (*http.Response, error) {
	return r.Execute(http.MethodDelete, path)
}

// Head executes a HEAD request using the builder configuration.
func (r *RequestBuilder) Head(path string) (*http.Response, error) {
	return r.Execute(http.MethodHead, path)
}

// Options executes an OPTIONS request using the builder configuration.
func (r *RequestBuilder) Options(path string) (*http.Response, error) {
	return r.Execute(http.MethodOptions, path)
}

// Trace executes a TRACE request using the builder configuration.
func (r *RequestBuilder) Trace(path string) (*http.Response, error) {
	return r.Execute(http.MethodTrace, path)
}

// Connect executes a CONNECT request using the builder configuration.
func (r *RequestBuilder) Connect(path string) (*http.Response, error) {
	return r.Execute(http.MethodConnect, path)
}

// Execute compiles builder configurations into modifiers and executes the request.
//
// Postconditions:
//   - Automatically releases the request instance back to the pool upon completion.
func (r *RequestBuilder) Execute(method, path string) (*http.Response, error) {
	if r.consumed {
		return nil, ErrBuilderConsumed
	}

	r.consumed = true

	client := generic.Coalesce[HTTPRequester](r.client, DefaultClient)

	defer r.Release()

	if r.pluginErr != nil {
		return nil, r.pluginErr
	}

	if len(r.pathParams) > 0 {
		path = urlkit.BuildPath(path, r.pathParams, nil)
	}

	ctx := generic.Coalesce(r.ctx, context.Background())

	mods := r.appliedMods

	if !r.authMod.IsZero() {
		mods = append(mods, r.authMod)
	}

	if len(r.multipartFields) > 0 {
		mods = append(mods, mod.WithMultipartFields(r.multipartFields))
	}

	if r.signer != nil {
		signer := r.signer

		mods = append(mods, RequestModifier{
			Kind: core.ModCustom,
			Fn: func(req core.Request) {
				if httpReq := req.HTTPRequest(); httpReq != nil {
					_ = signer(httpReq)
				}
			},
		})
	}

	if r.outputFile != "" || r.outputDirectory != "" {
		return r.executeDownload(ctx, client, method, path, mods, r.outputFile)
	}

	resp, err := client.Request(ctx, method, path, mods...)
	if err != nil {
		return nil, err
	}

	for i := range r.validators {
		if err := r.validators[i](resp); err != nil {
			return resp, err
		}
	}

	if err := r.checkExpectedStatus(resp, path); err != nil {
		return resp, err
	}

	if r.sink != nil {
		if err := r.sink(resp); err != nil {
			return resp, err
		}

		return resp, nil
	}

	if r.result != nil {
		if err := HandleResponse(resp, r.result, client); err != nil {
			return resp, err
		}

		return resp, nil
	}

	return resp, nil
}

// checkExpectedStatus verifies that the response status code matches expectations configured via [RequestBuilder.ExpectStatus].
func (r *RequestBuilder) checkExpectedStatus(resp *http.Response, finalPath string) error {
	if len(r.expectedStatuses) == 0 || resp == nil {
		return nil
	}

	if slices.Contains(r.expectedStatuses, resp.StatusCode) {
		return nil
	}

	return &Error{
		Op:   "expect_status",
		Path: finalPath,
		Code: resp.StatusCode,
		Err:  ErrUnexpectedStatus,
	}
}

// executeDownload manages multi-part resumable file downloads with exponential backoff retries.
func (r *RequestBuilder) executeDownload(
	ctx context.Context,
	client HTTPRequester,
	method, path string,
	mods []RequestModifier,
	outputFile string,
) (*http.Response, error) {
	d := download.Downloader{
		OutputFile:      outputFile,
		OutputDirectory: r.outputDirectory,
		RetryOverride:   r.retryOverride,
	}

	return d.Execute(ctx, client, method, path, mods)
}

// FetchTo executes the request and unmarshals the response into T.
func (r *RequestBuilder) FetchTo[T any](method, path string) (T, *http.Response, error) {
	var target T

	resp, err := r.SetResult(&target).Execute(method, path)

	return target, resp, err
}

// GetTo executes a GET request and unmarshals the response into T.
func (r *RequestBuilder) GetTo[T any](path string) (T, *http.Response, error) {
	return r.FetchTo[T](http.MethodGet, path)
}

// PostTo executes a POST request with payload and unmarshals the response into T.
func (r *RequestBuilder) PostTo[T any](path string, body ...any) (T, *http.Response, error) {
	if len(body) > 0 {
		r.SetBody(body[0])
	}

	return r.FetchTo[T](http.MethodPost, path)
}

// PutTo executes a PUT request with payload and unmarshals the response into T.
//
// See [RequestBuilder.PostTo] for payload handling details.
func (r *RequestBuilder) PutTo[T any](path string, body ...any) (T, *http.Response, error) {
	if len(body) > 0 {
		r.SetBody(body[0])
	}

	return r.FetchTo[T](http.MethodPut, path)
}

// PatchTo executes a PATCH request with payload and unmarshals the response into T.
//
// See [RequestBuilder.PostTo] for payload handling details.
func (r *RequestBuilder) PatchTo[T any](path string, body ...any) (T, *http.Response, error) {
	if len(body) > 0 {
		r.SetBody(body[0])
	}

	return r.FetchTo[T](http.MethodPatch, path)
}

// DeleteTo executes a DELETE request and unmarshals the response into T.
//
// See [RequestBuilder.GetTo] for unmarshaling details.
func (r *RequestBuilder) DeleteTo[T any](path string) (T, *http.Response, error) {
	return r.FetchTo[T](http.MethodDelete, path)
}

// ExecuteTo executes the request with method and path, unmarshaling the response into T.
func (r *RequestBuilder) ExecuteTo[T any](method, path string) (T, *http.Response, error) {
	return r.FetchTo[T](method, path)
}

// ExecuteResult executes the request and returns a [generic.Result] wrapping the unmarshaled response or error.
func (r *RequestBuilder) ExecuteResult[T any](method, path string) (generic.Result[T], *http.Response) {
	val, resp, err := r.FetchTo[T](method, path)
	if err != nil {
		return generic.Failure[T](err), resp
	}

	return generic.Success(val), resp
}

// FetchResult executes a request and returns a [generic.Result] wrapping the unmarshaled response or error.
// Alias for [RequestBuilder.ExecuteResult].
func (r *RequestBuilder) FetchResult[T any](method, path string) (generic.Result[T], *http.Response) {
	return r.ExecuteResult[T](method, path)
}

// Result executes the request and returns a pure [generic.Result], automatically draining and closing the response body.
func (r *RequestBuilder) Result[T any](method, path string) generic.Result[T] {
	//nolint:bodyclose // Body is closed by response handler or DrainAndClose.
	val, resp, err := r.FetchTo[T](method, path)
	if resp != nil && resp.Body != nil {
		DrainAndClose(resp)
	}

	if err != nil {
		return generic.Failure[T](err)
	}

	return generic.Success(val)
}

// ExecuteEither executes the request and unmarshals 2xx responses into type R (Right)
// and 4xx/5xx responses into type L (Left).
func (r *RequestBuilder) ExecuteEither[L, R any](method, path string) (generic.Either[L, R], *http.Response, error) {
	var (
		right R
		left  L
	)

	r.SetResult(&right).SetError(&left)

	resp, err := r.Execute(method, path)
	if err != nil {
		if resp != nil && resp.StatusCode >= http.StatusBadRequest {
			return generic.Left[L, R](left), resp, nil
		}

		return generic.Either[L, R]{}, resp, err
	}

	return generic.Right[L](right), resp, nil
}

// FetchEither executes a request and unmarshals 2xx responses into type R (Right)
// and 4xx/5xx responses into type L (Left).
func (r *RequestBuilder) FetchEither[L, R any](method, path string) (generic.Either[L, R], *http.Response, error) {
	return r.ExecuteEither[L, R](method, path)
}

// GetEither executes a GET request and unmarshals 2xx responses into type R (Right)
// and 4xx/5xx responses into type L (Left).
func (r *RequestBuilder) GetEither[L, R any](path string) (generic.Either[L, R], *http.Response, error) {
	return r.FetchEither[L, R](http.MethodGet, path)
}

// PostEither executes a POST request with optional payload and unmarshals 2xx responses into type R (Right)
// and 4xx/5xx responses into type L (Left).
func (r *RequestBuilder) PostEither[L, R any](path string, body ...any) (generic.Either[L, R], *http.Response, error) {
	if len(body) > 0 {
		r.SetBody(body[0])
	}

	return r.FetchEither[L, R](http.MethodPost, path)
}
