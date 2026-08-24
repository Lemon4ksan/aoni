// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h1engine

import (
	"github.com/lemon4ksan/foundation/borrow"
)

// BodyScoped borrows the request body without memory allocation.
func (req *Request) BodyScoped(s *borrow.Scope) borrow.Bytes {
	b := req.Body()
	if len(b) == 0 {
		return borrow.Bytes{}
	}
	return borrow.NewBytes(b, nil)
}

// ReadBodyScoped executes fn with the underlying request body buffer borrowed for the duration of the call.
func (req *Request) ReadBodyScoped(fn func([]byte) error) error {
	s := borrow.AcquireScope()
	defer s.Release()

	b := req.Body()
	return fn(b)
}

// BodyScoped borrows the response body without memory allocation.
func (resp *Response) BodyScoped(s *borrow.Scope) borrow.Bytes {
	b := resp.Body()
	if len(b) == 0 {
		return borrow.Bytes{}
	}
	return borrow.NewBytes(b, nil)
}

// ReadBodyScoped executes fn with the underlying response body buffer borrowed for the duration of the call.
func (resp *Response) ReadBodyScoped(fn func([]byte) error) error {
	s := borrow.AcquireScope()
	defer s.Release()

	b := resp.Body()
	return fn(b)
}

// PeekScoped borrows the header value associated with key into the given borrow scope.
func (h *RequestHeader) PeekScoped(s *borrow.Scope, key string) borrow.Bytes {
	b := h.Peek(key)
	if len(b) == 0 {
		return borrow.Bytes{}
	}
	return borrow.NewBytes(b, nil)
}

// PeekScoped borrows the header value associated with key into the given borrow scope.
func (h *ResponseHeader) PeekScoped(s *borrow.Scope, key string) borrow.Bytes {
	b := h.Peek(key)
	if len(b) == 0 {
		return borrow.Bytes{}
	}
	return borrow.NewBytes(b, nil)
}

// PathScoped borrows the URI path into the given borrow scope.
func (u *URI) PathScoped(s *borrow.Scope) borrow.Bytes {
	b := u.Path()
	if len(b) == 0 {
		return borrow.Bytes{}
	}
	return borrow.NewBytes(b, nil)
}

// QueryScoped borrows the raw URI query string into the given borrow scope.
func (u *URI) QueryScoped(s *borrow.Scope) borrow.Bytes {
	b := u.QueryString()
	if len(b) == 0 {
		return borrow.Bytes{}
	}
	return borrow.NewBytes(b, nil)
}

// PeekScoped borrows the query/argument value associated with key into the given borrow scope.
func (a *Args) PeekScoped(s *borrow.Scope, key string) borrow.Bytes {
	b := a.Peek(key)
	if len(b) == 0 {
		return borrow.Bytes{}
	}
	return borrow.NewBytes(b, nil)
}
