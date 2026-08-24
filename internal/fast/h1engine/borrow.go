// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h1engine

import (
	"io"

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

// ReadStreamScoped reads from the response body stream chunk by chunk, passing each borrowed slice to fn.
func (resp *Response) ReadStreamScoped(s *borrow.Scope, fn func(chunk borrow.Bytes) error) error {
	r := resp.BodyStream()
	if r == nil {
		b := resp.Body()
		if len(b) > 0 {
			return fn(borrow.NewBytes(b, nil))
		}
		return nil
	}

	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if callErr := fn(borrow.NewBytes(buf[:n], nil)); callErr != nil {
				return callErr
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
	}
	return nil
}

// PeekScoped borrows the header value associated with key into the given borrow scope.
func (h *RequestHeader) PeekScoped(s *borrow.Scope, key string) borrow.Bytes {
	b := h.Peek(key)
	if len(b) == 0 {
		return borrow.Bytes{}
	}
	return borrow.NewBytes(b, nil)
}

// CookieScoped borrows the cookie value associated with key into the given borrow scope.
func (h *RequestHeader) CookieScoped(s *borrow.Scope, key string) borrow.Bytes {
	b := h.Cookie(key)
	if len(b) == 0 {
		return borrow.Bytes{}
	}
	return borrow.NewBytes(b, nil)
}

// PeekAllScoped borrows all header values associated with key into a slice of borrowed bytes.
func (h *RequestHeader) PeekAllScoped(s *borrow.Scope, key string) []borrow.Bytes {
	values := h.PeekAll(key)
	if len(values) == 0 {
		return nil
	}
	res := make([]borrow.Bytes, len(values))
	for i, v := range values {
		res[i] = borrow.NewBytes(v, nil)
	}
	return res
}

// TrailerScoped borrows the trailer value associated with key into the given borrow scope.
func (h *RequestHeader) TrailerScoped(s *borrow.Scope, key string) borrow.Bytes {
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

// CookieScoped borrows the cookie value associated with key into the given borrow scope.
func (h *ResponseHeader) CookieScoped(s *borrow.Scope, key string) borrow.Bytes {
	var c Cookie
	c.SetKey(key)
	if !h.Cookie(&c) {
		return borrow.Bytes{}
	}
	b := c.Value()
	if len(b) == 0 {
		return borrow.Bytes{}
	}
	return borrow.NewBytes(b, nil)
}

// PeekAllScoped borrows all header values associated with key into a slice of borrowed bytes.
func (h *ResponseHeader) PeekAllScoped(s *borrow.Scope, key string) []borrow.Bytes {
	values := h.PeekAll(key)
	if len(values) == 0 {
		return nil
	}
	res := make([]borrow.Bytes, len(values))
	for i, v := range values {
		res[i] = borrow.NewBytes(v, nil)
	}
	return res
}

// TrailerScoped borrows the trailer value associated with key into the given borrow scope.
func (h *ResponseHeader) TrailerScoped(s *borrow.Scope, key string) borrow.Bytes {
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

// HostScoped borrows the URI host into the given borrow scope.
func (u *URI) HostScoped(s *borrow.Scope) borrow.Bytes {
	b := u.Host()
	if len(b) == 0 {
		return borrow.Bytes{}
	}
	return borrow.NewBytes(b, nil)
}

// SchemeScoped borrows the URI scheme into the given borrow scope.
func (u *URI) SchemeScoped(s *borrow.Scope) borrow.Bytes {
	b := u.Scheme()
	if len(b) == 0 {
		return borrow.Bytes{}
	}
	return borrow.NewBytes(b, nil)
}

// UsernameScoped borrows the URI username into the given borrow scope.
func (u *URI) UsernameScoped(s *borrow.Scope) borrow.Bytes {
	b := u.Username()
	if len(b) == 0 {
		return borrow.Bytes{}
	}
	return borrow.NewBytes(b, nil)
}

// PasswordScoped borrows the URI password into the given borrow scope.
func (u *URI) PasswordScoped(s *borrow.Scope) borrow.Bytes {
	b := u.Password()
	if len(b) == 0 {
		return borrow.Bytes{}
	}
	return borrow.NewBytes(b, nil)
}

// FullURIScoped borrows the full URI into the given borrow scope.
func (u *URI) FullURIScoped(s *borrow.Scope) borrow.Bytes {
	b := u.FullURI()
	if len(b) == 0 {
		return borrow.Bytes{}
	}
	return borrow.NewBytes(b, nil)
}

// RequestURIScoped borrows the request URI into the given borrow scope.
func (u *URI) RequestURIScoped(s *borrow.Scope) borrow.Bytes {
	b := u.RequestURI()
	if len(b) == 0 {
		return borrow.Bytes{}
	}
	return borrow.NewBytes(b, nil)
}

// HashScoped borrows the URI hash/fragment into the given borrow scope.
func (u *URI) HashScoped(s *borrow.Scope) borrow.Bytes {
	b := u.Hash()
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

// PeekAllScoped borrows all query/argument values associated with key into a slice of borrowed bytes.
func (a *Args) PeekAllScoped(s *borrow.Scope, key string) []borrow.Bytes {
	values := a.PeekMulti(key)
	if len(values) == 0 {
		return nil
	}
	res := make([]borrow.Bytes, len(values))
	for i, v := range values {
		res[i] = borrow.NewBytes(v, nil)
	}
	return res
}

// KeyScoped borrows the cookie key into the given borrow scope.
func (c *Cookie) KeyScoped(s *borrow.Scope) borrow.Bytes {
	b := c.Key()
	if len(b) == 0 {
		return borrow.Bytes{}
	}
	return borrow.NewBytes(b, nil)
}

// ValueScoped borrows the cookie value into the given borrow scope.
func (c *Cookie) ValueScoped(s *borrow.Scope) borrow.Bytes {
	b := c.Value()
	if len(b) == 0 {
		return borrow.Bytes{}
	}
	return borrow.NewBytes(b, nil)
}

// DomainScoped borrows the cookie domain into the given borrow scope.
func (c *Cookie) DomainScoped(s *borrow.Scope) borrow.Bytes {
	b := c.Domain()
	if len(b) == 0 {
		return borrow.Bytes{}
	}
	return borrow.NewBytes(b, nil)
}

// PathScoped borrows the cookie path into the given borrow scope.
func (c *Cookie) PathScoped(s *borrow.Scope) borrow.Bytes {
	b := c.Path()
	if len(b) == 0 {
		return borrow.Bytes{}
	}
	return borrow.NewBytes(b, nil)
}

// CookieScoped borrows the full serialized cookie into the given borrow scope.
func (c *Cookie) CookieScoped(s *borrow.Scope) borrow.Bytes {
	b := c.Cookie()
	if len(b) == 0 {
		return borrow.Bytes{}
	}
	return borrow.NewBytes(b, nil)
}
