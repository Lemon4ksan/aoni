// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package mod provides declarative request modifiers for customizing an [http.Request] prior to dispatch.
package mod

import (
	"bytes"
	"context"
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
	"github.com/lemon4ksan/aoni/fingerprint"
	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/fingerprint/p0f"
	"github.com/lemon4ksan/aoni/internal/io"
	"github.com/lemon4ksan/aoni/telemetry"
)

// ErrBodyNotSeekable indicates that the request body cannot be rewound for hedging or retries.
var ErrBodyNotSeekable = errors.New("aoni: body does not support seeking for hedging")

// WithVar replaces a single placeholder (e.g. "{key}") in the path with an escaped value.
func WithVar(key string, value any) aoni.RequestModifier {
	return func(req *http.Request) {
		placeholder := "{" + key + "}"
		escapedValue := url.PathEscape(fmt.Sprint(value))

		req.URL.Path = strings.ReplaceAll(req.URL.Path, placeholder, escapedValue)
		if req.URL.RawPath != "" {
			req.URL.RawPath = strings.ReplaceAll(req.URL.RawPath, placeholder, escapedValue)
		}
	}
}

// WithVars replaces multiple placeholder keys in the path with their respective values.
func WithVars(pairs ...any) aoni.RequestModifier {
	return func(req *http.Request) {
		if len(pairs)%2 != 0 {
			return
		}

		for i := 0; i < len(pairs); i += 2 {
			key := fmt.Sprint(pairs[i])
			value := fmt.Sprint(pairs[i+1])
			WithVar(key, value)(req)
		}
	}
}

// WithQuery encodes query parameters directly into the URL RawQuery string without intermediate map allocations.
//
// Delegates to custom [aoni.QueryEncoder] if configured in the request context;
// otherwise uses high-performance zero-allocation encoding via [values.StructToQueryString].
func WithQuery(query any) aoni.RequestModifier {
	return func(req *http.Request) {
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

		if req.URL.RawQuery == "" {
			req.URL.RawQuery = qStr
		} else {
			req.URL.RawQuery += "&" + qStr
		}
	}
}

func resolveQueryString(req *http.Request, query any) (string, error) {
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

// WithHeader sets the key header field to the given value.
func WithHeader(key, value string) aoni.RequestModifier {
	return func(req *http.Request) {
		req.Header.Set(key, value)
	}
}

// WithHeaders sets new key-value pairs in request headers.
func WithHeaders(headers map[string]string) aoni.RequestModifier {
	return func(req *http.Request) {
		for k, v := range headers {
			req.Header.Set(k, v)
		}
	}
}

// ResetHeaders clears all headers from the request.
func ResetHeaders() aoni.RequestModifier {
	return func(req *http.Request) {
		req.Header = make(http.Header)
	}
}

// WithBearer applies a Bearer Token authorization header.
func WithBearer(token string) aoni.RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

// WithBasicAuth applies Basic Authorization credentials.
func WithBasicAuth(username, password string) aoni.RequestModifier {
	return func(req *http.Request) {
		req.SetBasicAuth(username, password)
	}
}

// WithUserAgent overrides the standard User-Agent header field.
func WithUserAgent(ua string) aoni.RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("User-Agent", ua)
	}
}

// WithContentType overrides the standard Content-Type header field.
func WithContentType(ct string) aoni.RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("Content-Type", ct)
	}
}

// WithAccept overrides the standard Accept header field.
func WithAccept(accept string) aoni.RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("Accept", accept)
	}
}

// WithCookie attaches a single cookie to the request.
func WithCookie(c *http.Cookie) aoni.RequestModifier {
	return func(req *http.Request) {
		req.AddCookie(c)
	}
}

// WithCookies attaches multiple cookies from a key-value map.
func WithCookies(kv map[string]string) aoni.RequestModifier {
	return func(req *http.Request) {
		for k, v := range kv {
			req.AddCookie(&http.Cookie{Name: k, Value: v}) //nolint:gosec
		}
	}
}

