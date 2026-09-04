// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	stdpath "path"
	"path/filepath"
	"slices"
	"time"

	"github.com/lemon4ksan/foundation/borrow"
	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/iokit"
	"github.com/lemon4ksan/foundation/net/http/header"
	"github.com/lemon4ksan/foundation/net/urlkit"
	"github.com/lemon4ksan/foundation/silicon/pool"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni/codec"
	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/netutil/sanitize"
	"github.com/lemon4ksan/aoni/telemetry"
)

var (
	// ErrUnexpectedStatus is returned when the response status code does not match expected codes.
	ErrUnexpectedStatus = errors.New("aoni: unexpected HTTP status code")

	// ErrDownloadFailed indicates a download request failure due to an HTTP error status code.
	ErrDownloadFailed = errors.New("aoni: download failed")

	// ErrRangeNotSatisfiable is returned when the requested byte range exceeds remote file size (HTTP 416).
	ErrRangeNotSatisfiable = errors.New("aoni: requested byte range not satisfiable by server")
)

type typedRequestPool struct {
	storage *pool.PerPStorage[*RequestBuilder]
}

func newTypedRequestPool() *typedRequestPool {
	return &typedRequestPool{
		storage: pool.NewPerPStorage(func() *RequestBuilder {
			return &RequestBuilder{
				appliedMods:      make([]RequestModifier, 0, 8),
				expectedStatuses: make([]int, 0, 4),
				headerEntries:    make([]headerEntry, 0, 8),
				queryEntries:     make([]queryParamEntry, 0, 8),
				pathParams:       make(map[string]string, 4),
				formFields:       make(map[string]string, 4),
			}
		}),
	}
}

// Get retrieves a pooled [RequestBuilder] instance bound to any engine or client.
func (p *typedRequestPool) Get(doer HTTPRequester) *RequestBuilder {
	if doer == nil {
		doer = DefaultClient
	}

	r := p.storage.Get()
	r.client = doer

	return r
}

// Put recycles a [RequestBuilder] instance back to the core-pinned storage after resetting fields.
func (p *typedRequestPool) Put(r *RequestBuilder) {
	if r == nil {
		return
	}

	r.Reset()
	p.storage.Put(r)
}

var requestBuilderPool = newTypedRequestPool()

func acquireRequestBuilder(doer HTTPRequester) *RequestBuilder {
	return requestBuilderPool.Get(doer)
}

type headerEntry struct {
	key string
	val string
}

type queryParamEntry struct {
	key string
	val string
}

// basicAuthCredentials stores HTTP Basic Authentication credentials.
type basicAuthCredentials struct {
	username string
	password string
}

// digestAuthCredentials stores RFC 7616 Digest Access Authentication credentials.
type digestAuthCredentials struct {
	username string
	password string
}

// RequestBuilder is a pooled request builder offering a chainable, fluent configuration API.
//
// Thread Safety:
// RequestBuilder instances are NOT safe for concurrent use across multiple goroutines.
// They are intended for single-goroutine linear construction and execution before being returned to the pool.
type RequestBuilder struct {
	ctx              context.Context
	body             any
	protoBody        proto.Message
	grpcWebBody      proto.Message
	result           any
	resultError      any
	queryStruct      any
	bearerToken      string
	outputFile       string
	outputDirectory  string
	correlationID    string
	forceContentType string
	label            string
	proxyOverride    string

	client            HTTPRequester
	basicAuth         *basicAuthCredentials
	digestAuth        *digestAuthCredentials
	headers           http.Header
	headerEntries     []headerEntry
	queryParams       url.Values
	queryEntries      []queryParamEntry
	pathParams        map[string]string
	formFields        map[string]string
	formFiles         map[string]io.Reader
	expectedStatuses  []int
	downloadProgress  ProgressFunc
	uploadProgress    ProgressFunc
	traceInfo         *telemetry.TraceInfo
	appliedMods       []RequestModifier
	timeout           time.Duration
	retryOverride     *core.RetryOverride
	xmlBody           any
	yamlBody          any
	useProtoDecoder   bool
	useGRPCWebDecoder bool
	useXMLDecoder     bool
	useYAMLDecoder    bool
}

