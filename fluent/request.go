// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fluent

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/netutil"
	"github.com/lemon4ksan/aoni/request"
	"github.com/lemon4ksan/aoni/telemetry"
)

var bytePool = sync.Pool{
	New: func() any {
		b := make([]byte, 32*1024)
		return &b
	},
}

var requestPool = sync.Pool{
	New: func() any {
		return &Request{
			headers:     make(http.Header),
			queryParams: make(url.Values),
			pathParams:  make(map[string]string),
		}
	},
}

// Request is a thread-safe pooled request builder offering a fluent chainable API.
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

	client           *aoni.Client
	basicAuth        *basicAuth
	digestAuth       *digestAuth
	headers          http.Header
	queryParams      url.Values
	pathParams       map[string]string
	downloadProgress aoni.ProgressFunc
	uploadProgress   aoni.ProgressFunc
	traceInfo        *telemetry.TraceInfo
	appliedMods      []aoni.RequestModifier
	timeout          time.Duration

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

// Reset clears all request fields to prepare the instance for sync.Pool recycling.
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
	r.downloadProgress = nil
	r.uploadProgress = nil
	r.traceInfo = nil
	r.appliedMods = r.appliedMods[:0]
	r.timeout = 0
	r.useProtoDecoder = false
	r.useGRPCWebDecoder = false

	for k := range r.headers {
		delete(r.headers, k)
	}

	for k := range r.queryParams {
		delete(r.queryParams, k)
	}

	for k := range r.pathParams {
		delete(r.pathParams, k)
	}
}

// SetContext associates a context.Context with the request execution.
func (r *Request) SetContext(ctx context.Context) *Request {
	r.ctx = ctx
	return r
}

// SetHeader sets a single HTTP request header.
func (r *Request) SetHeader(header, value string) *Request {
	r.headers.Set(header, value)
	return r
}

// SetHeaders bulk-sets HTTP headers from a map.
func (r *Request) SetHeaders(headers map[string]string) *Request {
	for k, v := range headers {
		r.headers.Set(k, v)
	}

	return r
}

// SetQueryParam appends a URL query parameter key-value pair.
func (r *Request) SetQueryParam(param, value string) *Request {
	r.queryParams.Add(param, value)
	return r
}

// SetQueryParams bulk-sets URL query parameters from a map.
func (r *Request) SetQueryParams(params map[string]string) *Request {
	for k, v := range params {
		r.queryParams.Add(k, v)
	}

	return r
}

// SetQueryStruct sets a struct to be marshalled into query parameters using aoni schema caching.
func (r *Request) SetQueryStruct(v any) *Request {
	r.queryStruct = v
	return r
}

// SetPathParam sets a URL path parameter to be interpolated (e.g. /users/{id}).
func (r *Request) SetPathParam(param, value string) *Request {
	r.pathParams[param] = value
	return r
}

// SetPathParams bulk-sets URL path parameters from a map.
func (r *Request) SetPathParams(params map[string]string) *Request {
	maps.Copy(r.pathParams, params)
	return r
}

// SetBearerToken injects an "Authorization: Bearer <token>" header.
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

// SetOutputFromHeader instructs the request to stream the downloaded file to targetDir
// using a sanitized, Path Traversal-safe filename extracted from Content-Disposition header.
func (r *Request) SetOutputFromHeader(targetDir string) *Request {
	r.outputDirectory = targetDir
	return r
}

// SetBody sets the payload body to be serialized into the request as JSON or raw stream.
func (r *Request) SetBody(body any) *Request {
	r.body = body
	return r
}

// SetProtoBody serializes a Protocol Buffer message into the binary request payload.
func (r *Request) SetProtoBody(msg proto.Message) *Request {
	r.protoBody = msg
	return r
}

// SetGRPCWebBody serializes a Protocol Buffer message into a gRPC-Web framed request payload.
func (r *Request) SetGRPCWebBody(msg proto.Message) *Request {
	r.grpcWebBody = msg
	return r
}

// SetResult sets the target struct pointer into which a 2xx response body is decoded.
func (r *Request) SetResult(result any) *Request {
	r.result = result
	return r
}

// SetProtoResult configures the response target to be decoded via ProtoDecoder.
func (r *Request) SetProtoResult(result any) *Request {
	r.result = result
	r.useProtoDecoder = true
	return r
}

// SetGRPCWebResult configures the response target to be decoded via GRPCWebDecoder.
func (r *Request) SetGRPCWebResult(result any) *Request {
	r.result = result
	r.useGRPCWebDecoder = true
	return r
}

// SetError sets the target struct pointer into which non-2xx response bodies are decoded.
func (r *Request) SetError(errResult any) *Request {
	r.resultError = errResult
	return r
}

