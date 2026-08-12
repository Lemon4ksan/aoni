// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"bytes"
	"context"
	stdio "io"
	"net/http"
	"net/url"
	"strings"
)

type stdRequestAdapter struct {
	req *http.Request
}

// NewStdRequestAdapter wraps stdReq into a pipeline.Request contract adapter.
func NewStdRequestAdapter(stdReq *http.Request) Request {
	if stdReq == nil {
		stdReq = &http.Request{Header: make(http.Header)}
	}

	return &stdRequestAdapter{req: stdReq}
}

func (s *stdRequestAdapter) Context() context.Context {
	return s.req.Context()
}

func (s *stdRequestAdapter) SetContext(ctx context.Context) {
	if s.req != nil {
		*s.req = *s.req.WithContext(ctx)
	}
}

func (s *stdRequestAdapter) Method() string {
	return s.req.Method
}

func (s *stdRequestAdapter) SetMethod(method string) {
	s.req.Method = method
}

func (s *stdRequestAdapter) URL() string {
	if s.req.URL == nil {
		return ""
	}

	return s.req.URL.String()
}

func (s *stdRequestAdapter) SetURL(urlStr string) {
	parsed, err := url.Parse(urlStr)
	if err == nil {
		s.req.URL = parsed
		s.req.Host = parsed.Host
	}
}

func (s *stdRequestAdapter) Path() string {
	if s.req.URL == nil {
		return ""
	}

	return s.req.URL.Path
}

func (s *stdRequestAdapter) SetPath(path string) {
	if s.req.URL != nil {
		s.req.URL.Path = path
	}
}

func (s *stdRequestAdapter) RawQuery() string {
	if s.req.URL == nil {
		return ""
	}

	return s.req.URL.RawQuery
}

func (s *stdRequestAdapter) SetRawQuery(query string) {
	if s.req.URL != nil {
		s.req.URL.RawQuery = query
	}
}

func (s *stdRequestAdapter) AddQueryParam(key, value string) {
	if s.req.URL == nil {
		return
	}

	q := s.req.URL.Query()
	q.Add(key, value)
	s.req.URL.RawQuery = q.Encode()
}

func (s *stdRequestAdapter) Header(key string) string {
	if s.req.Header == nil {
		return ""
	}

	return s.req.Header.Get(key)
}

func (s *stdRequestAdapter) SetHeader(key, value string) {
	if s.req.Header == nil {
		s.req.Header = make(http.Header)
	}

	if strings.EqualFold(key, "Host") {
		s.req.Host = value
	}

	s.req.Header.Set(key, value)
}

func (s *stdRequestAdapter) AddHeader(key, value string) {
	if s.req.Header == nil {
		s.req.Header = make(http.Header)
	}

	s.req.Header.Add(key, value)
}

func (s *stdRequestAdapter) DelHeader(key string) {
	if s.req.Header != nil {
		s.req.Header.Del(key)
	}
}

func (s *stdRequestAdapter) ResetHeaders() {
	s.req.Header = make(http.Header)
}

func (s *stdRequestAdapter) SetBodyBytes(body []byte) {
	s.req.Body = stdio.NopCloser(bytes.NewReader(body))
	s.req.ContentLength = int64(len(body))
}

func (s *stdRequestAdapter) BodyBytes() []byte {
	if s.req.Body == nil {
		return nil
	}

	b, _ := stdio.ReadAll(s.req.Body)
	s.req.Body = stdio.NopCloser(bytes.NewReader(b))

	return b
}

func (s *stdRequestAdapter) SetBodyStream(r stdio.Reader, contentLength int64) {
	if rc, ok := r.(stdio.ReadCloser); ok {
		s.req.Body = rc
	} else if r != nil {
		s.req.Body = stdio.NopCloser(r)
	}

	s.req.ContentLength = contentLength
}

func (s *stdRequestAdapter) BodyStream() stdio.Reader {
	return s.req.Body
}

func (s *stdRequestAdapter) HTTPRequest() *http.Request {
	return s.req
}

func (s *stdRequestAdapter) EngineRequest() any {
	return s.req
}
