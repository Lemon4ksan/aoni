// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"net/url"
	"strings"

	"github.com/lemon4ksan/foundation/net/http/header"
	"github.com/lemon4ksan/foundation/net/urlkit"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/fast/h1engine"
	"github.com/lemon4ksan/aoni/netutil"
)

// resolveTargetFastURI resolves and sets the request URI using pre-parsed BaseURL bytes with zero allocations (RFC 3986 §3 & §5.2).
func (c *Client) resolveTargetFastURI(fastReq *h1engine.Request, path string) error {
	// Zero-allocation fast path for absolute-path references (RFC 3986 §4.2 & §5.2.3).
	if len(c.prepared.BaseURLHostBytes) > 0 && len(path) > 0 && path[0] == '/' && (len(path) < 2 || path[1] != '/') {
		c.setFastURI(fastReq, path)
		return nil
	}

	return c.resolveTargetURLFastFallback(fastReq, path)
}

func (c *Client) setFastURI(fastReq *h1engine.Request, path string) {
	fastReq.URI().SetSchemeBytes(c.prepared.BaseURLSchemeBytes)
	fastReq.URI().SetHostBytes(c.prepared.BaseURLHostBytes)

	if len(c.prepared.BaseURLCleanPathBytes) == 0 {
		fastReq.URI().SetPathBytes(bytesconv.S2B(path))

		return
	}

	var stackBuf [256]byte

	needed := len(c.prepared.BaseURLCleanPathBytes) + len(path)

	var pathBuf []byte
	if needed <= len(stackBuf) {
		pathBuf = stackBuf[:0]
	} else {
		pathBuf = make([]byte, 0, needed)
	}

	pathBuf = append(pathBuf, c.prepared.BaseURLCleanPathBytes...)
	pathBuf = append(pathBuf, path...)
	fastReq.URI().SetPathBytes(pathBuf)
}

// formatTargetURL computes the target URL string from path and BaseURL (RFC 3986 §5.2 & §5.3).
func (c *Client) formatTargetURL(path string) (string, error) {
	if path == "" && (c.cfg.Defaults.BaseURL == nil || c.cfg.Defaults.BaseURL.Host == "") {
		return "", ErrTargetURLEmpty
	}

	return urlkit.ResolveString(c.cfg.Defaults.BaseURL, path)
}

// resolveTargetURLFastFallback formats target URL when fast-path byte slices cannot be directly applied (RFC 3986 §5.2 & §5.3).
func (c *Client) resolveTargetURLFastFallback(fastReq *h1engine.Request, path string) error {
	targetURL, err := c.formatTargetURL(path)
	if err != nil {
		return err
	}

	fastReq.SetRequestURI(targetURL)

	return nil
}

// resolveTargetURL resolves path against BaseURL and sets it into the generic request adapter (RFC 3986 §5.2).
func (c *Client) resolveTargetURL(req aoni.Request, path string) error {
	if fastReqAdapter, ok := req.(*Request); ok {
		if err := c.resolveTargetFastURI(fastReqAdapter.req, path); err != nil {
			return err
		}

		applyUserinfoAuth(req, bytesconv.B2S(fastReqAdapter.req.URI().FullURI()))

		return nil
	}

	targetURL, err := c.formatTargetURL(path)
	if err != nil {
		return err
	}

	req.SetURL(targetURL)
	applyUserinfoAuth(req, targetURL)

	return nil
}

// applyUserinfoAuth extracts embedded userinfo credentials and sets standard HTTP Basic Auth (RFC 3986 §3.2.1).
func applyUserinfoAuth(req aoni.Request, targetURL string) {
	if !strings.Contains(targetURL, "@") {
		return
	}

	if parsed, err := url.Parse(targetURL); err == nil && parsed.User != nil {
		username := parsed.User.Username()
		password, _ := parsed.User.Password()

		if req.Header(header.Authorization) == "" {
			req.SetHeader(header.Authorization, netutil.FormatBasicAuth(username, password))
		}
	}
}