// WithBody replaces the request body stream with the provided reader.
func WithBody(r stdio.Reader) aoni.RequestModifier {
	return func(req *http.Request) {
		rc, ok := r.(stdio.ReadCloser)
		if !ok && r != nil {
			rc = stdio.NopCloser(r)
		}

		req.Body = rc

		if r != nil {
			if b, ok := r.(interface{ Len() int }); ok {
				req.ContentLength = int64(b.Len())
			} else if s, ok := r.(interface{ Len() int64 }); ok {
				req.ContentLength = s.Len()
			}
		}

		if r != nil {
			req.GetBody = func() (stdio.ReadCloser, error) {
				if seeker, ok := r.(stdio.Seeker); ok {
					if _, err := seeker.Seek(0, stdio.SeekStart); err != nil {
						return nil, err
					}

					return stdio.NopCloser(r), nil
				}

				return nil, ErrBodyNotSeekable
			}
		}
	}
}

// WithJSONBody serializes payload as JSON, sets the request body, and adds Content-Type: application/json.
func WithJSONBody(payload any) aoni.RequestModifier {
	return func(req *http.Request) {
		bodyBytes, err := json.Marshal(payload)
		if err != nil {
			aoni.GetOrInitRequestConfig(req).BodyError = err
			return
		}

		req.Body = stdio.NopCloser(bytes.NewReader(bodyBytes))
		req.ContentLength = int64(len(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		req.GetBody = func() (stdio.ReadCloser, error) {
			return stdio.NopCloser(bytes.NewReader(bodyBytes)), nil
		}
	}
}

// WithProtoBody serializes payload as binary Protocol Buffer bytes.
func WithProtoBody(msg proto.Message) aoni.RequestModifier {
	return func(req *http.Request) {
		if msg == nil {
			return
		}

		bodyBytes, err := proto.Marshal(msg)
		if err != nil {
			aoni.GetOrInitRequestConfig(req).BodyError = err
			return
		}

		req.Body = stdio.NopCloser(bytes.NewReader(bodyBytes))
		req.ContentLength = int64(len(bodyBytes))
		req.Header.Set("Content-Type", "application/x-protobuf")
		req.Header.Set("Accept", "application/x-protobuf")

		req.GetBody = func() (stdio.ReadCloser, error) {
			return stdio.NopCloser(bytes.NewReader(bodyBytes)), nil
		}
	}
}

// WithGRPCWebBody serializes payload as gRPC-Web framed Protocol Buffer bytes.
func WithGRPCWebBody(msg proto.Message) aoni.RequestModifier {
	return func(req *http.Request) {
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

		req.Body = stdio.NopCloser(bytes.NewReader(frame))
		req.ContentLength = int64(len(frame))
		req.Header.Set("Content-Type", "application/grpc-web+proto")
		req.Header.Set("Accept", "application/grpc-web+proto")
		req.Header.Set("X-Grpc-Web", "1")
		req.Header.Set("X-User-Agent", "grpc-web-javascript/0.1")

		req.GetBody = func() (stdio.ReadCloser, error) {
			return stdio.NopCloser(bytes.NewReader(frame)), nil
		}
	}
}

// WithMultipart builds a multipart/form-data payload from form fields and stream readers.
//
// Streams files into the body buffer via [io.CopyZeroAlloc] to eliminate allocations.
func WithMultipart(fields map[string]string, files map[string]stdio.Reader) aoni.RequestModifier {
	return func(req *http.Request) {
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

		req.Body = stdio.NopCloser(body)
		req.ContentLength = int64(body.Len())
		req.Header.Set("Content-Type", writer.FormDataContentType())
	}
}

// WithOrigin overrides the standard Origin header field.
func WithOrigin(origin string) aoni.RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("Origin", origin)
	}
}

// WithFormValues merges url.Values into the request body as form-urlencoded.
func WithFormValues(values url.Values) aoni.RequestModifier {
	return func(req *http.Request) {
		encoded := values.Encode()
		req.Body = stdio.NopCloser(strings.NewReader(encoded))
		req.ContentLength = int64(len(encoded))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		req.GetBody = func() (stdio.ReadCloser, error) {
			return stdio.NopCloser(strings.NewReader(encoded)), nil
		}
	}
}

