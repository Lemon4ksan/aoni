// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package digest implements RFC 7616 and RFC 2617 Digest Access Authentication for HTTP transactions.
// Core implementation is located in [github.com/lemon4ksan/foundation/net/http/digest].
package digest

import (
	fdigest "github.com/lemon4ksan/foundation/net/http/digest"
)

var (
	ErrDigestBadChallenge    = fdigest.ErrDigestBadChallenge
	ErrDigestInvalidCharset  = fdigest.ErrDigestInvalidCharset
	ErrDigestAlgNotSupported = fdigest.ErrDigestAlgNotSupported
	ErrDigestQopNotSupported = fdigest.ErrDigestQopNotSupported
)

// Transport wraps an [http.RoundTripper] to automatically resolve HTTP 401 Digest Authentication challenges.
type Transport = fdigest.Transport
