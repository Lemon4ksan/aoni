// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

		if err := Validate(query); err != nil {
			ctx := context.WithValue(req.Context(), queryErrorCtxKey{}, err)
			*req = *req.WithContext(ctx)
			return
		}

		qValues, err := StructToValues(query)
		if err != nil {
			ctx := context.WithValue(req.Context(), queryErrorCtxKey{}, err)
			*req = *req.WithContext(ctx)
			return
		}

		if len(qValues) > 0 {
			existing := req.URL.Query()

			for k, vs := range qValues {
				existing[k] = vs
			}

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

		// Set GetBody for hedging support — allows cloning the request body.
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
			ctx := context.WithValue(req.Context(), bodyErrorCtxKey{}, err)
			*req = *req.WithContext(ctx)
			return
		}

		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		req.ContentLength = int64(len(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		// Set GetBody for hedging support — allows cloning the request body.
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(bodyBytes)), nil
		}
	}
}

// WithMultipart builds a multipart/form-data body from fields and
// files, sets Content-Length and Content-Type (with boundary). A
// write error during formatting silently returns with an incomplete
// body.
func WithMultipart(fields map[string]string, files map[string]io.Reader) RequestModifier {
	return func(req *http.Request) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		for k, v := range fields {
			if err := writer.WriteField(k, v); err != nil {
				return
			}
		}

		for key, r := range files {
			part, err := writer.CreateFormFile(key, key)
			if err != nil {
				return
			}

			bufPtr := bytePool.Get().(*[]byte)
			_, err = io.CopyBuffer(part, r, *bufPtr)
			bytePool.Put(bufPtr)

			if err != nil {
				return
			}
		}

		if err := writer.Close(); err != nil {
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

// Debug returns a [RequestModifier] that tags the request for
// verbose logging. The [Client] must have a [Logger] set via
// [Client.WithLogger] for output to appear.
func Debug() RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), debugCtxKey{}, true)
		*req = *req.WithContext(ctx)
	}
}

// WithDecoder overrides the response [Decoder] for this request.
// The client-level decoder set via [Client.WithBaseResponse] is
// ignored when this modifier is present.
func WithDecoder(d Decoder) RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), decoderCtxKey{}, d)
		*req = *req.WithContext(ctx)
	}
}

// WithErrorModel tells [Client.Request] to deserialize non-2xx
// response bodies into target. Inspect the result with
// [errors.As] against [APIError].
func WithErrorModel(target any) RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), errorModelCtxKey{}, target)
		*req = *req.WithContext(ctx)
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
		ctx := context.WithValue(req.Context(), downloadProgressCtxKey{}, onProgress)
		*req = *req.WithContext(ctx)
	}
}

// WithHedging overrides the client-level hedging delay for this
// request. A duration <= 0 disables hedging for the request.
func WithHedging(delay time.Duration) RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), hedgingCtxKey{}, delay)
		*req = *req.WithContext(ctx)
	}
}

// CaptureResponse stores the final [http.Response] pointer in
// target after the request completes. Useful for inspecting
// headers or status codes in middleware hooks.
func CaptureResponse(target **http.Response) RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), capturerCtxKey{}, target)
		*req = *req.WithContext(ctx)
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
// HTTP/1.1 request. For HTTP/2, use [H2FramedTransport] instead.
func WithOrderedHeaders(order []string) RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), orderedHeadersCtxKey{}, order)
		*req = *req.WithContext(ctx)
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

	rt := &http3.Transport{
		TLSClientConfig: &tls.Config{
			NextProtos: []string{"h3"},
		},
		QUICConfig: quicCfg,
	}

	newClient.http = &http.Client{
		Transport: rt,
	}

	return newClient
}

// WithForceHTTP1 returns a [RequestModifier] that advertises only
// http/1.1 in ALPN, preventing the server from upgrading to HTTP/2.
func WithForceHTTP1() RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), alpnOverrideCtxKey{}, []string{"http/1.1"})
		*req = *req.WithContext(ctx)
	}
}

// WithForceHTTP2 returns a [RequestModifier] that advertises only
// h2 in ALPN, forcing the server to use HTTP/2.
func WithForceHTTP2() RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), alpnOverrideCtxKey{}, []string{"h2"})
		*req = *req.WithContext(ctx)
	}
}

// WithALPN returns a [RequestModifier] that sets custom ALPN protocols.
func WithALPN(protocols []string) RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), alpnOverrideCtxKey{}, protocols)
		*req = *req.WithContext(ctx)
	}
}

// WithP0fSignature returns a [RequestModifier] that stores a p0f signature
// in the request context. When used with [Client.WithP0fSignature], the
// TCP/IP fields (TTL, DF, window size) are spoofed to match the specified OS.
func WithP0fSignature(sig *p0f.Signature) RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), p0fSignatureCtxKey{}, sig)
		*req = *req.WithContext(ctx)
	}
}
