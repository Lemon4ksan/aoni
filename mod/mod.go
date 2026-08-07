// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package mod provides declarative request modifiers for customizing an [aoni.Request] prior to execution.
package mod

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	stdio "io"
	"maps"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/values"
	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/fingerprint"
	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/fingerprint/p0f"
	"github.com/lemon4ksan/aoni/internal/bytesconv"
	"github.com/lemon4ksan/aoni/internal/io"
	"github.com/lemon4ksan/aoni/internal/pipeline"
	"github.com/lemon4ksan/aoni/netutil/fragment"
	"github.com/lemon4ksan/aoni/telemetry"
)

var (
	// ErrBodyNotSeekable indicates that the request body stream cannot be rewound for retries or hedging.
	ErrBodyNotSeekable = errors.New("aoni: body does not support seeking for hedging")

	// ErrInvalidPairCount is returned when WithVars receives an odd number of key-value arguments.
	ErrInvalidPairCount = errors.New("aoni mod: WithVars requires an even number of key-value pairs")
)

var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

// ============================================================================
// 1. URI & PATH PARAMETER MODIFIERS
// ============================================================================

// WithVar constructs an [aoni.RequestModifier] that interpolates a single URI template placeholder (e.g. "{key}") with value.
//
// Preconditions:
//   - Escapes value using [url.PathEscape].
func WithVar(key string, value any) aoni.RequestModifier {
	return func(req aoni.Request) {
		placeholder := "{" + key + "}"
		escapedValue := url.PathEscape(fmt.Sprint(value))

		path := req.Path()
		if strings.Contains(path, placeholder) {
			req.SetPath(strings.ReplaceAll(path, placeholder, escapedValue))
		}
	}
}

// WithVars constructs an [aoni.RequestModifier] replacing multiple path template placeholders using key-value pairs.
//
// Preconditions:
//   - Requires an even number of arguments (alternating key and value pairs).
func WithVars(pairs ...any) aoni.RequestModifier {
	if len(pairs)%2 != 0 {
		return func(req aoni.Request) {
			aoni.GetOrInitRequestConfig(req).BodyError = ErrInvalidPairCount
		}
	}

	return func(req aoni.Request) {
		for i := 0; i < len(pairs); i += 2 {
			key := fmt.Sprint(pairs[i])
			value := fmt.Sprint(pairs[i+1])
			WithVar(key, value)(req)
		}
	}
}

// WithURL returns an [aoni.RequestModifier] that overrides the target request URL directly, bypassing any default BaseURL.
func WithURL(raw string) aoni.RequestModifier {
	return func(req aoni.Request) {
		if raw != "" {
			req.SetURL(raw)
		}
	}
}

// WithoutBaseURL returns an [aoni.RequestModifier] that forces the request to ignore the client's default BaseURL.
func WithoutBaseURL() aoni.RequestModifier {
	return func(req aoni.Request) {
		req.SetURL(req.Path())
	}
}

// WithoutBaseResponse returns an [aoni.RequestModifier] that disables envelope unwrapping (BaseResponse) for a single request.
func WithoutBaseResponse() aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).DisableBaseResponse = true
	}
}

// WithBaseResponse returns an [aoni.RequestModifier] that overrides or sets the BaseResponse envelope provider for a single request.
func WithBaseResponse(provider func() aoni.BaseResponse) aoni.RequestModifier {
	return func(req aoni.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)
		if provider == nil {
			cfg.DisableBaseResponse = true
			cfg.BaseResponseOverride = nil
		} else {
			cfg.BaseResponseOverride = provider
			cfg.DisableBaseResponse = false
		}
	}
}

// WithQuery constructs an [aoni.RequestModifier] encoding query parameters from a struct or map into the request URL.
//
// Side Effects:
//   - Appends encoded key-value pairs to the existing [aoni.Request.RawQuery] string.
func WithQuery(query any) aoni.RequestModifier {
	return func(req aoni.Request) {
		if query == nil {
			return
		}

		qStr, err := resolveQueryString(req, query)
		if err != nil {
			aoni.GetOrInitRequestConfig(req).QueryError = err
			return
		}

		if qStr == "" {
			return
		}

		curr := req.RawQuery()
		if curr == "" {
			req.SetRawQuery(qStr)
			return
		}

		req.SetRawQuery(curr + "&" + qStr)
	}
}

func resolveQueryString(req aoni.Request, query any) (string, error) {
	cfg := aoni.GetRequestConfig(req.Context())
	if cfg != nil && cfg.QueryEncoder != nil {
		qVals, err := cfg.QueryEncoder(query)
		if err != nil || len(qVals) == 0 {
			return "", err
		}

		return qVals.Encode(), nil
	}

	return values.StructToQueryString(query)
}

