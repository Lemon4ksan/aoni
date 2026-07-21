// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package mod provides request modifiers for customizing an [http.Request] before execution.
package mod

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec"
	"github.com/lemon4ksan/aoni/ja4"
	"github.com/lemon4ksan/aoni/p0f"
	"github.com/lemon4ksan/aoni/values"
)

// Modifier is a type alias for [aoni.RequestModifier].
type Modifier = aoni.RequestModifier

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
// It accepts alternating key-value arguments.
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

// WithQuery encodes a struct or map as URL query parameters and appends them to the request URL.
func WithQuery(query any) aoni.RequestModifier {
	return func(req *http.Request) {
		if query == nil {
			return
		}

		encoder := values.StructToValues
		if cfg := aoni.GetRequestConfig(req.Context()); cfg != nil && cfg.QueryEncoder != nil {
			encoder = cfg.QueryEncoder
		}

		qValues, err := encoder(query)
		if err != nil {
			aoni.GetOrInitRequestConfig(req).QueryError = err
			return
		}

		if len(qValues) > 0 {
			existing := req.URL.Query()
			maps.Copy(existing, qValues)
			req.URL.RawQuery = existing.Encode()
		}
	}
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

// ResetHeaders returns a modifier that clears all headers from the request.
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
func WithBody(r io.Reader) aoni.RequestModifier {
	return func(req *http.Request) {
		rc, ok := r.(io.ReadCloser)
		if !ok && r != nil {
			rc = io.NopCloser(r)
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
			req.GetBody = func() (io.ReadCloser, error) {
				if seeker, ok := r.(io.Seeker); ok {
					if _, err := seeker.Seek(0, io.SeekStart); err != nil {
						return nil, err
					}

					return io.NopCloser(r), nil
				}

				return nil, errors.New("aoni: body does not support seeking for hedging")
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

		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		req.ContentLength = int64(len(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(bodyBytes)), nil
		}
	}
}

// WithMultipart builds a multipart/form-data body from fields and files.
func WithMultipart(fields map[string]string, files map[string]io.Reader) aoni.RequestModifier {
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

			buf := make([]byte, 32*1024)

			_, err = io.CopyBuffer(part, r, buf)
			if err != nil {
				aoni.GetOrInitRequestConfig(req).BodyError = err
				return
			}
		}

		if err := writer.Close(); err != nil {
			aoni.GetOrInitRequestConfig(req).BodyError = err
			return
		}

		req.Body = io.NopCloser(body)
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

// WithFormValues merges the provided url.Values into the request body as
// application/x-www-form-urlencoded.
func WithFormValues(values url.Values) aoni.RequestModifier {
	return func(req *http.Request) {
		encoded := values.Encode()
		req.Body = io.NopCloser(strings.NewReader(encoded))
		req.ContentLength = int64(len(encoded))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(encoded)), nil
		}
	}
}

// WithFormBody serializes payload as URL-encoded form values, sets the request
// body, and adds a Content-Type: application/x-www-form-urlencoded header.
func WithFormBody(payload any) aoni.RequestModifier {
	return func(req *http.Request) {
		if payload == nil {
			return
		}

		if r, ok := payload.(io.Reader); ok {
			rc, ok := r.(io.ReadCloser)
			if !ok {
				rc = io.NopCloser(r)
			}

			req.Body = rc
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

			return
		}

		encoder := values.StructToValues
		if cfg := aoni.GetRequestConfig(req.Context()); cfg != nil && cfg.QueryEncoder != nil {
			encoder = cfg.QueryEncoder
		}

		values, err := encoder(payload)
		if err != nil {
			aoni.GetOrInitRequestConfig(req).BodyError = err
			return
		}

		encoded := values.Encode()
		req.Body = io.NopCloser(strings.NewReader(encoded))
		req.ContentLength = int64(len(encoded))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(encoded)), nil
		}
	}
}

// WithFallback returns a [RequestModifier] that registers f as the
// fallback for this request. See [FallbackMiddleware].
func WithFallback(f aoni.FallbackFunc) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).Fallback = f
	}
}

// WithDebug tags the request for verbose debug logging.
func WithDebug() aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).Debug = true
	}
}

// WithDecoder is an alias for [codec.WithDecoder].
// It overrides the response Decoder for this request.
var WithDecoder = codec.WithDecoder

// WithErrorModel sets the target struct/map for non-2xx response decoding.
func WithErrorModel(model any) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).ErrorModel = model
	}
}

// WithUploadProgress wraps the request body with a [ProgressReader].
// that calls onProgress during reads. The total parameter is
// Content-Length or -1 when unknown.
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

// WithCaptureResponse stores a pointer reference to capture the raw http.Response.
func WithCaptureResponse(target any) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).Capturer = target
	}
}

// WithOrderedHeaders sets header serialization order for HTTP/1.1.
func WithOrderedHeaders(headers []string) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).OrderedHeaders = headers
	}
}

// WithALPN sets custom ALPN protocols for TLS.
func WithALPN(protos ...string) aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).ALPNOverride = protos
	}
}

