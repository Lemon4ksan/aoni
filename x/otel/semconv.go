// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package otel

import (
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"github.com/lemon4ksan/aoni"
)

// Standard OpenTelemetry Semantic Convention Attribute Keys (v1.26+ / v1.30+).
const (
	// HTTP Request & Response Attributes
	KeyHTTPRequestMethod      = "http.request.method"
	KeyHTTPResponseStatusCode = "http.response.status_code"
	KeyHTTPRequestBodySize    = "http.request.body.size"
	KeyHTTPResponseBodySize   = "http.response.body.size"
	KeyHTTPRequestResendCount = "http.request.resend_count"

	// Server & Network Attributes
	KeyServerAddress          = "server.address"
	KeyServerPort             = "server.port"
	KeyNetworkProtocolName    = "network.protocol.name"
	KeyNetworkProtocolVersion = "network.protocol.version"
	KeyNetworkTransport       = "network.transport"
	KeyNetworkPeerAddress     = "network.peer.address"
	KeyNetworkPeerPort        = "network.peer.port"

	// URL Attributes
	KeyURLFull   = "url.full"
	KeyURLScheme = "url.scheme"
	KeyURLPath   = "url.path"
	KeyURLQuery  = "url.query"

	// Client & User Agent Attributes
	KeyUserAgentOriginal = "user_agent.original"
	KeyClientAddress     = "client.address"

	// Exception & Error Attributes
	KeyExceptionType       = "exception.type"
	KeyExceptionMessage    = "exception.message"
	KeyExceptionStacktrace = "exception.stacktrace"
	KeyErrorType           = "error.type"

	// RPC Semantic Conventions (gRPC / gRPC-Web)
	KeyRPCSystem         = "rpc.system"
	KeyRPCService        = "rpc.service"
	KeyRPCMethod         = "rpc.method"
	KeyRPCGRPCStatusCode = "rpc.grpc.status_code"

	// Service & Resource Attributes
	KeyServiceName          = "service.name"
	KeyServiceVersion       = "service.version"
	KeyTelemetrySDKName     = "telemetry.sdk.name"
	KeyTelemetrySDKLanguage = "telemetry.sdk.language"
	KeyTelemetrySDKVersion  = "telemetry.sdk.version"
)

// Attribute represents a key-value telemetry metadata pair.
type Attribute struct {
	Key   string
	Value any
}

// StringAttr constructs a string-valued [Attribute].
func StringAttr(key, val string) Attribute {
	return Attribute{Key: key, Value: val}
}

// IntAttr constructs an integer-valued [Attribute].
func IntAttr(key string, val int) Attribute {
	return Attribute{Key: key, Value: val}
}

// Int64Attr constructs an int64-valued [Attribute].
func Int64Attr(key string, val int64) Attribute {
	return Attribute{Key: key, Value: val}
}

// BoolAttr constructs a boolean-valued [Attribute].
func BoolAttr(key string, val bool) Attribute {
	return Attribute{Key: key, Value: val}
}

// Float64Attr constructs a float64-valued [Attribute].
func Float64Attr(key string, val float64) Attribute {
	return Attribute{Key: key, Value: val}
}