// ============================================================================
// 2. HEADER & AUTHENTICATION MODIFIERS
// ============================================================================

// WithHeader constructs an [aoni.RequestModifier] setting a single request header key to value.
func WithHeader(key, value string) aoni.RequestModifier {
	return func(req aoni.Request) {
		req.SetHeader(key, value)
	}
}

// WithHeaderBytes constructs an [aoni.RequestModifier] setting a request header using byte slices for zero-allocation setup.
func WithHeaderBytes(key, value []byte) aoni.RequestModifier {
	return func(req aoni.Request) {
		req.SetHeaderBytes(key, value)
	}
}

// WithHeaderFunc constructs an [aoni.RequestModifier] evaluating provider dynamically at request execution time to set key header.
func WithHeaderFunc(key string, provider func() string) aoni.RequestModifier {
	return func(req aoni.Request) {
		if provider != nil && key != "" {
			if val := provider(); val != "" {
				req.SetHeader(key, val)
			}
		}
	}
}

// WithDynamicHeader is an alias for [WithHeaderFunc].
func WithDynamicHeader(key string, provider func() string) aoni.RequestModifier {
	return WithHeaderFunc(key, provider)
}

// WithHeaders constructs an [aoni.RequestModifier] bulk-setting multiple HTTP request headers from a map.
func WithHeaders(headers map[string]string) aoni.RequestModifier {
	return func(req aoni.Request) {
		for k, v := range headers {
			req.SetHeader(k, v)
		}
	}
}

// ResetHeaders constructs an [aoni.RequestModifier] clearing all headers from the request.
func ResetHeaders() aoni.RequestModifier {
	return func(req aoni.Request) {
		req.ResetHeaders()
	}
}

// WithBearer constructs an [aoni.RequestModifier] setting an "Authorization: Bearer <token>" header.
func WithBearer(token string) aoni.RequestModifier {
	return func(req aoni.Request) {
		req.SetHeader("Authorization", "Bearer "+token)
	}
}

// WithBasicAuth constructs an [aoni.RequestModifier] setting HTTP Basic Authentication credentials.
func WithBasicAuth(username, password string) aoni.RequestModifier {
	return func(req aoni.Request) {
		auth := username + ":" + password
		req.SetHeader("Authorization", "Basic "+base64.StdEncoding.EncodeToString(bytesconv.S2B(auth)))
	}
}

// WithUserAgent constructs an [aoni.RequestModifier] overriding the standard User-Agent header.
func WithUserAgent(ua string) aoni.RequestModifier {
	return func(req aoni.Request) {
		req.SetHeader("User-Agent", ua)
	}
}

// WithContentType constructs an [aoni.RequestModifier] overriding the standard Content-Type header.
func WithContentType(ct string) aoni.RequestModifier {
	return func(req aoni.Request) {
		req.SetHeader("Content-Type", ct)
	}
}

// WithAccept constructs an [aoni.RequestModifier] overriding the standard Accept header.
func WithAccept(accept string) aoni.RequestModifier {
	return func(req aoni.Request) {
		req.SetHeader("Accept", accept)
	}
}

// WithOrigin constructs an [aoni.RequestModifier] overriding the standard Origin header.
func WithOrigin(origin string) aoni.RequestModifier {
	return func(req aoni.Request) {
		req.SetHeader("Origin", origin)
	}
}

// WithIfNoneMatch constructs an [aoni.RequestModifier] setting the If-None-Match conditional header.
func WithIfNoneMatch(etag string) aoni.RequestModifier {
	return func(req aoni.Request) {
		req.SetHeader("If-None-Match", etag)
	}
}

// WithIfMatch constructs an [aoni.RequestModifier] setting the If-Match conditional header.
func WithIfMatch(etag string) aoni.RequestModifier {
	return func(req aoni.Request) {
		req.SetHeader("If-Match", etag)
	}
}

// WithIfModifiedSince constructs an [aoni.RequestModifier] setting the If-Modified-Since conditional header in HTTP-time format.
func WithIfModifiedSince(t time.Time) aoni.RequestModifier {
	return func(req aoni.Request) {
		req.SetHeader("If-Modified-Since", t.UTC().Format(http.TimeFormat))
	}
}

// ============================================================================
// 3. COOKIE MODIFIERS
// ============================================================================

// WithCookie constructs an [aoni.RequestModifier] attaching a single [*http.Cookie] to the request.
func WithCookie(c *http.Cookie) aoni.RequestModifier {
	return func(req aoni.Request) {
		if c != nil {
			req.AddHeader("Cookie", c.String())
		}
	}
}

