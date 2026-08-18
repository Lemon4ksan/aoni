// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package ws provides WebSocket utilities for the aoni project.
package ws

import (
	"bytes"
	"net/http"
	"strings"

	"github.com/lemon4ksan/foundation/net/hpack"
)

// EncodeConnectHeaders encodes HTTP/2 Extended CONNECT pseudo-headers and request headers into HPACK block bytes.
func EncodeConnectHeaders(
	scheme, path, host string,
	req *http.Request,
	isForbiddenHeader func(string) bool,
) ([]byte, error) {
	var buf bytes.Buffer

	encoder := hpack.NewEncoder(&buf)

	pseudoHeaders := []hpack.HeaderField{
		{Name: ":method", Value: "CONNECT"},
		{Name: ":protocol", Value: "websocket"},
		{Name: ":scheme", Value: scheme},
		{Name: ":path", Value: path},
		{Name: ":authority", Value: host},
	}

	for _, h := range pseudoHeaders {
		if err := encoder.WriteField(h); err != nil {
			return nil, err
		}
	}

	if err := encoder.WriteField(hpack.HeaderField{Name: "sec-websocket-version", Value: "13"}); err != nil {
		return nil, err
	}

	if req != nil {
		for k, vv := range req.Header {
			lowerKey := strings.ToLower(k)
			if isForbiddenHeader != nil && isForbiddenHeader(lowerKey) {
				continue
			}

			for _, v := range vv {
				if err := encoder.WriteField(hpack.HeaderField{Name: lowerKey, Value: v}); err != nil {
					return nil, err
				}
			}
		}
	}

	return buf.Bytes(), nil
}
