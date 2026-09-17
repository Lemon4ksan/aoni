// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package requestutil_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testing/assert"
	"github.com/lemon4ksan/foundation/testing/require"

	"github.com/lemon4ksan/aoni/internal/requestutil"
)

func TestRedactHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty_input",
			input:    "",
			expected: "",
		},
		{
			name: "no_sensitive_headers",
			input: "Host: example.com\r\n" +
				"User-Agent: curl/7.68.0\r\n" +
				"Accept: */*",
			expected: "Host: example.com\r\n" +
				"User-Agent: curl/7.68.0\r\n" +
				"Accept: */*",
		},
		{
			name: "sensitive_authorization",
			input: "Host: example.com\r\n" +
				"Authorization: Bearer secret-token-123\r\n" +
				"Accept: */*",
			expected: "Host: example.com\r\n" +
				"authorization: <redacted>\r\n" +
				"Accept: */*",
		},
		{
			name: "sensitive_cookie_and_set_cookie",
			input: "Cookie: session=xyz; id=1\r\n" +
				"Set-Cookie: token=abc; Path=/\r\n" +
				"Proxy-Authorization: Basic dXNlcjpwYXNz",
			expected: "cookie: <redacted>\r\n" +
				"set-cookie: <redacted>\r\n" +
				"proxy-authorization: <redacted>",
		},
		{
			name: "mixed_case_sensitive",
			input: "AUTHORIZATION: Bearer token\r\n" +
				"CoOkIe: sess=123",
			expected: "authorization: <redacted>\r\n" +
				"cookie: <redacted>",
		},
		{
			name: "line_without_colon",
			input: "GET / HTTP/1.1\r\n" +
				"Host: example.com\r\n" +
				"\r\n" +
				"body content",
			expected: "GET / HTTP/1.1\r\n" +
				"Host: example.com\r\n" +
				"\r\n" +
				"body content",
		},
		{
			name: "leading_trailing_spaces_around_key",
			input: "  Authorization  : Bearer token\r\n" +
				"Content-Type: text/plain",
			expected: "authorization: <redacted>\r\n" +
				"Content-Type: text/plain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := requestutil.RedactHeaders([]byte(tt.input))
			assert.Equal(t, tt.expected, string(got))
		})
	}
}

func TestIsSensitiveHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		header   string
		expected bool
	}{
		{name: "auth_lower", header: "authorization", expected: true},
		{name: "auth_title", header: "Authorization", expected: true},
		{name: "auth_upper", header: "AUTHORIZATION", expected: true},
		{name: "cookie_lower", header: "cookie", expected: true},
		{name: "cookie_title", header: "Cookie", expected: true},
		{name: "set_cookie_lower", header: "set-cookie", expected: true},
		{name: "set_cookie_title", header: "Set-Cookie", expected: true},
		{name: "proxy_auth_lower", header: "proxy-authorization", expected: true},
		{name: "proxy_auth_title", header: "Proxy-Authorization", expected: true},
		{name: "non_sensitive_host", header: "Host", expected: false},
		{name: "non_sensitive_content_type", header: "Content-Type", expected: false},
		{name: "substring_auth_extra", header: "Authorization-Extra", expected: false},
		{name: "substring_x_cookie", header: "X-Cookie", expected: false},
		{name: "empty_header", header: "", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := requestutil.IsSensitiveHeader([]byte(tt.header))
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestFindFirstNonWhitespaceByte(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected byte
	}{
		{name: "empty_slice", input: "", expected: 0},
		{name: "only_spaces", input: "    ", expected: 0},
		{name: "tabs_and_newlines", input: "\t\r\n\t  \r", expected: 0},
		{name: "leading_whitespace_char", input: "   \t\n  X", expected: 'X'},
		{name: "first_byte_printable", input: "Hello", expected: 'H'},
		{name: "json_bracket", input: "  \n  { \"key\": 1 }", expected: '{'},
		{name: "xml_angle", input: "\r\n  <root/>", expected: '<'},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := requestutil.FindFirstNonWhitespaceByte([]byte(tt.input))
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestIsCloudflareChallengeBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{name: "empty_input", input: "", expected: false},
		{name: "cf_challenge_keyword", input: "please solve cf-challenge to proceed", expected: true},
		{name: "ray_id_keyword", input: "cloudflare ray id: 7e9f1a2b", expected: true},
		{name: "cloudflare_keyword", input: "<title>Just a moment... cloudflare</title>", expected: true},
		{name: "normal_html", input: "<html><body>Welcome to our API</body></html>", expected: false},
		{name: "random_text", input: "just standard error response without protection", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := requestutil.IsCloudflareChallengeBytes([]byte(tt.input))
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestFormatGRPCTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{name: "negative_duration", duration: -10 * time.Second, expected: "0m"},
		{name: "zero_duration", duration: 0, expected: "0m"},
		{name: "nanoseconds", duration: 500 * time.Nanosecond, expected: "500n"},
		{name: "microseconds", duration: 45 * time.Microsecond, expected: "45u"},
		{name: "milliseconds", duration: 250 * time.Millisecond, expected: "250m"},
		{name: "seconds", duration: 15 * time.Second, expected: "15S"},
		{name: "minutes", duration: 12 * time.Minute, expected: "12M"},
		{name: "hours", duration: 3 * time.Hour, expected: "3H"},
		{name: "large_hours", duration: 48 * time.Hour, expected: "48H"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := requestutil.FormatGRPCTimeout(tt.duration)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestIsIdempotentAndSafeMethod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		method       string
		isIdempotent bool
		isSafe       bool
	}{
		{method: "GET", isIdempotent: true, isSafe: true},
		{method: "HEAD", isIdempotent: true, isSafe: true},
		{method: "OPTIONS", isIdempotent: true, isSafe: true},
		{method: "TRACE", isIdempotent: true, isSafe: true},
		{method: "PUT", isIdempotent: true, isSafe: false},
		{method: "DELETE", isIdempotent: true, isSafe: false},
		{method: "POST", isIdempotent: false, isSafe: false},
		{method: "PATCH", isIdempotent: false, isSafe: false},
		{method: "CONNECT", isIdempotent: false, isSafe: false},
		{method: "PURGE", isIdempotent: false, isSafe: false},
		{method: "unknown", isIdempotent: false, isSafe: false},
	}

	for _, tt := range tests {
		t.Run(strings.ToLower(tt.method), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.isIdempotent, requestutil.IsIdempotentMethod(tt.method))
			assert.Equal(t, tt.isSafe, requestutil.IsSafeMethod(tt.method))
		})
	}
}

func TestTruncateBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		body     string
		maxBytes int
		contains string
	}{
		{
			name:     "body_within_limit",
			body:     "short string",
			maxBytes: 50,
			contains: "short string",
		},
		{
			name:     "body_exceeding_limit",
			body:     "12345678901234567890",
			maxBytes: 10,
			contains: "1234567890... [truncated 10 bytes]",
		},
		{
			name:     "default_limit_fallback",
			body:     "short string with negative limit",
			maxBytes: -5,
			contains: "short string with negative limit",
		},
		{
			name:     "zero_limit_fallback",
			body:     "short string with zero limit",
			maxBytes: 0,
			contains: "short string with zero limit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := requestutil.TruncateBody([]byte(tt.body), tt.maxBytes)
			assert.Equal(t, tt.contains, got)
		})
	}
}

func TestIsStreamingResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		resp        *http.Response
		isStreaming bool
	}{
		{
			name:        "nil_response",
			resp:        nil,
			isStreaming: false,
		},
		{
			name: "sse_text_event_stream",
			resp: &http.Response{
				Header: http.Header{
					"Content-Type": []string{"text/event-stream; charset=utf-8"},
				},
			},
			isStreaming: true,
		},
		{
			name: "ndjson_stream",
			resp: &http.Response{
				Header: http.Header{
					"Content-Type": []string{"application/x-ndjson"},
				},
			},
			isStreaming: true,
		},
		{
			name: "application_stream",
			resp: &http.Response{
				Header: http.Header{
					"Content-Type": []string{"application/stream+json"},
				},
			},
			isStreaming: true,
		},
		{
			name: "chunked_text_plain_without_content_length",
			resp: &http.Response{
				Header: http.Header{
					"Content-Type":      []string{"text/plain"},
					"Transfer-Encoding": []string{"chunked"},
				},
				ContentLength: -1,
			},
			isStreaming: true,
		},
		{
			name: "chunked_text_plain_with_known_length",
			resp: &http.Response{
				Header: http.Header{
					"Content-Type":      []string{"text/plain"},
					"Transfer-Encoding": []string{"chunked"},
				},
				ContentLength: 1024,
			},
			isStreaming: false,
		},
		{
			name: "standard_json_response",
			resp: &http.Response{
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				ContentLength: 256,
			},
			isStreaming: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.isStreaming, requestutil.IsStreamingResponse(tt.resp))
		})
	}
}