// WithFormBody serializes payload as URL-encoded form values into the request body.
func WithFormBody(payload any) aoni.RequestModifier {
	return func(req *http.Request) {
		if payload == nil {
			return
		}

		if r, ok := payload.(stdio.Reader); ok {
			rc, ok := r.(stdio.ReadCloser)
			if !ok {
				rc = stdio.NopCloser(r)
			}

			req.Body = rc
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

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
		req.Body = stdio.NopCloser(strings.NewReader(encoded))
		req.ContentLength = int64(len(encoded))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		req.GetBody = func() (stdio.ReadCloser, error) {
			return stdio.NopCloser(strings.NewReader(encoded)), nil
		}
	}
}

// WithFallback registers a request-level fallback response generator.
func WithFallback(f aoni.FallbackFunc) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).Fallback = f
	}
}

// WithDebug tags the request for verbose diagnostic logging.
func WithDebug() aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).Debug = true
	}
}

// WithDecoder overrides the response Decoder for this request.
func WithDecoder(d any) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).Decoder = d
	}
}

// WithForceContentType forces automatic response decoding using the specified MIME type.
func WithForceContentType(mime string) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).ForceContentType = mime
	}
}

// WithErrorModel sets the target struct/map for non-2xx response decoding.
func WithErrorModel(model any) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).ErrorModel = model
	}
}

// WithUploadProgress wraps the request body to trigger progress callbacks during uploads.
func WithUploadProgress(progress aoni.ProgressFunc) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).UploadProgress = progress
	}
}

// WithDownloadProgress triggers periodic callbacks during response reads.
func WithDownloadProgress(progress aoni.ProgressFunc) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).DownloadProgress = progress
	}
}

// WithCaptureResponse captures a pointer reference to the raw http.Response.
func WithCaptureResponse(target any) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).Capturer = target
	}
}

// WithCorrelationID assigns an end-to-end tracing Correlation ID to the request.
func WithCorrelationID(id string) aoni.RequestModifier {
	activeID := id
	if activeID == "" {
		activeID = telemetry.GenerateCorrelationID()
	}

	return func(req *http.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)
		if cfg.TraceInfo != nil {
			cfg.TraceInfo.CorrelationID = activeID
		}

		req.Header.Set("X-Correlation-ID", activeID)
	}
}

// WithLabel attaches a route or metric label to the request.
func WithLabel(label string) aoni.RequestModifier {
	return func(req *http.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)
		cfg.Label = label

		if cfg.TraceInfo != nil {
			cfg.TraceInfo.Label = label
		}
	}
}

// WithAllowNonReadOnlyHedging permits request hedging for non-idempotent HTTP methods.
func WithAllowNonReadOnlyHedging(allow bool) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).AllowNonReadOnlyHedging = allow
	}
}

// WithOrderedHeaders sets HTTP/1.1 header wire serialization order.
func WithOrderedHeaders(headers []string) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).OrderedHeaders = headers
	}
}

// WithALPN overrides negotiated ALPN protocols for TLS handshakes.
func WithALPN(protos ...string) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).ALPNOverride = protos
	}
}

var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

func escapeQuotes(s string) string {
	return quoteEscaper.Replace(s)
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

// WithStreamingMultipart streams a multipart/form-data body via an [stdio.Pipe] using zero-copy transfers.
func WithStreamingMultipart(fields map[string]string, files map[string]stdio.Reader) aoni.RequestModifier {
	return func(req *http.Request) {
		pr, pw := stdio.Pipe()

		writer := multipart.NewWriter(pw)
		if cfg := aoni.GetOrInitRequestConfig(req); cfg.MultipartBoundary != "" {
			_ = writer.SetBoundary(cfg.MultipartBoundary)
		}

		ctx := req.Context()

		go func() {
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
		}()

		req.Body = pr
		req.Header.Set("Content-Type", writer.FormDataContentType())
	}
}

// WithMultiReadThreshold sets the multi-read RAM threshold in bytes for a single request.
func WithMultiReadThreshold(threshold int64) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).MultiReadThreshold = threshold
	}
}

// WithMultiReadDisableDisk disables disk fallback when multi-read threshold is exceeded.
func WithMultiReadDisableDisk(disable bool) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).MultiReadDisableDisk = disable
	}
}

// WithProxyOverride routes this request through a custom proxy URL.
func WithProxyOverride(rawURL string) aoni.RequestModifier {
	return func(req *http.Request) {
		if u, err := url.Parse(rawURL); err == nil {
			aoni.GetOrInitRequestConfig(req).ProxyAddr = u
		}
	}
}