// WithCookies constructs an [aoni.RequestModifier] attaching multiple cookie key-value pairs to the request.
func WithCookies(kv map[string]string) aoni.RequestModifier {
	return func(req aoni.Request) {
		for k, v := range kv {
			req.AddHeader("Cookie", k+"="+v)
		}
	}
}

// WithPartitionKey constructs an [aoni.RequestModifier] attaching a CHIPS (RFC 6265bis) partition key for iFrame/widget context.
func WithPartitionKey(key string) aoni.RequestModifier {
	return func(req aoni.Request) {
		req.SetContext(cookie.WithPartitionKey(req.Context(), key))
	}
}

// ============================================================================
// 4. BODY & PAYLOAD MODIFIERS
// ============================================================================

// WithBody constructs an [aoni.RequestModifier] replacing the request body with the provided stream reader.
func WithBody(r stdio.Reader) aoni.RequestModifier {
	return func(req aoni.Request) {
		if r == nil {
			return
		}

		if b, ok := r.(*bytes.Buffer); ok {
			req.SetBodyBytes(b.Bytes())
			return
		}

		if b, ok := r.(*bytes.Reader); ok {
			buf := make([]byte, b.Len())
			_, _ = b.ReadAt(buf, 0)
			req.SetBodyBytes(buf)

			return
		}

		var lenVal int64 = -1
		if b, ok := r.(interface{ Len() int }); ok {
			lenVal = int64(b.Len())
		} else if s, ok := r.(interface{ Len() int64 }); ok {
			lenVal = s.Len()
		}

		req.SetBodyStream(r, lenVal)
	}
}

// WithBodyBytes constructs an [aoni.RequestModifier] setting raw byte slice payload directly as the request body.
func WithBodyBytes(b []byte) aoni.RequestModifier {
	return func(req aoni.Request) {
		req.SetBodyBytes(b)
	}
}

// WithJSONBody constructs an [aoni.RequestModifier] marshaling payload to JSON and setting Content-Type to application/json.
func WithJSONBody(payload any) aoni.RequestModifier {
	return func(req aoni.Request) {
		if payload == nil {
			return
		}

		bodyBytes, err := json.Marshal(payload)
		if err != nil {
			aoni.GetOrInitRequestConfig(req).BodyError = err
			return
		}

		req.SetBodyBytes(bodyBytes)
		req.SetHeader("Content-Type", "application/json")
	}
}

// WithProtoBody constructs an [aoni.RequestModifier] serializing a [proto.Message] into binary Protocol Buffer bytes.
func WithProtoBody(msg proto.Message) aoni.RequestModifier {
	return func(req aoni.Request) {
		if msg == nil {
			return
		}

		bodyBytes, err := proto.Marshal(msg)
		if err != nil {
			aoni.GetOrInitRequestConfig(req).BodyError = err
			return
		}

		req.SetBodyBytes(bodyBytes)
		req.SetHeader("Content-Type", "application/x-protobuf")
		req.SetHeader("Accept", "application/x-protobuf")
	}
}

// WithGRPCWebBody constructs an [aoni.RequestModifier] serializing a [proto.Message] into 5-byte gRPC-Web framed bytes.
func WithGRPCWebBody(msg proto.Message) aoni.RequestModifier {
	return func(req aoni.Request) {
		if msg == nil {
			return
		}

		protoBytes, err := proto.Marshal(msg)
		if err != nil {
			aoni.GetOrInitRequestConfig(req).BodyError = err
			return
		}

		frame := make([]byte, 5+len(protoBytes))
		frame[0] = 0x00
		binary.BigEndian.PutUint32(frame[1:5], uint32(len(protoBytes))) //nolint:gosec
		copy(frame[5:], protoBytes)

		req.SetBodyBytes(frame)
		req.SetHeader("Content-Type", "application/grpc-web+proto")
		req.SetHeader("Accept", "application/grpc-web+proto")
		req.SetHeader("X-Grpc-Web", "1")
		req.SetHeader("X-User-Agent", "grpc-web-javascript/0.1")
	}
}

