// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h1engine_test

import (
	"bufio"
	"bytes"
	"testing"

	"github.com/lemon4ksan/aoni/internal/fast/h1engine"
)

func FuzzH1Request(f *testing.F) {
	f.Add([]byte("GET /index.html HTTP/1.1\r\nHost: example.com\r\nUser-Agent: aoni\r\n\r\n"))
	f.Add([]byte("POST /api/v1 HTTP/1.1\r\nHost: example.com\r\nContent-Length: 5\r\n\r\nhello"))
	f.Add([]byte("POST /chunked HTTP/1.1\r\nHost: example.com\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nhello\r\n0\r\n\r\n"))
	f.Add([]byte(""))
	f.Add([]byte("\r\n\r\n\r\n"))
	f.Add([]byte("INVALID REQUEST LINE WITH NO SPACES\r\n\r\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64*1024 {
			return
		}

		var req h1engine.Request
		br := bufio.NewReader(bytes.NewReader(data))
		if err := req.Read(br); err == nil {
			_ = req.Header.Method()
			_ = req.Header.RequestURI()
			_ = req.Host()
			_ = req.Body()
			_ = req.URI().String()
		}
	})
}

func FuzzH1Response(f *testing.F) {
	f.Add([]byte("HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: 12\r\n\r\nhello world!"))
	f.Add([]byte("HTTP/1.1 100 Continue\r\n\r\nHTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n"))
	f.Add([]byte("HTTP/1.1 304 Not Modified\r\nETag: \"123\"\r\n\r\n"))
	f.Add([]byte("HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n4\r\nWiki\r\n5\r\npedia\r\n0\r\n\r\n"))
	f.Add([]byte(""))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64*1024 {
			return
		}

		var resp h1engine.Response
		br := bufio.NewReader(bytes.NewReader(data))
		if err := resp.ReadLimitBody(br, 64*1024); err == nil {
			_ = resp.StatusCode()
			_ = resp.Header.ContentType()
			_ = resp.Body()
		}
	})
}

func FuzzH1URI(f *testing.F) {
	f.Add([]byte("https://user:pass@example.com:8443/path/to/resource?query=1&b=2#section"))
	f.Add([]byte("http://[::1]:8080/"))
	f.Add([]byte("/relative/path?arg=val"))
	f.Add([]byte(""))
	f.Add([]byte("https://invalid host name:port/path"))

	f.Fuzz(func(t *testing.T, raw []byte) {
		var u h1engine.URI
		u.Parse(nil, raw)
		_ = u.Scheme()
		_ = u.Host()
		_ = u.Path()
		_ = u.QueryString()
		_ = u.String()
	})
}
