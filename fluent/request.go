// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fluent

import (
	"context"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	fio "github.com/lemon4ksan/foundation/io"
	furl "github.com/lemon4ksan/foundation/net/url"
	"github.com/lemon4ksan/foundation/silicon/pool"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec"
	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/middleware"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/netutil"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/request"
	"github.com/lemon4ksan/aoni/resiliency"
	"github.com/lemon4ksan/aoni/telemetry"
)

type typedRequestPool struct {
	storage *pool.PerPStorage[*Request]
}

func newTypedRequestPool() *typedRequestPool {
	return &typedRequestPool{
		storage: pool.NewPerPStorage(func() *Request {
			return &Request{
				appliedMods:      make([]aoni.RequestModifier, 0, 8),
				expectedStatuses: make([]int, 0, 4),
				headerEntries:    make([]headerEntry, 0, 8),
				queryEntries:     make([]queryParamEntry, 0, 8),
				pathParams:       make(map[string]string, 4),
				formFields:       make(map[string]string, 4),
			}
		}),
	}
}

// Get retrieves a pooled [Request] instance bound to any engine or client.
func (p *typedRequestPool) Get(doer any) *Request {
	reqClient := request.AsRequester(doer)

	r := p.storage.Get()
	r.client = reqClient

	return r
}

// Put recycles a [Request] instance back to the core-pinned storage after resetting fields.
func (p *typedRequestPool) Put(r *Request) {
	if r == nil {
		return
	}

	r.Reset()
	p.storage.Put(r)
}

var requestPool = newTypedRequestPool()

func acquireRequest(doer any) *Request {
	return requestPool.Get(doer)
}

type headerEntry struct {
	key string
	val string
}

type queryParamEntry struct {
	key string
	val string
}

// Request is a pooled request builder offering a chainable, fluent configuration API.
//
// Thread Safety:
// Request instances are NOT safe for concurrent use across multiple goroutines.
// They are intended for single-goroutine linear construction and execution before being returned to the pool.
type Request struct {
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

	client            request.Requester
	basicAuth         *basicAuth
	digestAuth        *digestAuth
	headers           http.Header
	headerEntries     []headerEntry
	queryParams       url.Values
	queryEntries      []queryParamEntry
	pathParams        map[string]string
	formFields        map[string]string
	formFiles         map[string]io.Reader
	expectedStatuses  []int
	downloadProgress  aoni.ProgressFunc
	uploadProgress    aoni.ProgressFunc
	traceInfo         *telemetry.TraceInfo
	appliedMods       []aoni.RequestModifier
	timeout           time.Duration
	retryOverride     *core.RetryOverride
	xmlBody           any
	yamlBody          any
	useProtoDecoder   bool
	useGRPCWebDecoder bool
	useXMLDecoder     bool
	useYAMLDecoder    bool
}

// basicAuth stores HTTP Basic Authentication credentials.
type basicAuth struct {
	username string
	password string
}

// digestAuth stores RFC 7616 Digest Access Authentication credentials.
type digestAuth struct {
	username string
	password string
}