// WithMultipart constructs an [aoni.RequestModifier] building an in-memory multipart/form-data request body.
func WithMultipart(fields map[string]string, files map[string]stdio.Reader) aoni.RequestModifier {
	return func(req aoni.Request) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		if cfg := aoni.GetOrInitRequestConfig(req); cfg.MultipartBoundary != "" {
			_ = writer.SetBoundary(cfg.MultipartBoundary)
		}

		for k, v := range fields {
			if err := writer.WriteField(k, v); err != nil {
				aoni.GetOrInitRequestConfig(req).BodyError = err
				return
			}
		}

		for key, r := range files {
			part, err := writer.CreateFormFile(key, key)
			if err != nil {
				aoni.GetOrInitRequestConfig(req).BodyError = err
				return
			}

			if _, err = io.CopyZeroAlloc(part, r); err != nil {
				aoni.GetOrInitRequestConfig(req).BodyError = err
				return
			}
		}

		if err := writer.Close(); err != nil {
			aoni.GetOrInitRequestConfig(req).BodyError = err
			return
		}

		req.SetBodyBytes(body.Bytes())
		req.SetHeader("Content-Type", writer.FormDataContentType())
	}
}

type MultipartField struct {
	Name        string
	Value       string
	Filename    string
	ContentType string
	Reader      stdio.Reader
}

// WithMultipartFields accepts an ordered slice of form fields with support for duplicate names (RFC 7578 Section 5.2)
func WithMultipartFields(fields []MultipartField) aoni.RequestModifier {
	return func(req aoni.Request) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		if cfg := aoni.GetOrInitRequestConfig(req); cfg.MultipartBoundary != "" {
			_ = writer.SetBoundary(cfg.MultipartBoundary)
		}

		for _, f := range fields {
			if f.Reader != nil || f.Filename != "" {
				ct := f.ContentType
				if ct == "" {
					ct = "application/octet-stream"
				}

				part, err := createFormFileHeader(writer, f.Name, f.Filename, ct)
				if err != nil {
					aoni.GetOrInitRequestConfig(req).BodyError = err
					return
				}

				if f.Reader != nil {
					if _, err = io.CopyZeroAlloc(part, f.Reader); err != nil {
						aoni.GetOrInitRequestConfig(req).BodyError = err
						return
					}
				}
			} else {
				if err := writer.WriteField(f.Name, f.Value); err != nil {
					aoni.GetOrInitRequestConfig(req).BodyError = err
					return
				}
			}
		}

		if err := writer.Close(); err != nil {
			aoni.GetOrInitRequestConfig(req).BodyError = err
			return
		}

		req.SetBodyBytes(body.Bytes())
		req.SetHeader("Content-Type", writer.FormDataContentType())
	}
}

// WithStreamingMultipart constructs an [aoni.RequestModifier] streaming multipart/form-data via an asynchronous pipe without in-memory buffering.
func WithStreamingMultipart(fields map[string]string, files map[string]stdio.Reader) aoni.RequestModifier {
	return func(req aoni.Request) {
		pr, pw := stdio.Pipe()

		writer := multipart.NewWriter(pw)
		if cfg := aoni.GetOrInitRequestConfig(req); cfg.MultipartBoundary != "" {
			_ = writer.SetBoundary(cfg.MultipartBoundary)
		}

		ctx := req.Context()
		go streamMultipartPayload(ctx, pw, writer, fields, files)

		req.SetBodyStream(pr, -1)
		req.SetHeader("Content-Type", writer.FormDataContentType())
	}
}

func streamMultipartPayload(
	ctx context.Context,
	pw *stdio.PipeWriter,
	writer *multipart.Writer,
	fields map[string]string,
	files map[string]stdio.Reader,
) {
	defer pw.Close()
	defer writer.Close()

	for k, v := range fields {
		select {
		case <-ctx.Done():
			_ = pw.CloseWithError(ctx.Err())
			return
		default:
			_ = writer.WriteField(k, v)
		}
	}

	for key, r := range files {
		select {
		case <-ctx.Done():
			_ = pw.CloseWithError(ctx.Err())
			return
		default:
			contentType, streamReader := detectMIMEAndReader(r)

			part, err := createFormFileHeader(writer, key, key, contentType)
			if err == nil {
				_, _ = io.CopyZeroAlloc(part, streamReader)
			}
		}
	}
}

func detectMIMEAndReader(r stdio.Reader) (string, stdio.Reader) {
	var buf [512]byte

	n, err := stdio.ReadFull(r, buf[:])
	if n > 0 {
		contentType := http.DetectContentType(buf[:n])
		reader := stdio.MultiReader(bytes.NewReader(buf[:n]), r)

		return contentType, reader
	}

	if err != nil && !errors.Is(err, stdio.EOF) && !errors.Is(err, stdio.ErrUnexpectedEOF) {
		return "application/octet-stream", r
	}

	return "application/octet-stream", r
}

