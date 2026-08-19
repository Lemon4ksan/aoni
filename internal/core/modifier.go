// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package core

import (
	"bytes"
	"context"
	"encoding/base64"
	stdio "io"
	"net/http"
	"net/url"
	"strings"
	"unsafe"
)

// ModifierType specifies the discrete operation type of a [RequestModifier] value.
type ModifierType uint8

const (
	ModNone ModifierType = iota
	ModHeader
	ModHeaderAdd
	ModQuery
	ModQueryAdd
	ModBearer
	ModBasicAuth
	ModBodyBytes
	ModBodyStream
	ModCustom
)

// RequestModifier represents a zero-allocation value-based modification payload.
type RequestModifier struct {
	Kind        ModifierType
	Key         string
	Value       string
	ContentType string
	Bytes       []byte
	Stream      stdio.Reader
	Fn          func(Request)
}

// IsZero reports whether the modifier is an uninitialized zero-value.
func (m RequestModifier) IsZero() bool {
	return m.Kind == ModNone && m.Key == "" && m.Value == "" && m.Bytes == nil && m.Stream == nil && m.Fn == nil
}

// Apply mutates an outgoing [Request] contract.
func (m RequestModifier) Apply(req Request) {
	switch m.Kind {
	case ModHeader:
		req.SetHeader(m.Key, m.Value)
	case ModHeaderAdd:
		req.AddHeader(m.Key, m.Value)
	case ModQuery, ModQueryAdd:
		req.AddQueryParam(m.Key, m.Value)
	case ModBearer:
		req.SetHeader("Authorization", "Bearer "+m.Value)
	case ModBasicAuth:
		encoded := base64.StdEncoding.EncodeToString(s2b(m.Key + ":" + m.Value))
		req.SetHeader("Authorization", "Basic "+encoded)
	case ModBodyBytes:
		req.SetBodyBytes(m.Bytes)

		if m.ContentType != "" {
			req.SetHeader("Content-Type", m.ContentType)
		}

	case ModBodyStream:
		var lenVal int64 = -1
		if b, ok := m.Stream.(interface{ Len() int }); ok {
			lenVal = int64(b.Len())
		} else if s, ok := m.Stream.(interface{ Len() int64 }); ok {
			lenVal = s.Len()
		}

		req.SetBodyStream(m.Stream, lenVal)

		if m.ContentType != "" {
			req.SetHeader("Content-Type", m.ContentType)
		}

	case ModCustom:
		if m.Fn != nil {
			m.Fn(req)
		}
	}
}

// ApplyStd applies modifications directly to standard *http.Request without wrapper allocations.
func (m RequestModifier) ApplyStd(req *http.Request) {
	switch m.Kind {
	case ModHeader:
		if req.Header == nil {
			req.Header = make(http.Header)
		}

		req.Header.Set(m.Key, m.Value)

	case ModHeaderAdd:
		if req.Header == nil {
			req.Header = make(http.Header)
		}

		req.Header.Add(m.Key, m.Value)

	case ModQuery:
		if req.URL != nil {
			q := req.URL.Query()
			q.Set(m.Key, m.Value)
			req.URL.RawQuery = q.Encode()
		}

	case ModQueryAdd:
		if req.URL != nil {
			q := req.URL.Query()
			q.Add(m.Key, m.Value)
			req.URL.RawQuery = q.Encode()
		}

	case ModBearer:
		if req.Header == nil {
			req.Header = make(http.Header)
		}

		req.Header.Set("Authorization", "Bearer "+m.Value)

	case ModBasicAuth:
		req.SetBasicAuth(m.Key, m.Value)
	case ModBodyBytes:
		buf := m.Bytes
		req.Body = stdio.NopCloser(bytes.NewReader(buf))
		req.GetBody = func() (stdio.ReadCloser, error) {
			return stdio.NopCloser(bytes.NewReader(buf)), nil
		}

		req.ContentLength = int64(len(buf))
		if m.ContentType != "" {
			if req.Header == nil {
				req.Header = make(http.Header)
			}

			req.Header.Set("Content-Type", m.ContentType)
		}

	case ModBodyStream:
		if r, ok := m.Stream.(stdio.ReadCloser); ok {
			req.Body = r
		} else if m.Stream != nil {
			req.Body = stdio.NopCloser(m.Stream)
		}

		req.ContentLength = -1
		if b, ok := m.Stream.(interface{ Len() int }); ok {
			req.ContentLength = int64(b.Len())
		} else if s, ok := m.Stream.(interface{ Len() int64 }); ok {
			req.ContentLength = s.Len()
		}

		if m.ContentType != "" {
			if req.Header == nil {
				req.Header = make(http.Header)
			}

			req.Header.Set("Content-Type", m.ContentType)
		}

	case ModCustom:
		if m.Fn != nil {
			m.Fn(&stdReqWrapper{req: req})
		}
	}
}