// R acquires a pooled, zero-allocation fluent [RequestBuilder] bound to this [Client] instance.
//
// Automatically recycled back to the core-pinned free-list upon request execution or explicit [RequestBuilder.Release].
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

// R acquires a pooled, zero-allocation fluent [RequestBuilder] bound to the shared [DefaultClient].
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
	r.body = nil
	r.protoBody = nil
	r.grpcWebBody = nil
	r.xmlBody = nil
	r.yamlBody = nil
	r.result = nil
	r.resultError = nil
	r.queryStruct = nil
	r.bearerToken = ""
	r.basicAuth = nil
	r.digestAuth = nil
	r.outputFile = ""
	r.outputDirectory = ""
	r.correlationID = ""
	r.forceContentType = ""
	r.label = ""
	r.proxyOverride = ""
	r.downloadProgress = nil
	r.uploadProgress = nil
	r.traceInfo = nil
	r.appliedMods = r.appliedMods[:0]
	r.expectedStatuses = r.expectedStatuses[:0]
	r.headerEntries = r.headerEntries[:0]
	r.queryEntries = r.queryEntries[:0]
	r.timeout = 0
	r.retryOverride = nil
	r.useProtoDecoder = false
	r.useGRPCWebDecoder = false
	r.useXMLDecoder = false
	r.useYAMLDecoder = false

	if r.headers != nil {
		releaseHeader(r.headers)
		r.headers = nil
	}

	clear(r.queryParams)
	clear(r.pathParams)
	clear(r.formFields)
	clear(r.formFiles)
}

var headerStorage = pool.NewPerPStorage(func() http.Header {
	return make(http.Header, 8)
})

func acquireHeader() http.Header {
	return headerStorage.Get()
}

func releaseHeader(h http.Header) {
	if h == nil {
		return
	}

	clear(h)
	headerStorage.Put(h)
}

// Release resets the request builder and returns it to the free-list pool.
func (r *RequestBuilder) Release() {
	if r == nil {
		return
	}

	r.Reset()
	requestBuilderPool.Put(r)
}

// Header returns or acquires the internal [http.Header] map.
func (r *RequestBuilder) Header() http.Header {
	if r.headers == nil {
		r.headers = acquireHeader()
		for i := range r.headerEntries {
			r.headers.Add(r.headerEntries[i].key, r.headerEntries[i].val)
		}

		r.headerEntries = r.headerEntries[:0]
	}

	return r.headers
}

// SetContext associates execution context with the request.
func (r *RequestBuilder) SetContext(ctx context.Context) *RequestBuilder {
	r.ctx = ctx
	return r
}

// SetHeader sets an HTTP header key-value pair.
func (r *RequestBuilder) SetHeader(header, value string) *RequestBuilder {
	if r.headers != nil {
		r.headers.Set(header, value)
		return r
	}

	r.headerEntries = append(r.headerEntries, headerEntry{key: header, val: value})

	return r
}

// SetHeaders bulk-sets HTTP headers from a map.
func (r *RequestBuilder) SetHeaders(headers map[string]string) *RequestBuilder {
	if r.headers != nil {
		for k, v := range headers {
			r.headers.Set(k, v)
		}

		return r
	}

	for k, v := range headers {
		r.headerEntries = append(r.headerEntries, headerEntry{key: k, val: v})
	}

	return r
}

// SetQueryParam appends a URL query parameter key-value pair.
func (r *RequestBuilder) SetQueryParam(param, value string) *RequestBuilder {
	if r.queryParams != nil {
		r.queryParams.Add(param, value)
		return r
	}

	r.queryEntries = append(r.queryEntries, queryParamEntry{key: param, val: value})

	return r
}