func createFormFileHeader(w *multipart.Writer, fieldname, filename, contentType string) (stdio.Writer, error) {
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition",
		fmt.Sprintf(`form-data; name="%s"; filename="%s"`, escapeQuotes(fieldname), escapeQuotes(filename)))

	if contentType != "" {
		h.Set("Content-Type", contentType)
	} else {
		h.Set("Content-Type", "application/octet-stream")
	}

	return w.CreatePart(h)
}

func escapeQuotes(s string) string {
	return quoteEscaper.Replace(s)
}

// WithFormValues constructs an [aoni.RequestModifier] encoding [url.Values] into the request body as application/x-www-form-urlencoded.
func WithFormValues(values url.Values) aoni.RequestModifier {
	return func(req aoni.Request) {
		encoded := values.Encode()
		req.SetBodyBytes(bytesconv.S2B(encoded))
		req.SetHeader("Content-Type", "application/x-www-form-urlencoded")
	}
}

// WithFormBody constructs an [aoni.RequestModifier] encoding a struct or map into URL-encoded form values.
func WithFormBody(payload any) aoni.RequestModifier {
	return func(req aoni.Request) {
		if payload == nil {
			return
		}

		if r, ok := payload.(stdio.Reader); ok {
			req.SetBodyStream(r, -1)
			req.SetHeader("Content-Type", "application/x-www-form-urlencoded")
			return
		}

		encoder := values.StructToValues
		if cfg := aoni.GetRequestConfig(req.Context()); cfg != nil && cfg.QueryEncoder != nil {
			encoder = cfg.QueryEncoder
		}

		vals, err := encoder(payload)
		if err != nil {
			aoni.GetOrInitRequestConfig(req).BodyError = err
			return
		}

		encoded := vals.Encode()
		req.SetBodyBytes(bytesconv.S2B(encoded))
		req.SetHeader("Content-Type", "application/x-www-form-urlencoded")
	}
}

// ============================================================================
// 5. PROTOCOL & NETWORK LAYER MODIFIERS
// ============================================================================

// WithOrderedHeaders constructs an [aoni.RequestModifier] setting HTTP/1.1 wire header serialization sequence.
func WithOrderedHeaders(headers []string) aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).OrderedHeaders = headers
	}
}

// WithALPN constructs an [aoni.RequestModifier] overriding negotiated ALPN protocols for TLS handshakes.
func WithALPN(protos ...string) aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).ALPNOverride = protos
	}
}

// WithoutAltSvc constructs an [aoni.RequestModifier] that disables Alt-Svc connection
// upgrades and IP pooling for a request, forcing direct resolution over a fresh socket.
func WithoutAltSvc() aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).DisableAltSvc = true
	}
}

// WithForceHTTP1 constructs an [aoni.RequestModifier] restricting ALPN negotiation strictly to HTTP/1.1.
func WithForceHTTP1() aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).ALPNOverride = []string{aoni.AlpnHTTP}
	}
}

// WithForceHTTP2 constructs an [aoni.RequestModifier] restricting ALPN negotiation strictly to HTTP/2.
func WithForceHTTP2() aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).ALPNOverride = []string{aoni.AlpnH2}
	}
}

// WithForceHTTP3 constructs an [aoni.RequestModifier] restricting ALPN negotiation strictly to HTTP/3.
func WithForceHTTP3() aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).ALPNOverride = []string{aoni.AlpnH3}
	}
}

// Without0RTT constructs an [aoni.RequestModifier] that disables TLS 1.3 / QUIC 0-RTT
// Early Data for a request, forcing standard 1-RTT handshake negotiation.
func Without0RTT() aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).Disable0RTT = true
	}
}

// WithTCPDelay constructs an [aoni.RequestModifier] adding randomized jitter delays prior to TCP socket dialing.
func WithTCPDelay(min, max time.Duration) aoni.RequestModifier {
	minDelay, maxDelay := min, max
	if minDelay > maxDelay {
		minDelay, maxDelay = maxDelay, minDelay
	}

	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).TCPDelay = aoni.TCPDelayRange{Min: minDelay, Max: maxDelay}
	}
}

// WithHappyEyeballs constructs an [aoni.RequestModifier] configuring IPv4/IPv6 stagger delays for request execution.
func WithHappyEyeballs(delay time.Duration) aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).HappyEyeballsDelay = delay
	}
}

// WithProxyDNS constructs an [aoni.RequestModifier] routing DNS resolutions through SOCKS5 or HTTP CONNECT proxies.
func WithProxyDNS() aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).ProxyDNS = true
	}
}

