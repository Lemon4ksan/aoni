// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package core

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/http"

	"github.com/lemon4ksan/foundation/silicon/bytesconv"
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
		req.SetHeader("Authorization", "Bearer "+m.Value)
	case ModBasicAuth:
		encoded := base64.StdEncoding.EncodeToString(bytesconv.S2B(m.Key + ":" + m.Value))
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
			r := NewStdRequest(req)
			m.Fn(r)
			ReleaseStdRequest(r)
		}
	}
}

// ProgressFunc is a callback invoked periodically to monitor stream upload or download progress.
type ProgressFunc func(current, total int64)
