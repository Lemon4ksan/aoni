// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lemon4ksan/miyako/generic"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"

	"github.com/lemon4ksan/aoni/p0f"
)

// RequestModifier represents a function that alters an [http.Request] before execution.
type RequestModifier = generic.Option[*http.Request]

// WithVar replaces a single placeholder (e.g. "{key}") in the path with an escaped value.
func WithVar(key string, value any) RequestModifier {
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
// If the argument list has an odd length, it returns early and performs no replacements.
func WithVars(pairs ...any) RequestModifier {
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
// It safely checks for validation errors using [Validate] and serialization failures.
// Any encountered errors are saved to the request context via queryErrorCtxKey.
// Existing query parameters in the URL are preserved and merged with the new values.
func WithQuery(query any) RequestModifier {
	return func(req *http.Request) {
		if query == nil {
			return
		}

		encoder := StructToValues
		if cfg := GetRequestConfig(req.Context()); cfg != nil && cfg.QueryEncoder != nil {
			encoder = cfg.QueryEncoder
		}

		qValues, err := encoder(query)
		if err != nil {
			getOrInitRequestConfig(req).QueryError = err
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
func WithHeader(key, value string) RequestModifier {
	return func(req *http.Request) {
		req.Header.Set(key, value)
	}
}

// WithHeaders sets new key-value pairs in request headers.
func WithHeaders(headers map[string]string) RequestModifier {
	return func(req *http.Request) {
		for k, v := range headers {
			req.Header.Set(k, v)
		}
	}
}

// ResetHeaders returns a modifier that clears all headers from the request.
func ResetHeaders() RequestModifier {
	return func(req *http.Request) {
		req.Header = make(http.Header)
	}
}

// WithBearer applies a Bearer Token authorization header.
func WithBearer(token string) RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

// WithBasicAuth applies Basic Authorization credentials.
func WithBasicAuth(username, password string) RequestModifier {
	return func(req *http.Request) {
		req.SetBasicAuth(username, password)
	}
}

// WithUserAgent overrides the standard User-Agent header field.
func WithUserAgent(ua string) RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("User-Agent", ua)
	}
}

// WithContentType overrides the standard Content-Type header field.
func WithContentType(ct string) RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("Content-Type", ct)
	}
}

// WithAccept overrides the standard Accept header field.
func WithAccept(accept string) RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("Accept", accept)
	}
}

// WithCookie attaches a single cookie to the request.
func WithCookie(c *http.Cookie) RequestModifier {
	return func(req *http.Request) {
		req.AddCookie(c)
	}
}

// WithCookies attaches multiple cookies from a key-value map.
func WithCookies(kv map[string]string) RequestModifier {
	return func(req *http.Request) {
		for k, v := range kv {
			req.AddCookie(&http.Cookie{Name: k, Value: v}) //nolint:gosec
		}
	}
}

// WithBody replaces the request body stream with the provided reader.
func WithBody(r io.Reader) RequestModifier {
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

		// Set GetBody for hedging support - allows cloning the request body.
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

// WithJSONBody serializes payload as JSON, sets the request body,
// and adds a Content-Type: application/json header. Marshaling
// errors are stored in the request context and retrievable via
// the body error hook.
func WithJSONBody(payload any) RequestModifier {
	return func(req *http.Request) {
		bodyBytes, err := json.Marshal(payload)
		if err != nil {
			getOrInitRequestConfig(req).BodyError = err
			return
		}

		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		req.ContentLength = int64(len(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		// Set GetBody for hedging support - allows cloning the request body.
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(bodyBytes)), nil
		}
	}
}

// WithMultipart builds a multipart/form-data body from fields and
// files, sets Content-Length and Content-Type (with boundary).
// Encoding errors are stored in the request context and retrievable
// via the body error hook (same as [WithJSONBody]).
func WithMultipart(fields map[string]string, files map[string]io.Reader) RequestModifier {
	return func(req *http.Request) {
		body := &bytes.Buffer{}

		writer := multipart.NewWriter(body)
		if cfg := getOrInitRequestConfig(req); cfg.MultipartBoundary != "" {
			_ = writer.SetBoundary(cfg.MultipartBoundary)
		}

		for k, v := range fields {
			if err := writer.WriteField(k, v); err != nil {
				getOrInitRequestConfig(req).BodyError = err

				return
			}
		}

		for key, r := range files {
			part, err := writer.CreateFormFile(key, key)
			if err != nil {
				getOrInitRequestConfig(req).BodyError = err

				return
			}

			bufPtr := bytePool.Get().(*[]byte)
			_, err = io.CopyBuffer(part, r, *bufPtr)
			bytePool.Put(bufPtr)

			if err != nil {
				getOrInitRequestConfig(req).BodyError = err

				return
			}
		}

		if err := writer.Close(); err != nil {
			getOrInitRequestConfig(req).BodyError = err

			return
		}

		req.Body = io.NopCloser(body)
		req.ContentLength = int64(body.Len())
		req.Header.Set("Content-Type", writer.FormDataContentType())
	}
}

// WithOrigin overrides the standard Origin header field.
func WithOrigin(origin string) RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("Origin", origin)
	}
}