// WithProxyOverride constructs an [aoni.RequestModifier] routing request traffic through a target proxy URL.
func WithProxyOverride(rawURL string) aoni.RequestModifier {
	return func(req aoni.Request) {
		if u, err := url.Parse(rawURL); err == nil {
			aoni.GetOrInitRequestConfig(req).ProxyAddr = u
		}
	}
}

// WithSSRFGuard constructs an [aoni.RequestModifier] enabling SSRF protections against loopback and private IP addresses.
func WithSSRFGuard() aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).SSRFGuard = true
	}
}

// WithInsecureSkipVerify constructs an [aoni.RequestModifier] bypassing TLS peer certificate verification for the request.
func WithInsecureSkipVerify() aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).InsecureSkipVerify = true
	}
}

// WithFragmentation constructs an [aoni.RequestModifier] configuring TCP packet fragmentation parameters.
func WithFragmentation(cfg fragment.Config) aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).Fragment = &cfg
	}
}

// WithFragment is an alias for [WithFragmentation].
func WithFragment(cfg fragment.Config) aoni.RequestModifier {
	return WithFragmentation(cfg)
}

// WithHostRewrite constructs an [aoni.RequestModifier] replacing host DNS remapping rules for the request.
func WithHostRewrite(rules map[string]string) aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).HostRewrite = &aoni.HostRewriteConfig{Rules: rules}
	}
}

// WithAppendHostRewrite constructs an [aoni.RequestModifier] appending new DNS remapping rules to existing request settings.
func WithAppendHostRewrite(rules map[string]string) aoni.RequestModifier {
	return func(req aoni.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)

		newRules := make(map[string]string, len(rules))
		if cfg.HostRewrite != nil && cfg.HostRewrite.Rules != nil {
			maps.Copy(newRules, cfg.HostRewrite.Rules)
		}

		maps.Copy(newRules, rules)
		cfg.HostRewrite = &aoni.HostRewriteConfig{Rules: newRules}
	}
}

// WithSocketController constructs an [aoni.RequestModifier] assigning a low-level socket controller callback.
func WithSocketController(controller aoni.SocketController) aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).SocketController = controller
	}
}

// WithP0fSignature constructs an [aoni.RequestModifier] setting p0f TCP stack signature parameters.
func WithP0fSignature(sig *p0f.Signature) aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).P0fSignature = sig
	}
}

// WithSessionCache constructs an [aoni.RequestModifier] assigning an isolated proxy-aware TLS [aoni.SessionCache].
func WithSessionCache(cache aoni.SessionCache) aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).SessionCache = cache
	}
}

// WithCertificatePin constructs an [aoni.RequestModifier] pinning SHA-256 public key hashes for target domains.
func WithCertificatePin(domain, hash string) aoni.RequestModifier {
	return func(req aoni.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)
		if cfg.CertificatePins == nil {
			cfg.CertificatePins = make(map[string][]string)
		}

		cfg.CertificatePins[domain] = append(cfg.CertificatePins[domain], hash)
	}
}

// WithPadding constructs an [aoni.RequestModifier] injecting random packet padding headers to confuse DPI length analysis.
func WithPadding(cfg fingerprint.PaddingConfig) aoni.RequestModifier {
	return func(req aoni.Request) {
		if padding := fingerprint.GeneratePadding(cfg); len(padding) > 0 {
			headerName := fingerprint.PaddingHeaderName(cfg)
			req.SetHeader(headerName, hex.EncodeToString(padding))
		}
	}
}

// ============================================================================
// 6. PIPELINE, RESILIENCE & CONTEXT MODIFIERS
// ============================================================================

// WithContext constructs an [aoni.RequestModifier] updating the execution context associated with the request.
func WithContext(ctx context.Context) aoni.RequestModifier {
	return func(req aoni.Request) {
		req.SetContext(ctx)
	}
}

// WithTimeout constructs an [aoni.RequestModifier] attaching a deadline timeout to the request context.
func WithTimeout(d time.Duration) aoni.RequestModifier {
	return func(req aoni.Request) {
		ctx, cancel := context.WithTimeout(req.Context(), d) //nolint:gosec
		req.SetContext(ctx)
		aoni.GetOrInitRequestConfig(req).RequestTimeoutCancel = cancel
	}
}

// WithPipeline constructs an [aoni.RequestModifier] overriding execution pipeline configurations for the request.
func WithPipeline(pipe aoni.PipelineConfig) aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).Pipeline = &pipe
	}
}

// PhaseID identifies fixed transaction execution phases.
type PhaseID = pipeline.PhaseID