// WithInsecureSkipVerify disables TLS certificate verification for this request.
func WithInsecureSkipVerify() aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).InsecureSkipVerify = true
	}
}

// WithTCPDelay adds random jitter delay before TCP dial.
func WithTCPDelay(min, max time.Duration) aoni.RequestModifier {
	minDelay, maxDelay := min, max
	if minDelay > maxDelay {
		minDelay, maxDelay = maxDelay, minDelay
	}

	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).TCPDelay = aoni.TCPDelayRange{Min: minDelay, Max: maxDelay}
	}
}

// WithConnMetadata associates user-defined metadata with the connection.
func WithConnMetadata(key string, val any) aoni.RequestModifier {
	return func(req *http.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)
		if cfg.Metadata == nil {
			cfg.Metadata = make(map[string]any)
		}

		cfg.Metadata[key] = val
	}
}

// WithCacheTTL sets caching TTL for this request.
func WithCacheTTL(ttl time.Duration) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).CacheTTL = ttl
	}
}

// WithRetryPolicy overrides retry logic for this request.
func WithRetryPolicy(override aoni.RetryOverride) aoni.RequestModifier {
	policy := override
	if policy.MaxAttempts < 1 {
		policy.MaxAttempts = 1
	}

	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).RetryPolicy = &policy
	}
}

// WithSSRFGuard enforces SSRF guard for this request.
func WithSSRFGuard() aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).SSRFGuard = true
	}
}

// WithHappyEyeballs sets Happy Eyeballs delay for this request.
func WithHappyEyeballs(delay time.Duration) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).HappyEyeballsDelay = delay
	}
}

// WithProxyDNS routes DNS lookups through proxy.
func WithProxyDNS() aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).ProxyDNS = true
	}
}

// WithP0fSignature sets p0f TCP fingerprint signature.
func WithP0fSignature(sig *p0f.Signature) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).P0fSignature = sig
	}
}

// WithSessionCache sets TLS session cache.
func WithSessionCache(cache aoni.SessionCache) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).SessionCache = cache
	}
}

// WithCurlDump dumps the equivalent cURL command to stderr using zero-copy body buffering.
func WithCurlDump() aoni.RequestModifier {
	return func(req *http.Request) {
		var body []byte

		if req.Body != nil && req.Body != http.NoBody {
			var buf bytes.Buffer

			_, _ = io.CopyZeroAlloc(&buf, req.Body)
			body = buf.Bytes()
			req.Body = stdio.NopCloser(bytes.NewReader(body))
		}

		curl := telemetry.CurlFromRequest(req, body)
		fmt.Fprintf(os.Stderr, "%s\n", curl)
	}
}

// WithPadding adds random packet padding headers to the request.
func WithPadding(cfg fingerprint.PaddingConfig) aoni.RequestModifier {
	return func(req *http.Request) {
		if padding := fingerprint.GeneratePadding(cfg); len(padding) > 0 {
			headerName := fingerprint.PaddingHeaderName(cfg)
			req.Header.Set(headerName, hex.EncodeToString(padding))
		}
	}
}

// WithTrace registers a connection tracer on the active request.
func WithTrace(target *telemetry.TraceInfo) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).TraceInfo = target
	}
}

// WithTraceJA4 populates JA4/JA4H telemetry fields for the active request.
func WithTraceJA4(target *telemetry.TraceInfo) aoni.RequestModifier {
	return func(req *http.Request) {
		store := &aoni.JA4ReportStore{Target: target}
		aoni.GetOrInitRequestConfig(req).JA4ReportStore = store
		target.JA4 = &ja4.Report{JA4H: telemetry.ComputeJA4HFromRequest(req)}
	}
}

// WithSocketController sets socket interception controller hook.
func WithSocketController(controller aoni.SocketController) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).SocketController = controller
	}
}

// WithClientHelloSpecProvider sets dynamic uTLS ClientHello spec provider.
func WithClientHelloSpecProvider(provider aoni.ClientHelloSpecProvider) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).ClientHelloSpecProvider = provider
	}
}