// SetOutput sets the local file path to stream and save the response body directly to disk.
func (r *Request) SetOutput(filePath string) *Request {
	r.outputFile = filePath
	return r
}

// SetSaveFileName is an alias for SetOutput.
func (r *Request) SetSaveFileName(filePath string) *Request {
	return r.SetOutput(filePath)
}

// SetDownloadProgress registers a callback function to monitor download progress in real-time.
func (r *Request) SetDownloadProgress(progress aoni.ProgressFunc) *Request {
	r.downloadProgress = progress
	return r
}

// SetUploadProgress registers a callback function to monitor request payload upload progress.
func (r *Request) SetUploadProgress(progress aoni.ProgressFunc) *Request {
	r.uploadProgress = progress
	return r
}

// SetTrace associates a TraceInfo container to capture detailed network timings and TLS details.
func (r *Request) SetTrace(info *telemetry.TraceInfo) *Request {
	r.traceInfo = info
	return r
}

// SetCorrelationID sets an end-to-end tracing Correlation ID for the request.
func (r *Request) SetCorrelationID(id string) *Request {
	r.correlationID = id
	return r
}

// SetForceContentType forces automatic response decoding using the specified MIME type.
func (r *Request) SetForceContentType(mime string) *Request {
	r.forceContentType = mime
	return r
}

// SetForceJSON forces automatic response decoding as JSON even if Content-Type header is missing or text/plain.
func (r *Request) SetForceJSON() *Request {
	return r.SetForceContentType("application/json")
}

// SetLabel attaches a human-readable metric/route label for observability.
func (r *Request) SetLabel(label string) *Request {
	r.label = label
	return r
}

// Apply injects reusable RequestModifier functions into the request builder.
func (r *Request) Apply(mods ...aoni.RequestModifier) *Request {
	r.appliedMods = append(r.appliedMods, mods...)
	return r
}

// SetTimeout sets a per-request execution deadline timeout.
func (r *Request) SetTimeout(timeout time.Duration) *Request {
	r.timeout = timeout
	return r
}

// Download is a convenience method to execute a GET request and stream the response directly to filePath.
func (r *Request) Download(url, filePath string) (*http.Response, error) {
	return r.SetOutput(filePath).Get(url)
}

// Execute compiles the request builder into RequestModifiers and executes the HTTP request.
func (r *Request) Execute(method, path string) (*http.Response, error) {
	client := r.client
	resultTarget := r.result
	outputFile := r.outputFile

	defer func() {
		r.Reset()
		requestPool.Put(r)
	}()

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
		var rawResp *http.Response

		mods = append(mods, mod.WithCaptureResponse(&rawResp))

		resp, err := client.Request(ctx, method, finalPath, mods...)
		if err != nil {
			return nil, err
		}

		if err := request.HandleResponse(resp, resultTarget, client); err != nil {
			return resp, err
		}

		return resp, nil
	}

	return client.Request(ctx, method, finalPath, mods...)
}

func (r *Request) buildModifiers() []aoni.RequestModifier {
	var mods []aoni.RequestModifier

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
	case r.protoBody != nil:
		mods = append(mods, mod.WithProtoBody(r.protoBody))
	case r.grpcWebBody != nil:
		mods = append(mods, mod.WithGRPCWebBody(r.grpcWebBody))
	case r.body != nil:
		if reader, ok := r.body.(io.Reader); ok {
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
	resp, err := client.Request(ctx, method, path, mods...)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		if r.resultError != nil {
			_ = request.HandleResponse(resp, r.resultError, client)
		}

		return resp, fmt.Errorf("aoni: download failed with status code %d", resp.StatusCode)
	}

	if outputFile == "" && r.outputDirectory != "" {
		filename := netutil.ExtractSanitizedFilename(resp.Header.Get("Content-Disposition"))
		outputFile = filepath.Join(r.outputDirectory, filename)
	}

	if err := os.MkdirAll(filepath.Dir(outputFile), 0o755); err != nil { //nolint:gosec
		return resp, fmt.Errorf("aoni: failed to create target directory: %w", err)
	}

	outFile, err := os.Create(outputFile)
	if err != nil {
		return resp, fmt.Errorf("aoni: failed to create output file: %w", err)
	}
	defer outFile.Close()

	bufPtr := bytePool.Get().(*[]byte)
	_, err = io.CopyBuffer(outFile, resp.Body, *bufPtr)
	bytePool.Put(bufPtr)

	if err != nil {
		return resp, fmt.Errorf("aoni: error writing downloaded file: %w", err)
	}

	return resp, nil
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
