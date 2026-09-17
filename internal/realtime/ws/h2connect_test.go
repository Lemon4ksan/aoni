// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ws_test

import (
	"net/http"
	"testing"

	"github.com/lemon4ksan/foundation/net/hpack"
	"github.com/lemon4ksan/foundation/testing/assert"
	"github.com/lemon4ksan/foundation/testing/require"

	"github.com/lemon4ksan/aoni/internal/realtime/ws"
)

func decodeHPACK(t *testing.T, payload []byte) map[string][]string {
	t.Helper()

	headers := make(map[string][]string)

	decoder := hpack.NewDecoder(4096, func(f hpack.HeaderField) {
		headers[f.Name] = append(headers[f.Name], f.Value)
	})

	_, err := decoder.Write(payload)
	require.NoError(t, err)

	return headers
}

func TestEncodeConnectHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		scheme             string
		path               string
		host               string
		reqHeaders         map[string][]string
		isForbidden        func(string) bool
		wantPseudoMethod   string
		wantPseudoProtocol string
		wantSecVersion     string
		checkForbidden     string
		wantCustom         string
	}{
		{
			name:               "basic_connect_without_request",
			scheme:             "https",
			path:               "/chat/ws",
			host:               "stream.example.com",
			reqHeaders:         nil,
			isForbidden:        nil,
			wantPseudoMethod:   "CONNECT",
			wantPseudoProtocol: "websocket",
			wantSecVersion:     "13",
		},
		{
			name:   "forbidden_header_filtering",
			scheme: "wss",
			path:   "/events",
			host:   "events.org",
			reqHeaders: map[string][]string{
				"Connection":        {"Upgrade"},
				"Upgrade":           {"websocket"},
				"Sec-WebSocket-Key": {"dGhlIHNhbXBsZSBub25jZQ=="},
				"X-Client-Version":  {"1.5.0"},
			},
			isForbidden: func(h string) bool {
				return h == "connection" || h == "upgrade"
			},
			wantPseudoMethod:   "CONNECT",
			wantPseudoProtocol: "websocket",
			wantSecVersion:     "13",
			checkForbidden:     "connection",
			wantCustom:         "1.5.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var req *http.Request
			if tt.reqHeaders != nil {
				req = &http.Request{Header: make(http.Header)}
				for k, vv := range tt.reqHeaders {
					for _, v := range vv {
						req.Header.Add(k, v)
					}
				}
			}

			encoded, err := ws.EncodeConnectHeaders(tt.scheme, tt.path, tt.host, req, tt.isForbidden)
			require.NoError(t, err)
			require.NotEmpty(t, encoded)

			decoded := decodeHPACK(t, encoded)

			assert.Equal(t, []string{tt.wantPseudoMethod}, decoded[":method"])
			assert.Equal(t, []string{tt.wantPseudoProtocol}, decoded[":protocol"])
			assert.Equal(t, []string{tt.scheme}, decoded[":scheme"])
			assert.Equal(t, []string{tt.path}, decoded[":path"])
			assert.Equal(t, []string{tt.host}, decoded[":authority"])
			assert.Equal(t, []string{tt.wantSecVersion}, decoded["sec-websocket-version"])

			if tt.checkForbidden != "" {
				_, exists := decoded[tt.checkForbidden]
				assert.False(t, exists)
			}

			if tt.wantCustom != "" {
				assert.Equal(t, []string{tt.wantCustom}, decoded["x-client-version"])
			}
		})
	}

	t.Run("multi_value_headers", func(t *testing.T) {
		t.Parallel()

		req := &http.Request{
			Header: http.Header{
				"X-Tag": []string{"tag1", "tag2"},
			},
		}

		encoded, err := ws.EncodeConnectHeaders("https", "/ws", "localhost", req, nil)
		require.NoError(t, err)

		decoded := decodeHPACK(t, encoded)
		assert.Equal(t, []string{"tag1", "tag2"}, decoded["x-tag"])
	})

	t.Run("case_insensitive_header_keys", func(t *testing.T) {
		t.Parallel()

		req := &http.Request{
			Header: http.Header{
				"AUTHORIZATION": []string{"Bearer 123"},
			},
		}

		encoded, err := ws.EncodeConnectHeaders("https", "/ws", "localhost", req, nil)
		require.NoError(t, err)

		decoded := decodeHPACK(t, encoded)
		assert.Equal(t, []string{"Bearer 123"}, decoded["authorization"])
	})
}
