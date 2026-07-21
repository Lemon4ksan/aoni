// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package proxy

import (
	"net/http"

	"github.com/lemon4ksan/aoni"
)

// RetryCondition returns a [RetryCondition] that retries when
// rotator considers the response or error a proxy fault.
func RetryCondition(rotator *Rotator) aoni.RetryCondition {
	return func(resp *http.Response, err error) bool {
		return rotator.isProxyFault(resp, err)
	}
}
