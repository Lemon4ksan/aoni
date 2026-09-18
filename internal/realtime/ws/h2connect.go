// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package ws provides WebSocket utilities for the aoni project.
package ws

import (
	"net/http"
	"strings"

	"github.com/lemon4ksan/foundation/net/hpack"
	"github.com/lemon4ksan/foundation/net/http/header"
)

// EncodeConnectHeaders encodes HTTP/2 Extended CONNECT pseudo-headers and request headers into HPACK block bytes.
func EncodeConnectHeaders(
	scheme, path, host string,
	req *http.Request,
	isForbiddenHeader func(string) bool,
) ([]byte, error) {

	hp := hpack.AcquireHPACK()
	defer hpack.ReleaseHPACK(hp)
	hf := hpack.AcquireHeaderField()
	defer hpack.ReleaseHeaderField(hf)

	var out []byte

	pseudoHeaders := [][2]string{
		{header.PseudoMethod, header.MethodConnect},
		{header.PseudoProtocol, header.ValueWebSocket},
		{header.PseudoScheme, scheme},
		{header.PseudoPath, path},
		{header.PseudoAuthority, host},
	}

	for _, h := range pseudoHeaders {
		hf.Set(h[0], h[1])
		out = hp.AppendHeader(out, hf, false)
	}

	hf.Set("sec-websocket-version", "13")
	out = hp.AppendHeader(out, hf, false)

	if req != nil {
		for k, vv := range req.Header {
			lowerKey := strings.ToLower(k)
			if isForbiddenHeader != nil && isForbiddenHeader(lowerKey) {
				continue
			}

			for _, v := range vv {
				hf.Set(lowerKey, v)
				out = hp.AppendHeader(out, hf, false)
			}
		}
	}

	return out, nil
}