// WithStreamingMultipart builds a multipart/form-data body using an
// [io.Pipe] so that file data is streamed rather than buffered in memory.
func WithStreamingMultipart(fields map[string]string, files map[string]io.Reader) aoni.RequestModifier {
	return func(req *http.Request) {
		pr, pw := io.Pipe()

		writer := multipart.NewWriter(pw)
		if cfg := aoni.GetOrInitRequestConfig(req); cfg.MultipartBoundary != "" {
			_ = writer.SetBoundary(cfg.MultipartBoundary)
		}

		go func() {
			defer pw.Close()
			defer writer.Close()

			for k, v := range fields {
				_ = writer.WriteField(k, v)
			}

			for key, r := range files {
				part, _ := writer.CreateFormFile(key, key)
				_, _ = io.Copy(part, r)
			}
		}()

		req.Body = pr
		req.Header.Set("Content-Type", writer.FormDataContentType())
	}
}

// WithMultiReadThreshold sets the multi-read threshold in bytes for a single request.
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
	if min > max {
		min, max = max, min
	}

	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).TCPDelay = aoni.TCPDelayRange{Min: min, Max: max}
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
	if override.MaxAttempts < 1 {
		override.MaxAttempts = 1
	}

	if override.Condition == nil {
		override.Condition = aoni.RetryOnErr()
	}

	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).RetryPolicy = &override
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

// WithCurlDump returns a [RequestModifier] that dumps the equivalent curl command to stderr.
func WithCurlDump() aoni.RequestModifier {
	return func(req *http.Request) {
		var body []byte

		if req.Body != nil && req.Body != http.NoBody {
			var buf bytes.Buffer

			_, _ = io.Copy(&buf, req.Body)
			body = buf.Bytes()
			req.Body = io.NopCloser(bytes.NewReader(body))
		}

		curl := aoni.CurlCommand(req, body)
		fmt.Fprintf(os.Stderr, "%s\n", curl)
	}
}

// WithPadding returns a [RequestModifier] that adds random packet padding
// headers to the request matching the given [PaddingConfig].
// This is a high-level helper to apply individual padding settings per request.
func WithPadding(cfg aoni.PaddingConfig) aoni.RequestModifier {
	return func(req *http.Request) {
		if padding := aoni.GeneratePadding(cfg); len(padding) > 0 {
			headerName := aoni.PaddingHeaderName(cfg)
			req.Header.Set(headerName, hex.EncodeToString(padding))
		}
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

// WithTraceContext returns a [RequestModifier] that attaches a new [TraceInfo]
// to the request context. This allows developers to retrieve network
// timing and JA4/JA4H fingerprints using [ResponseTrace] after the request finishes.
func WithTraceContext() aoni.RequestModifier {
	return func(req *http.Request) {
		info := &aoni.TraceInfo{}
		aoni.GetOrInitRequestConfig(req).TraceInfo = info
		aoni.TraceJA4(info)(req)
	}
}

// WithFragmentation returns a RequestModifier that sets fragmentation configuration on the request context.
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

// AppendHostRewrite returns a RequestModifier that appends new host rewrite rules to the existing
// HostRewriteConfig in the request context, or creates a new one if none are present.
func AppendHostRewrite(rules map[string]string) aoni.RequestModifier {
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

// WithResponseValidator attaches a validation function that is invoked by
// [Client.Request] immediately after a successful HTTP round-trip, before the
// response body is decoded.
func WithResponseValidator(fn func(resp *http.Response) error) aoni.RequestModifier {
	return func(req *http.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)

		existing := cfg.ResponseValidator
		if existing != nil {
			cfg.ResponseValidator = func(resp *http.Response) error {
				err1 := existing(resp)

				err2 := fn(resp)
				if err2 != nil {
					return err2
				}

				return err1
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

// WithForceHTTP1 returns a RequestModifier that advertises only
// http/1.1 in ALPN, preventing the server from upgrading to HTTP/2.
func WithForceHTTP1() aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).ALPNOverride = []string{aoni.AlpnHTTP}
	}
}

// WithForceHTTP2 returns a RequestModifier that advertises only
// h2 in ALPN, forcing the server to use HTTP/2.
func WithForceHTTP2() aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).ALPNOverride = []string{"h2"}
	}
}

// WithForceHTTP3 returns a RequestModifier that advertises only
// h3 in ALPN, forcing the server to use HTTP/3 (QUIC) for this request.
func WithForceHTTP3() aoni.RequestModifier {
	return func(req *http.Request) {
		aoni.GetOrInitRequestConfig(req).ALPNOverride = []string{"h3"}
	}
}

// WithIfNoneMatch sets the If-None-Match request header to the provided ETag value.
func WithIfNoneMatch(etag string) aoni.RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("If-None-Match", etag)
	}
}

// WithIfMatch sets the If-Match request header to the provided ETag value.
func WithIfMatch(etag string) aoni.RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("If-Match", etag)
	}
}

// WithIfModifiedSince sets the If-Modified-Since request header.
func WithIfModifiedSince(t time.Time) aoni.RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("If-Modified-Since", t.UTC().Format(http.TimeFormat))
	}
}

// WithContext returns a RequestModifier that sets the context on the request.
func WithContext(ctx context.Context) aoni.RequestModifier {
	return func(req *http.Request) {
		*req = *req.WithContext(ctx)
	}
}

// WithTimeout sets a timeout for the request by wrapping its context with a deadline.
func WithTimeout(d time.Duration) aoni.RequestModifier {
	return func(req *http.Request) {
		ctx, cancel := context.WithTimeout(req.Context(), d) //nolint:gosec
		*req = *req.WithContext(ctx)
		aoni.GetOrInitRequestConfig(req).RequestTimeoutCancel = cancel
	}
}