// WithJA4Callback sets callback for computed JA4 report.
func WithJA4Callback(fn func(ja4.Report)) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).JA4Callback = fn
	}
}

// WithTraceContext attaches a new [telemetry.TraceInfo] container to the request context.
func WithTraceContext() aoni.RequestModifier {
	return func(req *http.Request) {
		info := &telemetry.TraceInfo{}
		aoni.GetOrInitRequestConfig(req).TraceInfo = info
		WithTraceJA4(info)(req)
	}
}

// WithFragmentation sets packet fragmentation configuration.
func WithFragmentation(cfg aoni.FragmentConfig) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).Fragment = &cfg
	}
}

// WithHostRewrite sets DNS rewrite rules.
func WithHostRewrite(rules map[string]string) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).HostRewrite = &aoni.HostRewriteConfig{Rules: rules}
	}
}

// WithAppendHostRewrite appends new DNS rewrite rules to the existing request configuration.
func WithAppendHostRewrite(rules map[string]string) aoni.RequestModifier {
	return func(req *http.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)

		newRules := make(map[string]string)
		if cfg.HostRewrite != nil && cfg.HostRewrite.Rules != nil {
			maps.Copy(newRules, cfg.HostRewrite.Rules)
		}

		maps.Copy(newRules, rules)
		cfg.HostRewrite = &aoni.HostRewriteConfig{Rules: newRules}
	}
}

// WithResponseValidator attaches a response validation function.
func WithResponseValidator(fn func(resp *http.Response) error) aoni.RequestModifier {
	return func(req *http.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)

		existing := cfg.ResponseValidator
		if existing != nil {
			cfg.ResponseValidator = func(resp *http.Response) error {
				if err := existing(resp); err != nil {
					return err
				}

				return fn(resp)
			}
		} else {
			cfg.ResponseValidator = fn
		}
	}
}

// WithPipeline overrides pipeline config for this request.
func WithPipeline(pipe aoni.PipelineConfig) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).Pipeline = &pipe
	}
}

// WithFragment configures packet fragmentation.
func WithFragment(cfg aoni.FragmentConfig) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).Fragment = &cfg
	}
}

// WithRedact configures sensitive header redaction rules.
func WithRedact(cfg aoni.RedactConfig) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).Redact = &cfg
	}
}

// WithCertificatePin pins SHA-256 certificate public key hashes.
func WithCertificatePin(domain, hash string) aoni.RequestModifier {
	return func(req *http.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)
		if cfg.CertificatePins == nil {
			cfg.CertificatePins = make(map[string][]string)
		}

		cfg.CertificatePins[domain] = append(cfg.CertificatePins[domain], hash)
	}
}

// WithForceHTTP1 advertises HTTP/1.1 only in ALPN.
func WithForceHTTP1() aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).ALPNOverride = []string{aoni.AlpnHTTP}
	}
}

// WithForceHTTP2 advertises HTTP/2 only in ALPN.
func WithForceHTTP2() aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).ALPNOverride = []string{"h2"}
	}
}

// WithForceHTTP3 advertises HTTP/3 only in ALPN.
func WithForceHTTP3() aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).ALPNOverride = []string{"h3"}
	}
}

// WithIfNoneMatch sets the If-None-Match header.
func WithIfNoneMatch(etag string) aoni.RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("If-None-Match", etag)
	}
}

// WithIfMatch sets the If-Match header.
func WithIfMatch(etag string) aoni.RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("If-Match", etag)
	}
}

// WithIfModifiedSince sets the If-Modified-Since header.
func WithIfModifiedSince(t time.Time) aoni.RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("If-Modified-Since", t.UTC().Format(http.TimeFormat))
	}
}

// WithContext sets context on the request.
func WithContext(ctx context.Context) aoni.RequestModifier {
	return func(req *http.Request) {
		*req = *req.WithContext(ctx)
	}
}

// WithTimeout sets a context deadline timeout for the request.
func WithTimeout(d time.Duration) aoni.RequestModifier {
	return func(req *http.Request) {
		ctx, cancel := context.WithTimeout(req.Context(), d) //nolint:gosec
		*req = *req.WithContext(ctx)
		aoni.GetOrInitRequestConfig(req).RequestTimeoutCancel = cancel
	}
}