// HTTPClientRequestAttributes extracts standard OpenTelemetry client attributes from an [aoni.Request].
func HTTPClientRequestAttributes(req aoni.Request) []Attribute {
	if req == nil {
		return nil
	}

	attrs := make([]Attribute, 0, 8)

	// HTTP Method
	method := req.Method()
	if method == "" {
		method = "GET"
	}
	attrs = append(attrs, StringAttr(KeyHTTPRequestMethod, method))

	// URL Parsing
	rawURL := req.URL()
	if rawURL != "" {
		attrs = append(attrs, StringAttr(KeyURLFull, rawURL))

		if u, err := url.Parse(rawURL); err == nil && u != nil {
			if u.Scheme != "" {
				attrs = append(attrs, StringAttr(KeyURLScheme, u.Scheme))
			}
			if u.Path != "" {
				attrs = append(attrs, StringAttr(KeyURLPath, u.Path))
			}
			if u.RawQuery != "" {
				attrs = append(attrs, StringAttr(KeyURLQuery, u.RawQuery))
			}

			// Hostname and Port
			host := u.Hostname()
			if host != "" {
				attrs = append(attrs, StringAttr(KeyServerAddress, host))
			}

			portStr := u.Port()
			if portStr != "" {
				if port, err := strconv.Atoi(portStr); err == nil {
					attrs = append(attrs, IntAttr(KeyServerPort, port))
				}
			} else {
				if strings.EqualFold(u.Scheme, "https") {
					attrs = append(attrs, IntAttr(KeyServerPort, 443))
				} else if strings.EqualFold(u.Scheme, "http") {
					attrs = append(attrs, IntAttr(KeyServerPort, 80))
				}
			}
		}
	}

	// User-Agent Header
	if ua := req.Header("User-Agent"); ua != "" {
		attrs = append(attrs, StringAttr(KeyUserAgentOriginal, ua))
	}

	return attrs
}

// HTTPClientResponseAttributes extracts standard OpenTelemetry attributes from an [aoni.Response].
func HTTPClientResponseAttributes(resp aoni.Response) []Attribute {
	if resp == nil {
		return nil
	}

	attrs := make([]Attribute, 0, 4)
	statusCode := resp.StatusCode()
	if statusCode > 0 {
		attrs = append(attrs, IntAttr(KeyHTTPResponseStatusCode, statusCode))
	}

	if httpResp := resp.HTTPResponse(); httpResp != nil && httpResp.Proto != "" {
		attrs = append(attrs, StringAttr(KeyNetworkProtocolVersion, httpResp.Proto))
	}

	return attrs
}

// ExceptionAttributes constructs standard OpenTelemetry exception event attributes for an error.
func ExceptionAttributes(err error) []Attribute {
	if err == nil {
		return nil
	}

	return []Attribute{
		StringAttr(KeyExceptionType, reflect.TypeOf(err).String()),
		StringAttr(KeyExceptionMessage, err.Error()),
	}
}

// IsGRPCRequest checks whether the given request is a native gRPC or gRPC-Web invocation.
func IsGRPCRequest(req aoni.Request) bool {
	if req == nil {
		return false
	}
	ct := req.Header("Content-Type")
	if ct == "" {
		ct = req.Header("content-type")
	}
	return strings.HasPrefix(ct, "application/grpc")
}

// GRPCClientRequestAttributes extracts OpenTelemetry RPC semantic conventions from a gRPC [aoni.Request].
func GRPCClientRequestAttributes(req aoni.Request) []Attribute {
	if req == nil {
		return nil
	}

	attrs := make([]Attribute, 0, 4)
	attrs = append(attrs, StringAttr(KeyRPCSystem, "grpc"))

	path := req.Path()
	if path == "" {
		if u, err := url.Parse(req.URL()); err == nil && u != nil {
			path = u.Path
		}
	}

	path = strings.TrimPrefix(path, "/")
	if idx := strings.IndexByte(path, '/'); idx > 0 {
		service := path[:idx]
		method := path[idx+1:]
		attrs = append(attrs,
			StringAttr(KeyRPCService, service),
			StringAttr(KeyRPCMethod, method),
		)
	}

	return attrs
}

// GRPCClientResponseAttributes extracts gRPC status code attributes from [aoni.Response].
func GRPCClientResponseAttributes(resp aoni.Response) []Attribute {
	if resp == nil {
		return nil
	}

	attrs := make([]Attribute, 0, 2)
	statusStr := resp.Header("grpc-status")
	if statusStr == "" {
		if trailers := resp.Trailers(); len(trailers) > 0 {
			if vals, ok := trailers["grpc-status"]; ok && len(vals) > 0 {
				statusStr = vals[0]
			}
		}
	}

	if statusStr != "" {
		if code, err := strconv.Atoi(statusStr); err == nil {
			attrs = append(attrs, IntAttr(KeyRPCGRPCStatusCode, code))
		}
	}

	return attrs
}
