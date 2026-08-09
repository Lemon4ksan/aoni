// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fluent

import (
	"context"
	"fmt"
	stdio "io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec"
	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/internal/io"
	"github.com/lemon4ksan/aoni/internal/pool"
	"github.com/lemon4ksan/aoni/middleware"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/netutil"
	"github.com/lemon4ksan/aoni/request"
	"github.com/lemon4ksan/aoni/telemetry"
)

// TypedRequestPool provides a zero-boxing free-list pool for [Request] instances.
type TypedRequestPool struct {
	mu    sync.Mutex
	items []*Request
}

// Get retrieves a pooled [Request] instance bound to client.
func (p *TypedRequestPool) Get(client *aoni.Client) *Request {
	p.mu.Lock()

	n := len(p.items)
	if n > 0 {
		r := p.items[n-1]
		p.items = p.items[:n-1]
		p.mu.Unlock()

		r.client = client

		return r
	}

	p.mu.Unlock()

	return &Request{
		client: client,
	}
}

// Put recycles a [Request] instance back to the free-list pool after resetting fields.
func (p *TypedRequestPool) Put(r *Request) {
	r.Reset()
	p.mu.Lock()

	if len(p.items) < 1024 {
		p.items = append(p.items, r)
	}

	p.mu.Unlock()
}

var requestPool = &TypedRequestPool{
	items: make([]*Request, 0, 128),
}

// Request is a pooled request builder offering a chainable, fluent configuration API.
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

	client           *aoni.Client
	basicAuth        *basicAuth
	digestAuth       *digestAuth
	headers          http.Header
	queryParams      url.Values
	pathParams       map[string]string
	formFields       map[string]string
	formFiles        map[string]stdio.Reader
	expectedStatuses []int
	downloadProgress aoni.ProgressFunc
	uploadProgress   aoni.ProgressFunc
	traceInfo        *telemetry.TraceInfo
	appliedMods      []aoni.RequestModifier
	timeout          time.Duration
	retryOverride    *aoni.RetryOverride

	useProtoDecoder   bool
	useGRPCWebDecoder bool
}

type basicAuth struct {
	username string
	password string
}

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
	r.timeout = 0
	r.retryOverride = nil
	r.useProtoDecoder = false
	r.useGRPCWebDecoder = false

	if r.headers != nil {
		pool.ReleaseHeader(r.headers)
		r.headers = nil
	}

	clear(r.queryParams)
	clear(r.pathParams)
	clear(r.formFields)
	clear(r.formFiles)
}

// Release resets the request builder and returns it to the free-list pool.
func (r *Request) Release() {
	if r == nil {
		return
	}

	r.Reset()
	requestPool.Put(r)
}

// Discard is an alias for [Request.Release].
func (r *Request) Discard() {
	r.Release()
}

// Header returns or acquires the internal [http.Header] map.
func (r *Request) Header() http.Header {
	if r.headers == nil {
		r.headers = pool.AcquireHeader()
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
	if r.headers == nil {
		r.headers = pool.AcquireHeader()
	}

	r.headers.Set(header, value)

	return r
}

// SetHeaders bulk-sets HTTP headers from a map.
func (r *Request) SetHeaders(headers map[string]string) *Request {
	if r.headers == nil {
		r.headers = pool.AcquireHeader()
	}

	for k, v := range headers {
		r.headers.Set(k, v)
	}

	return r
}

// SetQueryParam appends a URL query parameter key-value pair.
func (r *Request) SetQueryParam(param, value string) *Request {
	if r.queryParams == nil {
		r.queryParams = make(url.Values, 4)
	}

	r.queryParams.Add(param, value)

	return r
}

// SetQueryParams bulk-sets URL query parameters from a map.
func (r *Request) SetQueryParams(params map[string]string) *Request {
	if r.queryParams == nil {
		r.queryParams = make(url.Values, len(params))
	}

	for k, v := range params {
		r.queryParams.Add(k, v)
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
func (r *Request) SetFormFile(fieldname string, reader stdio.Reader) *Request {
	if r.formFiles == nil {
		r.formFiles = make(map[string]stdio.Reader, 2)
	}

	r.formFiles[fieldname] = reader

	return r
}

// SetProxy routes this request through a target proxy URL.
func (r *Request) SetProxy(proxyURL string) *Request {
	r.proxyOverride = proxyURL
	return r
}

// SetRetry configures custom retry parameters for this request attempt.
func (r *Request) SetRetry(maxAttempts int, backoff time.Duration) *Request {
	r.retryOverride = &aoni.RetryOverride{
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

	if encMod := c.Encode(body); encMod != nil {
		r.appliedMods = append(r.appliedMods, encMod)
	}

	if decMod := c.Decode(); decMod != nil {
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

// SetSaveFileName is an alias for [Request.SetOutput].
func (r *Request) SetSaveFileName(filePath string) *Request {
	return r.SetOutput(filePath)
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

	finalPath := interpolatePathParams(path, r.pathParams)

	ctx := r.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	mods := r.buildModifiers()

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

func (r *Request) buildModifiers() []aoni.RequestModifier {
	estimatedCap := len(r.headers) + len(r.appliedMods) + 12
	mods := make([]aoni.RequestModifier, 0, estimatedCap)

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
	case r.body != nil:
		if reader, ok := r.body.(stdio.Reader); ok {
			mods = append(mods, mod.WithBody(reader))
		} else {
			mods = append(mods, mod.WithJSONBody(r.body))
		}
	}

	if r.useProtoDecoder {
		mods = append(mods, decode.WithProto())
	} else if r.useGRPCWebDecoder {
		mods = append(mods, decode.WithGRPCWeb())
	}

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

	if len(r.appliedMods) > 0 {
		mods = append(mods, r.appliedMods...)
	}

	if r.timeout > 0 {
		mods = append(mods, mod.WithTimeout(r.timeout))
	}

	return mods
}

func (r *Request) executeDownload(
	ctx context.Context,
	client *aoni.Client,
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
			attemptMods = append(attemptMods, mod.WithHeader("Range", fmt.Sprintf("bytes=%d-", existingSize)))
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

	_, err = io.CopyZeroAlloc(outFile, resp.Body)

	return err
}

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

// Do executes a request with any custom HTTP method against path.
func (r *Request) Do(method, path string) (*http.Response, error) {
	return r.Execute(method, path)
}

func interpolatePathParams(rawPath string, params map[string]string) string {
	if len(params) == 0 || strings.IndexByte(rawPath, '{') == -1 {
		return rawPath
	}

	var sb strings.Builder
	sb.Grow(len(rawPath) + 16)

	for {
		start := strings.IndexByte(rawPath, '{')
		if start == -1 {
			sb.WriteString(rawPath)
			break
		}

		end := strings.IndexByte(rawPath[start:], '}')
		if end == -1 {
			sb.WriteString(rawPath)
			break
		}

		end += start

		sb.WriteString(rawPath[:start])
		paramName := rawPath[start+1 : end]

		if val, ok := params[paramName]; ok {
			sb.WriteString(url.PathEscape(val))
		} else {
			sb.WriteByte('{')
			sb.WriteString(paramName)
			sb.WriteByte('}')
		}

		rawPath = rawPath[end+1:]
	}

	return sb.String()
}