const (
	PhasePrep        = pipeline.PhasePrep
	PhaseCacheLookup = pipeline.PhaseCacheLookup
	PhaseDispatch    = pipeline.PhaseDispatch
	PhaseDecompress  = pipeline.PhaseDecompress
	PhaseWAF         = pipeline.PhaseWAF
	PhaseValidate    = pipeline.PhaseValidate
	PhaseCacheSave   = pipeline.PhaseCacheSave
)

// WithUnsafePhaseOrder sets a custom phase order for the pipeline.
func WithUnsafePhaseOrder(phases ...PhaseID) aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).UnsafePhaseOrder = phases
	}
}

// WithUnsafeDisableFlags allows to disable pipeline phases instantly (by clearing bits in 1 CPU cycle).
// Example: mod.WithUnsafeDisableFlags(pipeline.FlagChallenge | pipeline.FlagCache)
func WithUnsafeDisableFlags(flags uint32) aoni.RequestModifier {
	return func(req aoni.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)
		cfg.DisabledFlags |= flags
	}
}

// WithUnsafeHook inserts a zero-allocation hook before the specified pipeline phase.
func WithUnsafeHook(phase pipeline.PhaseID, hook pipeline.UnsafeHook) aoni.RequestModifier {
	return func(req aoni.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)
		if cfg.UnsafeHooks == nil {
			cfg.UnsafeHooks = make(map[pipeline.PhaseID][]pipeline.UnsafeHook)
		}

		cfg.UnsafeHooks[phase] = append(cfg.UnsafeHooks[phase], hook)
	}
}

// WithRetryPolicy constructs an [aoni.RequestModifier] assigning custom retry parameters to the request.
func WithRetryPolicy(override aoni.RetryOverride) aoni.RequestModifier {
	policy := override
	if policy.MaxAttempts < 1 {
		policy.MaxAttempts = 1
	}

	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).RetryPolicy = &policy
	}
}

// WithAllowNonReadOnlyHedging constructs an [aoni.RequestModifier] permitting request hedging for non-idempotent HTTP methods.
func WithAllowNonReadOnlyHedging(allow bool) aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).AllowNonReadOnlyHedging = allow
	}
}

// WithFallback constructs an [aoni.RequestModifier] registering an alternative response fallback generator.
func WithFallback(f aoni.FallbackFunc) aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).Fallback = f
	}
}

// WithResponseValidator constructs an [aoni.RequestModifier] attaching custom response validation predicates.
func WithResponseValidator(fn func(resp *http.Response) error) aoni.RequestModifier {
	return func(req aoni.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)

		existing := cfg.ResponseValidator
		if existing != nil {
			cfg.ResponseValidator = func(resp *http.Response) error {
				if err := existing(resp); err != nil {
					return err
				}

				return fn(resp)
			}

			return
		}

		cfg.ResponseValidator = fn
	}
}

// WithMultiReadThreshold constructs an [aoni.RequestModifier] configuring RAM buffering bounds for replayable reads.
func WithMultiReadThreshold(threshold int64) aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).MultiReadThreshold = threshold
	}
}

// WithMultiReadDisableDisk constructs an [aoni.RequestModifier] disabling temporary file disk backing on buffer overflows.
func WithMultiReadDisableDisk(disable bool) aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).MultiReadDisableDisk = disable
	}
}

// WithCacheTTL constructs an [aoni.RequestModifier] configuring custom response caching TTL for the request.
func WithCacheTTL(ttl time.Duration) aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).CacheTTL = ttl
	}
}

// WithRedact constructs an [aoni.RequestModifier] configuring header and key redaction rules for logging.
func WithRedact(cfg aoni.RedactConfig) aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).Redact = &cfg
	}
}

// WithConnMetadata constructs an [aoni.RequestModifier] associating custom key-value metadata with the request connection.
func WithConnMetadata(key string, val any) aoni.RequestModifier {
	return func(req aoni.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)
		if cfg.Metadata == nil {
			cfg.Metadata = make(map[string]any)
		}

		cfg.Metadata[key] = val
	}
}

// WithForceContentType constructs an [aoni.RequestModifier] forcing response decoding via a specific MIME type.
func WithForceContentType(mime string) aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).ForceContentType = mime
	}
}

// WithErrorModel constructs an [aoni.RequestModifier] assigning a target struct pointer for non-2xx API error response unmarshaling.
func WithErrorModel(model any) aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).ErrorModel = model
	}
}

// WithDecoder constructs an [aoni.RequestModifier] overriding the response decoder implementation for the request.
func WithDecoder(d aoni.ResponseDecoder) aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).Decoder = d
	}
}

