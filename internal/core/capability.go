// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package core

import (
	"io"
)

// ContentTyper is implemented by request payloads or models that declare their own MIME Content-Type.
type ContentTyper interface {
	ContentType() string
}

// BodyProvider is implemented by types capable of providing their own payload stream.
type BodyProvider interface {
	Body() (io.Reader, error)
}

// BodyRewinder is implemented by request payloads supporting reproducible body re-reads
// (required for RFC 7231 307/308 redirect replay, speculative hedging, and proxy failovers).
type BodyRewinder interface {
	GetBody() (io.ReadCloser, error)
}

// DirectConsumer is implemented by response targets that consume the raw response
// stream directly without structured JSON/XML/Protobuf decoding or MIME checks.
type DirectConsumer interface {
	ReadFromReader(r io.Reader) error
}