// SetQueryParams bulk-sets URL query parameters from a map.
func (r *RequestBuilder) SetQueryParams(params map[string]string) *RequestBuilder {
	if r.queryParams != nil {
		for k, v := range params {
			r.queryParams.Add(k, v)
		}

		return r
	}

	for k, v := range params {
		r.queryEntries = append(r.queryEntries, queryParamEntry{key: k, val: v})
	}

	return r
}

// ExpectStatus asserts that the response status code matches one of the expected HTTP status codes.
func (r *RequestBuilder) ExpectStatus(codes ...int) *RequestBuilder {
	r.expectedStatuses = append(r.expectedStatuses, codes...)
	return r
}

// SetFormField adds a form key-value field for multipart/form-data requests.
func (r *RequestBuilder) SetFormField(key, value string) *RequestBuilder {
	if r.formFields == nil {
		r.formFields = make(map[string]string, 4)
	}

	r.formFields[key] = value

	return r
}

// SetFormFile attaches a stream reader as a file part in multipart/form-data requests.
func (r *RequestBuilder) SetFormFile(fieldname string, reader io.Reader) *RequestBuilder {
	if r.formFiles == nil {
		r.formFiles = make(map[string]io.Reader, 2)
	}

	r.formFiles[fieldname] = reader

	return r
}

// SetProxy routes this request through a target proxy URL.
func (r *RequestBuilder) SetProxy(proxyURL string) *RequestBuilder {
	r.proxyOverride = proxyURL
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
	}

	return r
}

