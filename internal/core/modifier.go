// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package core

import (
	"bytes"
	"context"
	"io"
	"iter"
	"net/http"
	"net/url"

	fio "github.com/lemon4ksan/foundation/io"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"

	"github.com/lemon4ksan/aoni/netutil"
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
	Stream      io.Reader
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
		req.SetHeader("Authorization", netutil.FormatBearerAuth(m.Value))
	case ModBasicAuth:
		req.SetHeader("Authorization", netutil.FormatBasicAuth(m.Key, m.Value))
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

		req.Header.Set("Authorization", netutil.FormatBearerAuth(m.Value))

	case ModBasicAuth:
		if req.Header == nil {
			req.Header = make(http.Header)
		}

		req.Header.Set("Authorization", netutil.FormatBasicAuth(m.Key, m.Value))

	case ModBodyBytes:
		buf := m.Bytes
		req.Body = io.NopCloser(bytes.NewReader(buf))
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(buf)), nil
		}

		req.ContentLength = int64(len(buf))
		if m.ContentType != "" {
			if req.Header == nil {
				req.Header = make(http.Header)
			}

			req.Header.Set("Content-Type", m.ContentType)
		}

	case ModBodyStream:
		if r, ok := m.Stream.(io.ReadCloser); ok {
			req.Body = r
		} else if m.Stream != nil {
			req.Body = io.NopCloser(m.Stream)
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
			r := &stdReqAdapter{req: req}
			m.Fn(r)
		}
	}
}

type stdReqAdapter struct {
	req *http.Request
}

func (s *stdReqAdapter) Context() context.Context       { return s.req.Context() }
func (s *stdReqAdapter) SetContext(ctx context.Context) { *s.req = *s.req.WithContext(ctx) }
func (s *stdReqAdapter) Method() string                 { return s.req.Method }
func (s *stdReqAdapter) SetMethod(m string)             { s.req.Method = m }
func (s *stdReqAdapter) URL() string {
	if s.req.URL == nil {
		return ""
	}

	return s.req.URL.String()
}

func (s *stdReqAdapter) SetURL(u string) {
	if parsed, err := url.Parse(u); err == nil {
		s.req.URL = parsed
		if parsed.Host != "" {
			s.req.Host = parsed.Host
		}
	}
}

func (s *stdReqAdapter) Path() string {
	if s.req.URL == nil {
		return ""
	}

	return s.req.URL.Path
}

func (s *stdReqAdapter) SetPath(p string) {
	if s.req.URL != nil {
		s.req.URL.Path = p
	}
}

func (s *stdReqAdapter) RawQuery() string {
	if s.req.URL == nil {
		return ""
	}

	return s.req.URL.RawQuery
}

func (s *stdReqAdapter) SetRawQuery(q string) {
	if s.req.URL != nil {
		s.req.URL.RawQuery = q
	}
}

func (s *stdReqAdapter) AddQueryParam(k, v string) {
	if s.req.URL == nil {
		return
	}

	q := s.req.URL.Query()
	q.Add(k, v)
	s.req.URL.RawQuery = q.Encode()
}

func (s *stdReqAdapter) Header(k string) string {
	if s.req.Header == nil {
		return ""
	}

	return s.req.Header.Get(k)
}

func (s *stdReqAdapter) Headers() iter.Seq2[[]byte, []byte] {
	return func(yield func([]byte, []byte) bool) {
		if s.req == nil || s.req.Header == nil {
			return
		}

		for k, vv := range s.req.Header {
			kB := bytesconv.S2B(k)
			for _, v := range vv {
				if !yield(kB, bytesconv.S2B(v)) {
					return
				}
			}
		}
	}
}

func (s *stdReqAdapter) SetHeader(k, v string) {
	if s.req.Header == nil {
		s.req.Header = make(http.Header)
	}

	s.req.Header.Set(k, v)
}

func (s *stdReqAdapter) AddHeader(k, v string) {
	if s.req.Header == nil {
		s.req.Header = make(http.Header)
	}

	s.req.Header.Add(k, v)
}

func (s *stdReqAdapter) DelHeader(k string) {
	if s.req.Header != nil {
		s.req.Header.Del(k)
	}
}

func (s *stdReqAdapter) ResetHeaders() {
	s.req.Header = make(http.Header)
}

func (s *stdReqAdapter) SetBodyBytes(b []byte) {
	s.req.Body = io.NopCloser(bytes.NewReader(b))
	s.req.ContentLength = int64(len(b))
}

func (s *stdReqAdapter) BodyBytes() []byte {
	if s.req.Body == nil {
		return nil
	}

	b, _ := io.ReadAll(s.req.Body)
	s.req.Body = io.NopCloser(bytes.NewReader(b))

	return b
}

func (s *stdReqAdapter) SetBodyStream(r io.Reader, cl int64) {
	if rc, ok := r.(io.ReadCloser); ok {
		s.req.Body = rc
	} else if r != nil {
		s.req.Body = io.NopCloser(r)
	}

	s.req.ContentLength = cl
}
func (s *stdReqAdapter) BodyStream() io.Reader      { return s.req.Body }
func (s *stdReqAdapter) HTTPRequest() *http.Request { return s.req }
func (s *stdReqAdapter) EngineRequest() any         { return s.req }

var _ Request = (*stdReqAdapter)(nil)

// ProgressFunc is a callback invoked periodically to monitor stream upload or download progress.
type ProgressFunc = fio.ProgressFunc