// WithUploadProgress constructs an [aoni.RequestModifier] registering an upload progress tracking callback.
func WithUploadProgress(progress aoni.ProgressFunc) aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).UploadProgress = progress
	}
}

// WithDownloadProgress constructs an [aoni.RequestModifier] registering a download progress tracking callback.
func WithDownloadProgress(progress aoni.ProgressFunc) aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).DownloadProgress = progress
	}
}

// WithCaptureResponse constructs an [aoni.RequestModifier] capturing a reference pointer to the raw [*http.Response].
func WithCaptureResponse(target any) aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).Capturer = target
	}
}

// ============================================================================
// 7. TELEMETRY, TRACING & DIAGNOSTICS MODIFIERS
// ============================================================================

// WithCorrelationID constructs an [aoni.RequestModifier] setting an end-to-end tracing correlation ID header ("X-Correlation-ID").
func WithCorrelationID(id string) aoni.RequestModifier {
	activeID := id
	if activeID == "" {
		activeID = telemetry.GenerateCorrelationID()
	}

	return func(req aoni.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)
		if cfg.TraceInfo != nil {
			cfg.TraceInfo.CorrelationID = activeID
		}

		req.SetHeader("X-Correlation-ID", activeID)
	}
}

// WithLabel constructs an [aoni.RequestModifier] assigning a route or metric label to the request context.
func WithLabel(label string) aoni.RequestModifier {
	return func(req aoni.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)
		cfg.Label = label

		if cfg.TraceInfo != nil {
			cfg.TraceInfo.Label = label
		}
	}
}

// WithDebug constructs an [aoni.RequestModifier] marking the request for verbose diagnostic logging.
func WithDebug() aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).Debug = true
	}
}

// WithCurlDump constructs an [aoni.RequestModifier] printing an equivalent shell-escaped cURL command to stderr.
func WithCurlDump() aoni.RequestModifier {
	return func(req aoni.Request) {
		stdReq := req.HTTPRequest()
		if stdReq != nil {
			dumpStdRequest(stdReq)
			return
		}

		dumpGenericRequest(req)
	}
}

func dumpStdRequest(stdReq *http.Request) {
	var body []byte
	if stdReq.Body != nil && stdReq.Body != http.NoBody {
		var buf bytes.Buffer

		_, _ = io.CopyZeroAlloc(&buf, stdReq.Body)
		body = buf.Bytes()
		stdReq.Body = stdio.NopCloser(bytes.NewReader(body))
	}

	curl := telemetry.CurlFromRequest(stdReq, body)
	fmt.Fprintf(os.Stderr, "%s\n", curl)
}

func dumpGenericRequest(req aoni.Request) {
	body := req.BodyBytes()

	dummyReq, _ := http.NewRequest(req.Method(), req.URL(), bytes.NewReader(body)) //nolint:noctx
	if dummyReq != nil {
		curl := telemetry.CurlFromRequest(dummyReq, body)
		fmt.Fprintf(os.Stderr, "%s\n", curl)
	}
}

// WithTrace constructs an [aoni.RequestModifier] assigning a connection tracer container to capture connection metrics.
func WithTrace(target *telemetry.TraceInfo) aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).TraceInfo = target
	}
}

// WithTraceJA4 constructs an [aoni.RequestModifier] enabling JA4/JA4H client fingerprint telemetry.
func WithTraceJA4(target *telemetry.TraceInfo) aoni.RequestModifier {
	return func(req aoni.Request) {
		store := &aoni.JA4ReportStore{Target: target}

		aoni.GetOrInitRequestConfig(req).JA4ReportStore = store
		if stdReq := req.HTTPRequest(); stdReq != nil {
			target.JA4 = &ja4.Report{JA4H: telemetry.ComputeJA4HFromRequest(stdReq)}
		}
	}
}

// WithTraceContext constructs an [aoni.RequestModifier] attaching a new [telemetry.TraceInfo] container to the request context.
func WithTraceContext() aoni.RequestModifier {
	return func(req aoni.Request) {
		info := &telemetry.TraceInfo{}
		aoni.GetOrInitRequestConfig(req).TraceInfo = info
		WithTraceJA4(info)(req)
	}
}

// WithJA4Callback constructs an [aoni.RequestModifier] setting a callback executed with the computed [ja4.Report] after TLS handshakes.
func WithJA4Callback(fn func(ja4.Report)) aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).JA4Callback = fn
	}
}

// WithClientHelloSpecProvider constructs an [aoni.RequestModifier] assigning a dynamic uTLS spec provider.
func WithClientHelloSpecProvider(provider aoni.ClientHelloSpecProvider) aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).ClientHelloSpecProvider = provider
	}
}
