// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"

	"github.com/lemon4ksan/aoni/p0f"
)

// RequestModifier represents a function that alters an [http.Request] before execution.
type RequestModifier func(req *http.Request)

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
			req.URL.RawQuery = qValues.Encode()
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
	}
}

// WithJSONBody serializes the payload as JSON and configures it as the request body.
// It sets the Content-Type header to "application/json".
// Any marshaling errors are written to the request context via bodyErrorCtxKey.
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
	}
}

// WithMultipart writes the provided text fields and files into a multipart/form-data body.
// It automatically computes and sets the Content-Type header with the boundary value.
// Any write errors during formatting cause the modifier to return early with an incomplete body.
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

// Debug returns a modifier that forces verbose logging of request and response details.
func Debug() RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), debugCtxKey{}, true)
		*req = *req.WithContext(ctx)
	}
}

// WithDecoder sets the response body [Decoder] strategy for the active request.
func WithDecoder(d Decoder) RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), decoderCtxKey{}, d)
		*req = *req.WithContext(ctx)
	}
}

// WithErrorModel configures a target structure to parse non-2xx API error responses.
func WithErrorModel(target any) RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), errorModelCtxKey{}, target)
		*req = *req.WithContext(ctx)
	}
}

// WithUploadProgress registers a progress callback for monitoring client uploads.
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

// WithDownloadProgress registers a progress callback for monitoring server downloads.
func WithDownloadProgress(onProgress ProgressFunc) RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), downloadProgressCtxKey{}, onProgress)
		*req = *req.WithContext(ctx)
	}
}

// WithHedging applies a request-specific hedging timeout.
func WithHedging(delay time.Duration) RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), hedgingCtxKey{}, delay)
		*req = *req.WithContext(ctx)
	}
}

// CaptureResponse captures the resulting [http.Response] structure upon request completion.
func CaptureResponse(target **http.Response) RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), capturerCtxKey{}, target)
		*req = *req.WithContext(ctx)
	}
}

// WithStreamingMultipart applies a streaming multipart request body.
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

// WithOrderedHeaders specifies the exact order of headers for HTTP/1.1 requests.
func WithOrderedHeaders(order []string) RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), orderedHeadersCtxKey{}, order)
		*req = *req.WithContext(ctx)
	}
}

// QUICMigrationConfig configures QUIC Connection Migration behavior for HTTP/3.
// Connection Migration allows a QUIC connection to survive network interface changes
// (e.g., Wi-Fi to LTE) by using Connection IDs instead of IP:Port binding.
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

// DefaultQUICMigrationConfig returns sensible defaults for Connection Migration.
func DefaultQUICMigrationConfig() QUICMigrationConfig {
	return QUICMigrationConfig{
		EnableMigration:   true,
		KeepAlivePeriod:   15 * time.Second,
		MaxIdleTimeout:    30 * time.Second,
		InitialPacketSize: 1200,
	}
}

// WithHTTP3 returns a new [Client] that uses HTTP/3 instead of HTTP/1.1.
func (c *Client) WithHTTP3() *Client {
	return c.WithHTTP3Config(nil)
}

// WithHTTP3Config returns a new [Client] that uses HTTP/3 with Connection Migration support.
// If config is nil, DefaultQUICMigrationConfig is used.
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

// WithForceHTTP1 returns a [RequestModifier] that forces ALPN to only http/1.1.
func WithForceHTTP1() RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), alpnOverrideCtxKey{}, []string{"http/1.1"})
		*req = *req.WithContext(ctx)
	}
}

// WithForceHTTP2 returns a [RequestModifier] that forces ALPN to only h2.
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