type stdReqWrapper struct {
	req *http.Request
}

func (s *stdReqWrapper) Context() context.Context {
	return s.req.Context()
}

func (s *stdReqWrapper) SetContext(ctx context.Context) {
	if s.req != nil {
		*s.req = *s.req.WithContext(ctx)
	}
}

func (s *stdReqWrapper) Method() string {
	return s.req.Method
}

func (s *stdReqWrapper) SetMethod(method string) {
	s.req.Method = method
}

func (s *stdReqWrapper) URL() string {
	if s.req.URL == nil {
		return ""
	}

	return s.req.URL.String()
}

func (s *stdReqWrapper) SetURL(urlStr string) {
	parsed, err := url.Parse(urlStr)
	if err == nil {
		s.req.URL = parsed
		s.req.Host = parsed.Host
	}
}

func (s *stdReqWrapper) Path() string {
	if s.req.URL == nil {
		return ""
	}

	return s.req.URL.Path
}

func (s *stdReqWrapper) SetPath(path string) {
	if s.req.URL != nil {
		s.req.URL.Path = path
		s.req.URL.RawPath = ""
	}
}

func (s *stdReqWrapper) RawQuery() string {
	if s.req.URL == nil {
		return ""
	}

	return s.req.URL.RawQuery
}

func (s *stdReqWrapper) SetRawQuery(query string) {
	if s.req.URL != nil {
		s.req.URL.RawQuery = query
	}
}

func (s *stdReqWrapper) AddQueryParam(key, value string) {
	if s.req.URL == nil {
		return
	}

	q := s.req.URL.Query()
	q.Add(key, value)
	s.req.URL.RawQuery = q.Encode()
}

func (s *stdReqWrapper) Header(key string) string {
	if s.req.Header == nil {
		return ""
	}

	return s.req.Header.Get(key)
}

func (s *stdReqWrapper) SetHeader(key, value string) {
	if s.req.Header == nil {
		s.req.Header = make(http.Header)
	}

	if strings.EqualFold(key, "Host") {
		s.req.Host = value
	}

	s.req.Header.Set(key, value)
}

func (s *stdReqWrapper) AddHeader(key, value string) {
	if s.req.Header == nil {
		s.req.Header = make(http.Header)
	}

	s.req.Header.Add(key, value)
}

func (s *stdReqWrapper) DelHeader(key string) {
	if s.req.Header != nil {
		s.req.Header.Del(key)
	}
}

func (s *stdReqWrapper) ResetHeaders() {
	s.req.Header = make(http.Header)
}

func (s *stdReqWrapper) SetBodyBytes(body []byte) {
	s.req.Body = stdio.NopCloser(bytes.NewReader(body))
	s.req.ContentLength = int64(len(body))
}

func (s *stdReqWrapper) BodyBytes() []byte {
	if s.req.Body == nil {
		return nil
	}

	b, _ := stdio.ReadAll(s.req.Body)
	s.req.Body = stdio.NopCloser(bytes.NewReader(b))

	return b
}

func (s *stdReqWrapper) SetBodyStream(r stdio.Reader, contentLength int64) {
	if rc, ok := r.(stdio.ReadCloser); ok {
		s.req.Body = rc
	} else if r != nil {
		s.req.Body = stdio.NopCloser(r)
	}

	s.req.ContentLength = contentLength
}

func (s *stdReqWrapper) BodyStream() stdio.Reader {
	return s.req.Body
}

func (s *stdReqWrapper) HTTPRequest() *http.Request {
	return s.req
}

func (s *stdReqWrapper) EngineRequest() any {
	return s.req
}

var _ Request = (*stdReqWrapper)(nil)

// ProgressFunc is a callback invoked periodically to monitor stream upload or download progress.
type ProgressFunc func(current, total int64)

func s2b(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}
