// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"errors"
	"io"
	"net/http"

	"github.com/lemon4ksan/miyako/generic"
)

// RequestModifier represents a function that alters an [http.Request] before execution.
// Concrete modifier implementations are located in the [github.com/lemon4ksan/aoni/mod] package.
type RequestModifier = generic.Option[*http.Request]

func withOrderedHeaders(headers []string) RequestModifier {
	return func(req *http.Request) {
		GetOrInitRequestConfig(req).OrderedHeaders = headers
	}
}

func withContentType(ct string) RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("Content-Type", ct)
	}
}

func withAccept(accept string) RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("Accept", accept)
	}
}

func withBody(r io.Reader) RequestModifier {
	return func(req *http.Request) {
		rc, ok := r.(io.ReadCloser)
		if !ok && r != nil {
			rc = io.NopCloser(r)
		}

		req.Body = rc

		if r != nil {
			if b, ok := r.(interface{ Len() int }); ok {
				req.ContentLength = int64(b.Len())
			} else if s, ok := r.(interface{ Len() int64 }); ok {
				req.ContentLength = s.Len()
			}
		}

		if r != nil {
			req.GetBody = func() (io.ReadCloser, error) {
				if seeker, ok := r.(io.Seeker); ok {
					if _, err := seeker.Seek(0, io.SeekStart); err != nil {
						return nil, err
					}

					return io.NopCloser(r), nil
				}

				return nil, errors.New("aoni: body does not support seeking for hedging")
			}
		}
	}
}

func withCaptureResponse(target any) RequestModifier {
	return func(req *http.Request) {
		GetOrInitRequestConfig(req).Capturer = target
	}
}