// WithDebug returns a [RequestModifier] that tags the request for
// verbose logging. The [Client] must have a [Logger] set via
// [WithClientLogger] for output to appear.
func WithDebug() RequestModifier {
	return func(req *http.Request) {
		getOrInitRequestConfig(req).Debug = true
	}
}

// WithDecoder overrides the response [Decoder] for this request.
// The client-level decoder set via [WithClientBaseResponse] is
// ignored when this modifier is present.
func WithDecoder(d Decoder) RequestModifier {
	return func(req *http.Request) {
		getOrInitRequestConfig(req).Decoder = d
	}
}

// WithErrorModel tells [Client.Request] to deserialize non-2xx
// response bodies into target. Inspect the result with
// [errors.As] against [APIError].
func WithErrorModel(target any) RequestModifier {
	return func(req *http.Request) {
		getOrInitRequestConfig(req).ErrorModel = target
	}
}

// WithUploadProgress wraps the request body with a [progressReader]
// that calls onProgress during reads. The total parameter is
// Content-Length or -1 when unknown.
func WithUploadProgress(onProgress ProgressFunc) RequestModifier {
	return func(req *http.Request) {
		if req.Body != nil && req.Body != http.NoBody {
			req.Body = &progressReader{
				reader:     req.Body,
				total:      req.ContentLength,
				onProgress: onProgress,
			}
		}
	}
}

// WithDownloadProgress registers onProgress to be called during
// response body reads. The callback fires with the bytes-read
// total and the Content-Length value.
func WithDownloadProgress(onProgress ProgressFunc) RequestModifier {
	return func(req *http.Request) {
		getOrInitRequestConfig(req).DownloadProgress = onProgress
	}
}

// WithHedging overrides the client-level hedging delay for this
// request. A duration <= 0 disables hedging for the request.
func WithHedging(delay time.Duration) RequestModifier {
	return func(req *http.Request) {
		getOrInitRequestConfig(req).HedgingDelayOverride = &delay
	}
}

// WithCaptureResponse stores the final [http.Response] pointer in
// target after the request completes. Useful for inspecting
// headers or status codes in middleware hooks.
func WithCaptureResponse(target **http.Response) RequestModifier {
	return func(req *http.Request) {
		getOrInitRequestConfig(req).Capturer = target
	}
}