// Reset clears all request fields to prepare the instance for pool recycling.
func (r *Request) Reset() {
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
func (r *Request) Release() {
	if r == nil {
		return
	}

	r.Reset()
	requestPool.Put(r)
}

// Header returns or acquires the internal [http.Header] map.
func (r *Request) Header() http.Header {
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
func (r *Request) SetContext(ctx context.Context) *Request {
	r.ctx = ctx
	return r
}

// SetHeader sets an HTTP header key-value pair.
func (r *Request) SetHeader(header, value string) *Request {
	if r.headers != nil {
		r.headers.Set(header, value)
		return r
	}

	r.headerEntries = append(r.headerEntries, headerEntry{key: header, val: value})

	return r
}

// SetHeaders bulk-sets HTTP headers from a map.
func (r *Request) SetHeaders(headers map[string]string) *Request {
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
func (r *Request) SetQueryParam(param, value string) *Request {
	if r.queryParams != nil {
		r.queryParams.Add(param, value)
		return r
	}

	r.queryEntries = append(r.queryEntries, queryParamEntry{key: param, val: value})

	return r
}

// SetQueryParams bulk-sets URL query parameters from a map.
func (r *Request) SetQueryParams(params map[string]string) *Request {
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
func (r *Request) ExpectStatus(codes ...int) *Request {
	r.expectedStatuses = append(r.expectedStatuses, codes...)
	return r
}

// SetFormField adds a form key-value field for multipart/form-data requests.
func (r *Request) SetFormField(key, value string) *Request {
	if r.formFields == nil {
		r.formFields = make(map[string]string, 4)
	}

	r.formFields[key] = value

	return r
}

// SetFormFile attaches a stream reader as a file part in multipart/form-data requests.
func (r *Request) SetFormFile(fieldname string, reader io.Reader) *Request {
	if r.formFiles == nil {
		r.formFiles = make(map[string]io.Reader, 2)
	}

	r.formFiles[fieldname] = reader

	return r
}

// SetProxy routes this request through a target proxy URL.
func (r *Request) SetProxy(proxyURL string) *Request {
	r.proxyOverride = proxyURL
	return r
}

// Retry sets the request retry policy via [resiliency.RetryBuilder].
func (r *Request) Retry(builder *resiliency.RetryBuilder) *Request {
	if builder != nil {
		override := builder.ToOverride()
		r.retryOverride = &override
	}

	return r
}

// SetRetry configures custom retry parameters for this request attempt.
func (r *Request) SetRetry(maxAttempts int, backoff time.Duration) *Request {
	r.retryOverride = &core.RetryOverride{
		MaxAttempts: maxAttempts,
		Backoff:     backoff,
		Condition:   middleware.RetryOnTransientErrors(),
	}

	return r
}

// WithCodec applies request encoding and response decoding strategies defined by codec.
func (r *Request) WithCodec(c codec.Codec, body any) *Request {
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
func (r *Request) SetQueryStruct(v any) *Request {
	r.queryStruct = v
	return r
}

// SetPathParam sets a URL path template parameter (e.g. /users/{id}).
func (r *Request) SetPathParam(param, value string) *Request {
	if r.pathParams == nil {
		r.pathParams = make(map[string]string, 4)
	}

	r.pathParams[param] = value

	return r
}

// SetPathParams bulk-sets URL path parameters from a map.
func (r *Request) SetPathParams(params map[string]string) *Request {
	if r.pathParams == nil {
		r.pathParams = make(map[string]string, len(params))
	}

	maps.Copy(r.pathParams, params)

	return r
}

// SetBearerToken sets an "Authorization: Bearer <token>" header.
func (r *Request) SetBearerToken(token string) *Request {
	r.bearerToken = token
	return r
}

// SetBasicAuth sets HTTP Basic Authentication credentials.
func (r *Request) SetBasicAuth(username, password string) *Request {
	r.basicAuth = &basicAuth{username: username, password: password}
	return r
}

// SetDigestAuth configures RFC 7616 Digest Access Authentication credentials.
func (r *Request) SetDigestAuth(username, password string) *Request {
	r.digestAuth = &digestAuth{username: username, password: password}
	return r
}

// SetPKCE adds PKCE code_challenge and code_challenge_method parameters for OAuth 2.0 requests (RFC 7636 / RFC 9700).
func (r *Request) SetPKCE(verifier string, method ...string) *Request {
	r.appliedMods = append(r.appliedMods, mod.WithPKCE(verifier, method...))
	return r
}

// SetPKCEVerifier adds the PKCE code_verifier parameter for OAuth 2.0 token requests (RFC 7636 / RFC 9700).
func (r *Request) SetPKCEVerifier(verifier string) *Request {
	r.appliedMods = append(r.appliedMods, mod.WithPKCEVerifier(verifier))
	return r
}

// SetOutputFromHeader instructs the request to stream the downloaded file to targetDir using Content-Disposition filenames.
func (r *Request) SetOutputFromHeader(targetDir string) *Request {
	r.outputDirectory = targetDir
	return r
}

// SetBody sets the payload body to be serialized into the request.
func (r *Request) SetBody(body any) *Request {
	r.body = body
	return r
}

// SetXMLBody serializes payload into XML request bytes and sets 'Content-Type: application/xml'.
func (r *Request) SetXMLBody(body any) *Request {
	r.xmlBody = body
	return r
}

// SetYAMLBody serializes payload into YAML request bytes and sets 'Content-Type: application/yaml'.
func (r *Request) SetYAMLBody(body any) *Request {
	r.yamlBody = body
	return r
}

// SetProtoBody serializes a [proto.Message] into binary request bytes.
func (r *Request) SetProtoBody(msg proto.Message) *Request {
	r.protoBody = msg
	return r
}

// SetGRPCWebBody serializes a [proto.Message] into a gRPC-Web framed request payload.
func (r *Request) SetGRPCWebBody(msg proto.Message) *Request {
	r.grpcWebBody = msg
	return r
}

// SetResult sets the target structure pointer for unmarshaling 2xx response bodies.
func (r *Request) SetResult(result any) *Request {
	r.result = result
	return r
}

// SetXMLResult configures response target unmarshaling via [decode.XMLDecoder].
func (r *Request) SetXMLResult(result any) *Request {
	r.result = result
	r.useXMLDecoder = true

	return r
}

// SetYAMLResult configures response target unmarshaling via [decode.YAMLDecoder].
func (r *Request) SetYAMLResult(result any) *Request {
	r.result = result
	r.useYAMLDecoder = true

	return r
}

// SetProtoResult configures response target unmarshaling via [decode.ProtoDecoder].
func (r *Request) SetProtoResult(result any) *Request {
	r.result = result
	r.useProtoDecoder = true

	return r
}

// SetGRPCWebResult configures response target unmarshaling via [decode.GRPCWebDecoder].
func (r *Request) SetGRPCWebResult(result any) *Request {
	r.result = result
	r.useGRPCWebDecoder = true

	return r
}

// SetError sets the target structure pointer for non-2xx response unmarshaling.
func (r *Request) SetError(errResult any) *Request {
	r.resultError = errResult
	return r
}

// SetOutput sets the local disk file path to stream and save the response payload directly.
func (r *Request) SetOutput(filePath string) *Request {
	r.outputFile = filePath
	return r
}

// SetDownloadProgress registers an [aoni.ProgressFunc] callback monitoring response stream reads.
func (r *Request) SetDownloadProgress(progress aoni.ProgressFunc) *Request {
	r.downloadProgress = progress
	return r
}

// SetUploadProgress registers an [aoni.ProgressFunc] callback monitoring request body uploads.
func (r *Request) SetUploadProgress(progress aoni.ProgressFunc) *Request {
	r.uploadProgress = progress
	return r
}

// SetTrace associates a [telemetry.TraceInfo] container to capture fine-grained network timings.
func (r *Request) SetTrace(info *telemetry.TraceInfo) *Request {
	r.traceInfo = info
	return r
}

// SetCorrelationID assigns an end-to-end tracing Correlation ID to the request.
func (r *Request) SetCorrelationID(id string) *Request {
	r.correlationID = id
	return r
}

// SetForceContentType forces response parsing using the specified MIME type.
func (r *Request) SetForceContentType(mime string) *Request {
	r.forceContentType = mime
	return r
}

// SetForceJSON forces response parsing as JSON regardless of Content-Type headers.
func (r *Request) SetForceJSON() *Request {
	return r.SetForceContentType("application/json")
}

// SetLabel attaches a human-readable metric or route label.
func (r *Request) SetLabel(label string) *Request {
	r.label = label
	return r
}

// Apply injects custom [aoni.RequestModifier] options into the builder chain.
func (r *Request) Apply(mods ...aoni.RequestModifier) *Request {
	r.appliedMods = append(r.appliedMods, mods...)
	return r
}

// SetTimeout sets a per-request context deadline timeout.
func (r *Request) SetTimeout(timeout time.Duration) *Request {
	r.timeout = timeout
	return r
}

// Download is a convenience method executing a GET request and streaming response bytes to filePath.
func (r *Request) Download(url, filePath string) (*http.Response, error) {
	return r.SetOutput(filePath).Get(url)
}

// Execute compiles builder configurations into modifiers and executes the request.
//
// Postconditions:
//   - Automatically releases the request instance back to the pool upon completion.
func (r *Request) Execute(method, path string) (*http.Response, error) {
	client := r.client
	resultTarget := r.result
	outputFile := r.outputFile

	defer r.Release()

	finalPath := furl.BuildPath(path, r.pathParams, nil)

	ctx := r.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	if r.digestAuth != nil {
		client = request.Configure(client, option.WithDigestAuth(r.digestAuth.username, r.digestAuth.password))
	}

	var stackBuf [stackModCapacity]aoni.RequestModifier

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

		if err := request.HandleResponse(resp, resultTarget, client); err != nil {
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

// checkExpectedStatus verifies that the response status code matches expectations configured via [Request.ExpectStatus].
func (r *Request) checkExpectedStatus(resp *http.Response, finalPath string) error {
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

const stackModCapacity = 16

// buildModifiers constructs value modifiers for headers, auth, body serialization, decoding, and telemetry.
func (r *Request) buildModifiers(stackBuf *[stackModCapacity]aoni.RequestModifier) []aoni.RequestModifier {
	estimatedCap := len(r.headerEntries) + len(r.headers) + len(r.queryEntries) + len(r.appliedMods) + 12

	var mods []aoni.RequestModifier
	if estimatedCap <= stackModCapacity {
		mods = stackBuf[:0]
	} else {
		mods = make([]aoni.RequestModifier, 0, estimatedCap)
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

func (r *Request) appendHeaderAndAuthModifiers(mods []aoni.RequestModifier) []aoni.RequestModifier {
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

func (r *Request) appendQueryAndBodyModifiers(mods []aoni.RequestModifier) []aoni.RequestModifier {
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

func (r *Request) appendTelemetryAndMiscModifiers(mods []aoni.RequestModifier) []aoni.RequestModifier {
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
func (r *Request) executeDownload(
	ctx context.Context,
	client request.Requester,
	method, path string,
	mods []aoni.RequestModifier,
	outputFile string,
) (*http.Response, error) {
	maxAttempts := 5
	if r.retryOverride != nil && r.retryOverride.MaxAttempts > 0 {
		maxAttempts = r.retryOverride.MaxAttempts
	}

	var (
		lastResp *http.Response
		lastErr  error
	)

	attemptMods := make([]aoni.RequestModifier, 0, len(mods)+1)
	for range maxAttempts {
		attemptMods = attemptMods[:0]
		attemptMods = append(attemptMods, mods...)
		existingSize := getFileSize(outputFile, r.outputDirectory, respHeaderFilename(lastResp))

		if existingSize > 0 {
			attemptMods = append(attemptMods, mod.WithHeader("Range", "bytes="+strconv.FormatInt(existingSize, 10)+"-"))
		}

		resp, err := client.Request(ctx, method, path, attemptMods...)
		if err != nil {
			lastErr = err

			if ctx.Err() != nil {
				return nil, ctx.Err()
			}

			continue
		}

		lastResp = resp
		targetPath := resolveOutputPath(outputFile, r.outputDirectory, resp.Header.Get("Content-Disposition"))

		if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
			_ = resp.Body.Close()
			return resp, &Error{Op: "download", Path: targetPath, Code: resp.StatusCode, Err: ErrRangeNotSatisfiable}
		}

		if resp.StatusCode >= http.StatusBadRequest {
			if r.resultError != nil {
				_ = request.HandleResponse(resp, r.resultError, client)
			} else {
				_ = resp.Body.Close()
			}

			return resp, &Error{Op: "download", Path: targetPath, Code: resp.StatusCode, Err: ErrDownloadFailed}
		}

		writeErr := writeDownloadedStream(targetPath, resp, existingSize)
		if writeErr == nil {
			return resp, nil
		}

		lastErr = writeErr
		_ = resp.Body.Close()

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}

	return lastResp, lastErr
}

// writeDownloadedStream writes HTTP response payload into outputFile on disk.
func writeDownloadedStream(outputFile string, resp *http.Response, previousSize int64) error {
	defer resp.Body.Close()

	if err := os.MkdirAll(filepath.Dir(outputFile), 0o755); err != nil {
		return err
	}

	flags := os.O_CREATE | os.O_WRONLY
	if resp.StatusCode == http.StatusPartialContent && previousSize > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	outFile, err := os.OpenFile(outputFile, flags, 0o644) //nolint:gosec
	if err != nil {
		return err
	}
	defer outFile.Close()

	_, err = fio.CopyZeroAlloc(outFile, resp.Body)

	return err
}

// getFileSize returns the byte size of existing local file for Range requests.
func getFileSize(outputFile, outputDirectory, headerFilename string) int64 {
	targetPath := resolveOutputPath(outputFile, outputDirectory, headerFilename)
	if targetPath == "" {
		return 0
	}

	info, err := os.Stat(targetPath)
	if err != nil || info.IsDir() {
		return 0
	}

	return info.Size()
}

// resolveOutputPath resolves destination local path from explicit path, output directory, or Content-Disposition.
func resolveOutputPath(outputFile, outputDirectory, contentDisposition string) string {
	if outputFile != "" {
		return outputFile
	}

	if outputDirectory != "" {
		filename := netutil.ExtractSanitizedFilename(contentDisposition)
		return filepath.Join(outputDirectory, filename)
	}

	return ""
}

// respHeaderFilename extracts the Content-Disposition header string from resp.
func respHeaderFilename(resp *http.Response) string {
	if resp == nil {
		return ""
	}

	return resp.Header.Get("Content-Disposition")
}

// Get executes a GET request against path.
func (r *Request) Get(path string) (*http.Response, error) {
	return r.Execute(http.MethodGet, path)
}

// Post executes a POST request against path.
func (r *Request) Post(path string) (*http.Response, error) {
	return r.Execute(http.MethodPost, path)
}

// Put executes a PUT request against path.
func (r *Request) Put(path string) (*http.Response, error) {
	return r.Execute(http.MethodPut, path)
}

// Delete executes a DELETE request against path.
func (r *Request) Delete(path string) (*http.Response, error) {
	return r.Execute(http.MethodDelete, path)
}

// Patch executes a PATCH request against path.
func (r *Request) Patch(path string) (*http.Response, error) {
	return r.Execute(http.MethodPatch, path)
}

// Head executes a HEAD request against path.
func (r *Request) Head(path string) (*http.Response, error) {
	return r.Execute(http.MethodHead, path)
}

// Options executes an OPTIONS request against path.
func (r *Request) Options(path string) (*http.Response, error) {
	return r.Execute(http.MethodOptions, path)
}

// Trace executes a TRACE request against path.
func (r *Request) Trace(path string) (*http.Response, error) {
	return r.Execute(http.MethodTrace, path)
}

// Connect executes a CONNECT request against path.
func (r *Request) Connect(path string) (*http.Response, error) {
	return r.Execute(http.MethodConnect, path)
}