func TestSummarizeMultipartBody(t *testing.T) {
	t.Parallel()

	t.Run("empty_body_or_type", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "", requestutil.SummarizeMultipartBody(nil, "multipart/form-data"))
		assert.Equal(t, "", requestutil.SummarizeMultipartBody([]byte("data"), ""))
	})

	t.Run("invalid_content_type", func(t *testing.T) {
		t.Parallel()

		got := requestutil.SummarizeMultipartBody([]byte("data"), "not-a-multipart")
		assert.Equal(t, "(multipart/form-data payload)", got)
	})

	t.Run("valid_multipart_payload", func(t *testing.T) {
		t.Parallel()

		var body bytes.Buffer

		writer := multipart.NewWriter(&body)

		err := writer.WriteField("username", "alice")
		require.NoError(t, err)

		part, err := writer.CreateFormFile("avatar", "profile.png")
		require.NoError(t, err)
		_, err = part.Write([]byte("image_data"))
		require.NoError(t, err)

		err = writer.Close()
		require.NoError(t, err)

		summary := requestutil.SummarizeMultipartBody(body.Bytes(), writer.FormDataContentType())
		assert.Contains(t, summary, "username=alice")
		assert.Contains(t, summary, "avatar=@profile.png")
	})

	t.Run("malformed_multipart_body", func(t *testing.T) {
		t.Parallel()
		// Contains boundary header but corrupted body bytes
		contentType := "multipart/form-data; boundary=myboundary"
		summary := requestutil.SummarizeMultipartBody([]byte("broken multipart content"), contentType)
		assert.Equal(t, "(multipart/form-data payload)", summary)
	})
}

func TestHeaderContainsToken(t *testing.T) {
	t.Parallel()

	header := http.Header{
		"Connection": []string{"keep-alive", "Upgrade, close"},
		"Accept":     []string{"application/json, text/plain;q=0.9"},
	}

	tests := []struct {
		name     string
		key      string
		target   string
		expected bool
	}{
		{name: "connection_upgrade", key: "Connection", target: "upgrade", expected: true},
		{name: "connection_close", key: "Connection", target: "CLOSE", expected: true},
		{name: "connection_keep_alive", key: "Connection", target: "keep-alive", expected: true},
		{name: "connection_not_present", key: "Connection", target: "trailer", expected: false},
		{name: "header_not_present", key: "X-Missing", target: "any", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := requestutil.HeaderContainsToken(header, tt.key, tt.target)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestCanonicalHeaderKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		input        string
		expectedStr  string
		expectedByte string
	}{
		{name: "empty_string", input: "", expectedStr: "", expectedByte: ""},
		{name: "content_type_lower", input: "content-type", expectedStr: "Content-Type", expectedByte: "Content-Type"},
		{name: "content_type_upper", input: "CONTENT-TYPE", expectedStr: "Content-Type", expectedByte: "Content-Type"},
		{name: "authorization", input: "authorization", expectedStr: "Authorization", expectedByte: "Authorization"},
		{name: "etag_lower", input: "etag", expectedStr: "ETag", expectedByte: "Etag"},
		{name: "user_agent_lower", input: "user-agent", expectedStr: "User-Agent", expectedByte: "User-Agent"},
		{name: "keep_alive_lower", input: "keep-alive", expectedStr: "Keep-Alive", expectedByte: "Keep-Alive"},
		{
			name:         "custom_header_lower",
			input:        "x-custom-request-id",
			expectedStr:  "X-Custom-Request-Id",
			expectedByte: "X-Custom-Request-Id",
		},
		{
			name:         "custom_header_mixed",
			input:        "x-FORWARDED-for",
			expectedStr:  "X-Forwarded-For",
			expectedByte: "X-Forwarded-For",
		},
		{
			name:         "long_header_over_64_bytes",
			input:        "x-custom-very-long-header-name-exceeding-sixty-four-bytes-limit-for-stack-allocation",
			expectedStr:  "X-Custom-Very-Long-Header-Name-Exceeding-Sixty-Four-Bytes-Limit-For-Stack-Allocation",
			expectedByte: "X-Custom-Very-Long-Header-Name-Exceeding-Sixty-Four-Bytes-Limit-For-Stack-Allocation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := requestutil.CanonicalHeaderKey(tt.input)
			assert.Equal(t, tt.expectedStr, got)

			b := requestutil.CanonicalHeaderKeyBytes([]byte(tt.input))
			assert.Equal(t, tt.expectedByte, string(b))
		})
	}

	t.Run("canonical_header_key_bytes_nil", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, requestutil.CanonicalHeaderKeyBytes(nil))
	})
}