// SetRetry configures custom retry parameters for this request attempt.
func (r *RequestBuilder) SetRetry(maxAttempts int, backoff time.Duration) *RequestBuilder {
	r.retryOverride = &core.RetryOverride{
		MaxAttempts: maxAttempts,
		Backoff:     backoff,
	}

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

// SetQueryStruct assigns a structure to be marshaled into query parameters.
func (r *RequestBuilder) SetQueryStruct(v any) *RequestBuilder {
	r.queryStruct = v
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

// SetBearerToken sets an "Authorization: Bearer <token>" header.
func (r *RequestBuilder) SetBearerToken(token string) *RequestBuilder {
	r.bearerToken = token
	return r
}

// SetBasicAuth sets HTTP Basic Authentication credentials.
func (r *RequestBuilder) SetBasicAuth(username, password string) *RequestBuilder {
	r.basicAuth = &basicAuthCredentials{username: username, password: password}
	return r
}

// SetDigestAuth configures RFC 7616 Digest Access Authentication credentials.
func (r *RequestBuilder) SetDigestAuth(username, password string) *RequestBuilder {
	r.digestAuth = &digestAuthCredentials{username: username, password: password}
	return r
}

// SetPKCE adds PKCE code_challenge and code_challenge_method parameters for OAuth 2.0 requests (RFC 7636 / RFC 9700).
func (r *RequestBuilder) SetPKCE(verifier string, method ...string) *RequestBuilder {
	r.appliedMods = append(r.appliedMods, mod.WithPKCE(verifier, method...))
	return r
}

// SetPKCEVerifier adds the PKCE code_verifier parameter for OAuth 2.0 token requests (RFC 7636 / RFC 9700).
func (r *RequestBuilder) SetPKCEVerifier(verifier string) *RequestBuilder {
	r.appliedMods = append(r.appliedMods, mod.WithPKCEVerifier(verifier))
	return r
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
	r.body = body
	return r
}

// SetXMLBody serializes payload into XML request bytes and sets 'Content-Type: application/xml'.
func (r *RequestBuilder) SetXMLBody(body any) *RequestBuilder {
	r.xmlBody = body
	return r
}

// SetYAMLBody serializes payload into YAML request bytes and sets 'Content-Type: application/yaml'.
func (r *RequestBuilder) SetYAMLBody(body any) *RequestBuilder {
	r.yamlBody = body
	return r
}

// SetProtoBody serializes a [proto.Message] into binary request bytes.
func (r *RequestBuilder) SetProtoBody(msg proto.Message) *RequestBuilder {
	r.protoBody = msg
	return r
}

// SetGRPCWebBody serializes a [proto.Message] into a gRPC-Web framed request payload.
func (r *RequestBuilder) SetGRPCWebBody(msg proto.Message) *RequestBuilder {
	r.grpcWebBody = msg
	return r
}

// SetResult sets the target structure pointer for unmarshaling 2xx response bodies.
//
// # Example
//
//	var user User
//	resp, err := client.R().SetResult(&user).Get("/users/1")
func (r *RequestBuilder) SetResult(result any) *RequestBuilder {
	r.result = result
	return r
}

// SetXMLResult configures response target unmarshaling via [decode.XMLDecoder].
func (r *RequestBuilder) SetXMLResult(result any) *RequestBuilder {
	r.result = result
	r.useXMLDecoder = true
	return r
}

// SetYAMLResult configures response target unmarshaling via [decode.YAMLDecoder].
func (r *RequestBuilder) SetYAMLResult(result any) *RequestBuilder {
	r.result = result
	r.useYAMLDecoder = true
	return r
}

// SetProtoResult configures response target unmarshaling via [decode.ProtoDecoder].
func (r *RequestBuilder) SetProtoResult(result any) *RequestBuilder {
	r.result = result
	r.useProtoDecoder = true
	return r
}

// SetGRPCWebResult configures response target unmarshaling via [decode.GRPCWebDecoder].
func (r *RequestBuilder) SetGRPCWebResult(result any) *RequestBuilder {
	r.result = result
	r.useGRPCWebDecoder = true
	return r
}

// SetError sets the target structure pointer for unmarshaling non-2xx error response bodies.
//
// # Example
//
//	var errResp ErrorResponse
//	resp, err := client.R().
//	    SetResult(&user).
//	    SetError(&errResp).
//	    Post("/users", req)
func (r *RequestBuilder) SetError(errResult any) *RequestBuilder {
	r.resultError = errResult
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
	r.downloadProgress = progress
	return r
}

// SetUploadProgress registers a [ProgressFunc] callback monitoring request body uploads.
func (r *RequestBuilder) SetUploadProgress(progress ProgressFunc) *RequestBuilder {
	r.uploadProgress = progress
	return r
}

// SetTrace associates a [telemetry.TraceInfo] container to capture fine-grained network timings.
func (r *RequestBuilder) SetTrace(info *telemetry.TraceInfo) *RequestBuilder {
	r.traceInfo = info
	return r
}

// SetCorrelationID assigns an end-to-end tracing Correlation ID to the request.
func (r *RequestBuilder) SetCorrelationID(id string) *RequestBuilder {
	r.correlationID = id
	return r
}

// SetForceContentType forces response parsing using the specified MIME type.
func (r *RequestBuilder) SetForceContentType(mime string) *RequestBuilder {
	r.forceContentType = mime
	return r
}

// SetForceJSON forces response parsing as JSON regardless of Content-Type headers.
func (r *RequestBuilder) SetForceJSON() *RequestBuilder {
	return r.SetForceContentType(header.MIMEApplicationJSON)
}

// SetLabel attaches a human-readable metric or route label.
func (r *RequestBuilder) SetLabel(label string) *RequestBuilder {
	r.label = label
	return r
}

// Apply injects custom [RequestModifier] options into the builder chain.
func (r *RequestBuilder) Apply(mods ...RequestModifier) *RequestBuilder {
	r.appliedMods = append(r.appliedMods, mods...)
	return r
}

// SetTimeout sets a per-request context deadline timeout.
func (r *RequestBuilder) SetTimeout(timeout time.Duration) *RequestBuilder {
	r.timeout = timeout
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
	client := r.client
	if client == nil {
		client = DefaultClient
	}

	resultTarget := r.result
	outputFile := r.outputFile

	defer r.Release()

	finalPath := path
	if len(r.pathParams) > 0 {
		finalPath = urlkit.BuildPath(path, r.pathParams, nil)
	}

	ctx := r.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	if r.digestAuth != nil {
		client = r.applyDigestAuth(client)
	}

	var stackBuf [stackModCapacity]RequestModifier

	mods := r.buildModifiers(&stackBuf)

	if outputFile != "" || r.outputDirectory != "" {
		return r.executeDownload(ctx, client, method, finalPath, mods, outputFile)
	}

	if resultTarget != nil {
		resp, err := client.Request(ctx, method, finalPath, mods...)
		if err != nil {
			return nil, err
		}

		if err := r.checkExpectedStatus(resp, finalPath); err != nil {
			return resp, err
		}

		if err := HandleResponse(resp, resultTarget, client); err != nil {
			return resp, err
		}

		return resp, nil
	}

	resp, err := client.Request(ctx, method, finalPath, mods...)
	if err != nil {
		return resp, err
	}

	if err := r.checkExpectedStatus(resp, finalPath); err != nil {
		return resp, err
	}

	return resp, nil
}

//go:noinline
func (r *RequestBuilder) applyDigestAuth(client HTTPRequester) HTTPRequester {
	if c, ok := client.(*Client); ok {
		return c.With(func(cfg *Config) {
			cfg.Engine.DigestAuth = &DigestAuthConfig{
				Username: r.digestAuth.username,
				Password: r.digestAuth.password,
			}
		})
	}

	return client
}

// checkExpectedStatus verifies that the response status code matches expectations configured via [RequestBuilder.ExpectStatus].
func (r *RequestBuilder) checkExpectedStatus(resp *http.Response, finalPath string) error {
	if len(r.expectedStatuses) == 0 || resp == nil {
		return nil
	}

	if slices.Contains(r.expectedStatuses, resp.StatusCode) {
		return nil
	}

	return r.unexpectedStatusError(resp, finalPath)
}

//go:noinline
func (r *RequestBuilder) unexpectedStatusError(resp *http.Response, finalPath string) error {
	return &Error{
		Op:   "expect_status",
		Path: finalPath,
		Code: resp.StatusCode,
		Err:  ErrUnexpectedStatus,
	}
}

// buildModifiers constructs value modifiers for headers, auth, body serialization, decoding, and telemetry.
func (r *RequestBuilder) buildModifiers(stackBuf *[stackModCapacity]RequestModifier) []RequestModifier {
	estimatedCap := len(r.headerEntries) + len(r.headers) + len(r.queryEntries) + len(r.appliedMods)
	if r.bearerToken != "" || r.basicAuth != nil || r.body != nil || r.protoBody != nil || r.timeout > 0 {
		estimatedCap += 4
	}

	if estimatedCap == 0 {
		return nil
	}

	var mods []RequestModifier
	if estimatedCap <= stackModCapacity && stackBuf != nil {
		mods = stackBuf[:0]
	} else {
		mods = make([]RequestModifier, 0, estimatedCap)
	}

	mods = r.appendHeaderAndAuthModifiers(mods)
	mods = r.appendQueryAndBodyModifiers(mods)
	mods = r.appendTelemetryAndMiscModifiers(mods)

	if len(r.appliedMods) > 0 {
		mods = append(mods, r.appliedMods...)
	}

	if r.timeout > 0 {
		mods = append(mods, mod.WithTimeout(r.timeout))
	}

	return mods
}

func (r *RequestBuilder) appendHeaderAndAuthModifiers(mods []RequestModifier) []RequestModifier {
	if len(r.headerEntries) > 0 {
		for i := range r.headerEntries {
			mods = append(mods, mod.WithHeader(r.headerEntries[i].key, r.headerEntries[i].val))
		}
	}

	if len(r.headers) > 0 {
		for k, v := range r.headers {
			for _, val := range v {
				mods = append(mods, mod.WithHeader(k, val))
			}
		}
	}

	if r.bearerToken != "" {
		mods = append(mods, mod.WithBearer(r.bearerToken))
	}

	if r.basicAuth != nil {
		mods = append(mods, mod.WithBasicAuth(r.basicAuth.username, r.basicAuth.password))
	}

	return mods
}

func (r *RequestBuilder) appendQueryAndBodyModifiers(mods []RequestModifier) []RequestModifier {
	if len(r.queryEntries) > 0 {
		for i := range r.queryEntries {
			mods = append(mods, mod.WithQuery(r.queryEntries[i].key, r.queryEntries[i].val))
		}
	}

	if len(r.queryParams) > 0 {
		mods = append(mods, mod.WithQuery(r.queryParams))
	}

	if r.queryStruct != nil {
		mods = append(mods, mod.WithQuery(r.queryStruct))
	}

	switch {
	case len(r.formFields) > 0 || len(r.formFiles) > 0:
		mods = append(mods, mod.WithMultipart(r.formFields, r.formFiles))
	case r.protoBody != nil:
		mods = append(mods, mod.WithProtoBody(r.protoBody))
	case r.grpcWebBody != nil:
		mods = append(mods, mod.WithGRPCWebBody(r.grpcWebBody))
	case r.xmlBody != nil:
		mods = append(mods, mod.WithXMLBody(r.xmlBody))
	case r.yamlBody != nil:
		mods = append(mods, mod.WithYAMLBody(r.yamlBody))
	case r.body != nil:
		if reader, ok := r.body.(io.Reader); ok {
			mods = append(mods, mod.WithBody(reader))
		} else {
			mods = append(mods, mod.WithJSONBody(r.body))
		}
	}

	switch {
	case r.useProtoDecoder:
		mods = append(mods, decode.WithProto())
	case r.useGRPCWebDecoder:
		mods = append(mods, decode.WithGRPCWeb())
	case r.useXMLDecoder:
		mods = append(mods, decode.WithXML())
	case r.useYAMLDecoder:
		mods = append(mods, decode.WithYAML())
	}

	return mods
}

func (r *RequestBuilder) appendTelemetryAndMiscModifiers(mods []RequestModifier) []RequestModifier {
	if r.resultError != nil {
		mods = append(mods, mod.WithErrorModel(r.resultError))
	}

	if r.downloadProgress != nil {
		mods = append(mods, mod.WithDownloadProgress(r.downloadProgress))
	}

	if r.uploadProgress != nil {
		mods = append(mods, mod.WithUploadProgress(r.uploadProgress))
	}

	if r.traceInfo != nil {
		mods = append(mods, mod.WithTrace(r.traceInfo))
	}

	if r.correlationID != "" {
		mods = append(mods, mod.WithCorrelationID(r.correlationID))
	}

	if r.forceContentType != "" {
		mods = append(mods, mod.WithForceContentType(r.forceContentType))
	}

	if r.label != "" {
		mods = append(mods, mod.WithLabel(r.label))
	}

	return mods
}

// executeDownload manages multi-part resumable file downloads with exponential backoff retries.
func (r *RequestBuilder) executeDownload(
	ctx context.Context,
	client HTTPRequester,
	method, path string,
	mods []RequestModifier,
	outputFile string,
) (*http.Response, error) {
	maxAttempts := r.resolveMaxDownloadAttempts()

	var (
		lastResp *http.Response
		lastErr  error
	)

	for attempt := range maxAttempts {
		if attempt > 0 {
			if err := sleepWithContext(ctx, calculateDownloadBackoff(attempt, r.retryOverride)); err != nil {
				if lastResp != nil && lastResp.Body != nil {
					_ = lastResp.Body.Close()
				}

				return nil, err
			}
		}

		resp, err := client.Request(ctx, method, path, mods...)
		if err != nil {
			lastErr = err
			continue
		}

		if isRetryableDownloadStatus(resp.StatusCode) {
			lastResp = resp
			lastErr = fmt.Errorf("server error: %d", resp.StatusCode)
			_ = resp.Body.Close()

			continue
		}

		if resp.StatusCode >= http.StatusBadRequest {
			return resp, &Error{
				Op:   "download",
				Path: path,
				Code: resp.StatusCode,
				Err:  ErrDownloadFailed,
			}
		}

		targetFile := resolveDownloadTarget(resp, path, outputFile, r.outputDirectory)
		if targetFile != "" {
			if err := saveResponseBodyToFile(resp, targetFile); err != nil {
				lastErr = err
				continue
			}
		}

		return resp, nil
	}

	if lastErr != nil {
		return lastResp, lastErr
	}

	return lastResp, nil
}

func (r *RequestBuilder) resolveMaxDownloadAttempts() int {
	if r.retryOverride != nil && r.retryOverride.MaxAttempts > 0 {
		return r.retryOverride.MaxAttempts
	}

	return 5
}

func calculateDownloadBackoff(attempt int, override *core.RetryOverride) time.Duration {
	if attempt <= 0 {
		return 0
	}

	if override != nil && override.Backoff > 0 {
		return override.Backoff * time.Duration(attempt)
	}

	return time.Duration(1<<attempt) * 100 * time.Millisecond
}

func isRetryableDownloadStatus(statusCode int) bool {
	return statusCode >= http.StatusInternalServerError
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func resolveDownloadTarget(resp *http.Response, targetPath, outputFile, outputDirectory string) string {
	if outputFile != "" {
		return outputFile
	}

	if outputDirectory == "" {
		return ""
	}

	var filename string
	if resp != nil && resp.Header != nil {
		if cd := resp.Header.Get(header.ContentDisposition); cd != "" {
			filename = sanitize.ExtractFilename(cd)
		}
	}

	if filename == "" {
		p := targetPath
		if u, err := url.Parse(targetPath); err == nil && u.Path != "" {
			p = u.Path
		}

		filename = stdpath.Base(p)
		if filename == "." || filename == "/" || filename == "" {
			filename = "downloaded_file"
		}
	}

	return filepath.Join(outputDirectory, filename)
}

func saveResponseBodyToFile(resp *http.Response, targetFile string) error {
	if targetFile == "" || resp == nil || resp.Body == nil {
		return nil
	}

	defer resp.Body.Close()

	if err := os.MkdirAll(filepath.Dir(targetFile), 0o750); err != nil && !os.IsExist(err) {
		return err
	}

	out, err := os.Create(targetFile)
	if err != nil {
		return err
	}
	defer out.Close()

	_, copyErr := iokit.CopyZeroAlloc(out, resp.Body)

	return copyErr
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
func (r *RequestBuilder) PutTo[T any](path string, body ...any) (T, *http.Response, error) {
	if len(body) > 0 {
		r.SetBody(body[0])
	}

	return r.FetchTo[T](http.MethodPut, path)
}

// PatchTo executes a PATCH request with payload and unmarshals the response into T.
func (r *RequestBuilder) PatchTo[T any](path string, body ...any) (T, *http.Response, error) {
	if len(body) > 0 {
		r.SetBody(body[0])
	}

	return r.FetchTo[T](http.MethodPatch, path)
}

// DeleteTo executes a DELETE request and unmarshals the response into T.
func (r *RequestBuilder) DeleteTo[T any](path string) (T, *http.Response, error) {
	return r.FetchTo[T](http.MethodDelete, path)
}

// ExecuteTo executes the request with method and path, unmarshaling the response into T.
func (r *RequestBuilder) ExecuteTo[T any](method, path string) (T, *http.Response, error) {
	return r.FetchTo[T](method, path)
}

// ExecuteResult executes the request and returns a Swift-inspired [generic.Result].
func (r *RequestBuilder) ExecuteResult[T any](method, path string) (generic.Result[T], *http.Response) {
	val, resp, err := r.FetchTo[T](method, path)
	if err != nil {
		return generic.Failure[T](err), resp
	}

	return generic.Success(val), resp
}

// FetchResult executes a request and returns a Swift-inspired [generic.Result] wrapping the unmarshaled response or error.
func (r *RequestBuilder) FetchResult[T any](method, path string) (generic.Result[T], *http.Response) {
	return r.ExecuteResult[T](method, path)
}

// FetchTo executes a request with method, path, and optional modifiers, unmarshaling the 2xx response into T.
func FetchTo[T any](
	ctx context.Context,
	c any,
	method, path string,
	mods ...RequestModifier,
) (T, *http.Response, error) {
	var (
		target T
		doer   HTTPRequester
	)
	if d, ok := c.(HTTPRequester); ok {
		doer = d
	} else if c == nil {
		doer = DefaultClient
	}

	resp, err := acquireRequestBuilder(doer).
		SetContext(ctx).
		SetResult(&target).
		Apply(mods...).
		Execute(method, path)

	return target, resp, err
}

// BatchFetchTo dispatches multiple requests concurrently and unmarshals each 2xx response payload into a slice of T.
func BatchFetchTo[T any](
	ctx context.Context,
	c any,
	method string,
	paths []string,
	mods ...RequestModifier,
) ([]T, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	results := make([]T, len(paths))

	type fetchResult struct {
		idx int
		err error
	}

	resCh := make(chan fetchResult, len(paths))

	for i, path := range paths {
		go func(idx int, p string) {
			val, resp, err := FetchTo[T](ctx, c, method, p, mods...)
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}

			if err == nil {
				results[idx] = val
			}

			resCh <- fetchResult{idx: idx, err: err}
		}(i, path)
	}

	var firstErr error
	for range paths {
		res := <-resCh
		if res.err != nil && firstErr == nil {
			firstErr = res.err
		}
	}

	return results, firstErr
}

// BatchGetTo dispatches multiple GET requests concurrently and unmarshals each 2xx response payload into a slice of T.
func BatchGetTo[T any](
	ctx context.Context,
	c any,
	paths []string,
	mods ...RequestModifier,
) ([]T, error) {
	return BatchFetchTo[T](ctx, c, http.MethodGet, paths, mods...)
}

// FetchScoped executes a request with method, path, and optional modifiers, passing the decoded response
// into fn within an active [borrow.Scope].
func FetchScoped[T any](
	ctx context.Context,
	c any,
	method, path string,
	fn func(scope *borrow.Scope, val T, resp *http.Response) error,
	mods ...RequestModifier,
) error {
	var (
		target T
		doer   HTTPRequester
	)
	if d, ok := c.(HTTPRequester); ok {
		doer = d
	} else if c == nil {
		doer = DefaultClient
	}

	resp, err := acquireRequestBuilder(doer).
		SetContext(ctx).
		SetResult(&target).
		Apply(mods...).
		Execute(method, path)
	if err != nil {
		return err
	}

	if resp != nil && resp.Body != nil {
		defer func() {
			_ = resp.Body.Close()
		}()
	}

	scope := borrow.AcquireScope()
	defer scope.Release()

	return fn(scope, target, resp)
}

// GetScoped dispatches a GET request and passes the decoded response T to fn within an active [borrow.Scope].
func GetScoped[T any](
	ctx context.Context,
	c any,
	path string,
	fn func(scope *borrow.Scope, val T, resp *http.Response) error,
	mods ...RequestModifier,
) error {
	return FetchScoped[T](ctx, c, http.MethodGet, path, fn, mods...)
}
