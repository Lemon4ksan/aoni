// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package proxy

import (
	"context"
	"errors"
	"net/http"

	"github.com/lemon4ksan/aoni"
)

// RetryCondition returns a [RetryCondition] that retries when
// rotator considers the response or error a proxy fault.
func RetryCondition(rotator *Rotator) aoni.RetryCondition {
	return func(resp aoni.Response, err error) bool {
		var httpResp *http.Response
		if resp != nil {
			httpResp = resp.HTTPResponse() //nolint:bodyclose
			if httpResp == nil {
				if err != nil {
					return !errors.Is(err, context.Canceled)
				}

				sc := resp.StatusCode()

				return sc == http.StatusProxyAuthRequired ||
					sc == http.StatusTooManyRequests ||
					sc == http.StatusBadGateway ||
					sc == http.StatusGatewayTimeout ||
					sc == http.StatusServiceUnavailable
			}
		}

		return rotator.isProxyFault(httpResp, err)
	}
}