// WithStreamingMultipart builds a multipart/form-data body using an
// [io.Pipe] so that file data is streamed rather than buffered in
// memory. Content-Length is not set because the total size is
// unknown until writing completes.
func WithStreamingMultipart(fields map[string]string, files map[string]io.Reader) RequestModifier {
	return func(req *http.Request) {
		pr, pw := io.Pipe()

		writer := multipart.NewWriter(pw)
		if cfg := getOrInitRequestConfig(req); cfg.MultipartBoundary != "" {
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

// WithOrderedHeaders sets the header serialization order for this
// HTTP/1.1 request. For HTTP/2, use [FramedTransport] instead.
func WithOrderedHeaders(order []string) RequestModifier {
	return func(req *http.Request) {
		getOrInitRequestConfig(req).OrderedHeaders = order
	}
}

// QUICMigrationConfig controls QUIC Connection Migration for HTTP/3.
// Migration lets a QUIC connection survive network interface changes
// (e.g. Wi-Fi to cellular) by tracking connection IDs instead of
// IP:port tuples. See [DefaultQUICMigrationConfig].
type QUICMigrationConfig struct {
	// EnableMigration enables QUIC Connection Migration (default: true).
	// When enabled, the client can survive IP address changes without renegotiating.
	EnableMigration bool
	// KeepAlivePeriod sends periodic keepalive packets to maintain the connection
	// during network transitions. Set to 0 to disable (default: 15s).
	KeepAlivePeriod time.Duration
	// MaxIdleTimeout is the maximum duration without network activity before
	// the connection is closed. Longer values allow more time for migration
	// but consume resources (default: 30s).
	MaxIdleTimeout time.Duration
	// DisablePathMTUDiscovery disables Path MTU Discovery during migration.
	// Disable if the network path is unreliable (default: false).
	DisablePathMTUDiscovery bool
	// InitialPacketSize sets the initial QUIC packet size (default: 1200).
	// Lower values improve compatibility with restrictive networks.
	InitialPacketSize uint16
}

// DefaultQUICMigrationConfig returns a [QUICMigrationConfig] with
// production-ready defaults.
func DefaultQUICMigrationConfig() QUICMigrationConfig {
	return QUICMigrationConfig{
		EnableMigration:   true,
		KeepAlivePeriod:   15 * time.Second,
		MaxIdleTimeout:    30 * time.Second,
		InitialPacketSize: 1200,
	}
}

// WithHTTP3 returns a clone of c that sends requests over HTTP/3
// (QUIC). Uses [DefaultQUICMigrationConfig] for migration settings.
func (c *Client) WithHTTP3() *Client {
	return c.WithHTTP3Config(nil)
}

// WithHTTP3Config returns a clone of c that sends requests over
// HTTP/3 (QUIC) with migration settings from config. When config
// is nil, [DefaultQUICMigrationConfig] values are used.
func (c *Client) WithHTTP3Config(config *QUICMigrationConfig) *Client {
	newClient := c.Clone()

	if config == nil {
		cfg := DefaultQUICMigrationConfig()
		config = &cfg
	}

	quicCfg := &quic.Config{
		EnableDatagrams:         true,
		DisablePathMTUDiscovery: config.DisablePathMTUDiscovery,
		InitialPacketSize:       config.InitialPacketSize,
	}

	if config.KeepAlivePeriod > 0 {
		quicCfg.KeepAlivePeriod = config.KeepAlivePeriod
	}

	if config.MaxIdleTimeout > 0 {
		quicCfg.MaxIdleTimeout = config.MaxIdleTimeout
	}

	if c.fingerprint.H3Settings != nil {
		quicCfg.InitialStreamReceiveWindow = c.fingerprint.H3Settings.InitialStreamReceiveWindow
		quicCfg.MaxStreamReceiveWindow = c.fingerprint.H3Settings.MaxStreamReceiveWindow
		quicCfg.InitialConnectionReceiveWindow = c.fingerprint.H3Settings.InitialConnectionReceiveWindow
		quicCfg.MaxConnectionReceiveWindow = c.fingerprint.H3Settings.MaxConnectionReceiveWindow
		quicCfg.MaxIncomingStreams = c.fingerprint.H3Settings.MaxIncomingStreams
		quicCfg.MaxIncomingUniStreams = c.fingerprint.H3Settings.MaxIncomingUniStreams
		quicCfg.EnableDatagrams = c.fingerprint.H3Settings.EnableDatagrams
	}

	rt := &http3.Transport{
		TLSClientConfig: &tls.Config{
			NextProtos: []string{"h3"},
		},
		QUICConfig: quicCfg,
	}

	newClient.engine = &http.Client{
		Transport: rt,
	}

	return newClient
}

// WithForceHTTP1 returns a [RequestModifier] that advertises only
// WithForceHTTP1 returns a [RequestModifier] that advertises only
// http/1.1 in ALPN, preventing the server from upgrading to HTTP/2.
func WithForceHTTP1() RequestModifier {
	return func(req *http.Request) {
		getOrInitRequestConfig(req).ALPNOverride = []string{"http/1.1"}
	}
}

// WithForceHTTP2 returns a [RequestModifier] that advertises only
// h2 in ALPN, forcing the server to use HTTP/2.
func WithForceHTTP2() RequestModifier {
	return func(req *http.Request) {
		getOrInitRequestConfig(req).ALPNOverride = []string{"h2"}
	}
}

// WithALPN returns a [RequestModifier] that sets custom ALPN protocols.
func WithALPN(protocols []string) RequestModifier {
	return func(req *http.Request) {
		getOrInitRequestConfig(req).ALPNOverride = protocols
	}
}

// WithP0fSignature returns a [RequestModifier] that stores a p0f signature
// in the request context. When used with [WithClientP0fSignature], the
// TCP/IP fields (TTL, DF, window size) are spoofed to match the specified OS.
func WithP0fSignature(sig *p0f.Signature) RequestModifier {
	return func(req *http.Request) {
		getOrInitRequestConfig(req).P0fSignature = sig
	}
}

// WithTimeout overrides the deadline for this individual request by setting
// a request-specific timeout duration. It does not affect the client-level
// timeout configured via [WithClientTimeout].
func WithTimeout(d time.Duration) RequestModifier {
	return func(req *http.Request) {
		getOrInitRequestConfig(req).TimeoutOverride = d
	}
}

// WithFormValues merges the provided url.Values into the request body as
// application/x-www-form-urlencoded. If the request already has a body, it
// is replaced. Use [WithBody] afterwards if you need to combine form data
// with a custom reader.
func WithFormValues(values url.Values) RequestModifier {
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
//
// If payload implements [io.Reader], it is used directly as the body.
// Otherwise, [Validate] is called first, then [StructToValues] converts the
// struct to url.Values. Validation or serialization errors are stored in the
// request context and returned by [Client.Request].
func WithFormBody(payload any) RequestModifier {
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

		encoder := StructToValues
		if cfg := GetRequestConfig(req.Context()); cfg != nil && cfg.QueryEncoder != nil {
			encoder = cfg.QueryEncoder
		}

		values, err := encoder(payload)
		if err != nil {
			getOrInitRequestConfig(req).BodyError = err
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

// WithIfNoneMatch sets the If-None-Match request header to the provided ETag
// value. The server responds with 304 Not Modified when the resource has not
// changed, allowing the client to use its cached copy.
func WithIfNoneMatch(etag string) RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("If-None-Match", etag)
	}
}

// WithIfMatch sets the If-Match request header to the provided ETag value.
// Typically used with PUT/PATCH/DELETE to ensure the resource has not been
// modified by another client since it was last fetched (optimistic locking).
func WithIfMatch(etag string) RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("If-Match", etag)
	}
}

// WithIfModifiedSince sets the If-Modified-Since request header. The server
// responds with 304 Not Modified when the resource has not changed since t,
// avoiding unnecessary payload transfer.
func WithIfModifiedSince(t time.Time) RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("If-Modified-Since", t.UTC().Format(http.TimeFormat))
	}
}

// WithCertificatePin returns a RequestModifier that pins the certificate of the given domain
// to the specified public key SHA-256 fingerprint hash during the TLS handshake.
//
// The hash can be in base64 or hex format, and optionally prefixed with "sha256/".
// Multiple pins can be added for the same domain (e.g. for key rotation backup).
// Matching supports wildcards like "*.example.com".
func WithCertificatePin(domain, hash string) RequestModifier {
	return func(req *http.Request) {
		cfg := getOrInitRequestConfig(req)
		if cfg.CertificatePins == nil {
			cfg.CertificatePins = make(map[string][]string)
		}

		cfg.CertificatePins[domain] = append(cfg.CertificatePins[domain], hash)
	}
}
