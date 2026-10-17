// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package requestutil

import (
	"net/url"
	"path"
	"strings"

	"github.com/lemon4ksan/foundation/net/urlkit"
)

// ResolveURL resolves a relative request path or absolute URL against baseURL.
//
// Unlike RFC 3986 / browser reference resolution, it guarantees that BaseURL paths
// (e.g. "/api/v1" or "/api/v1/") are preserved as base prefixes and not displaced
// when relative paths begin with or lack a leading slash.
//
// Absolute URLs and protocol-relative URLs bypass the base URL.
func ResolveURL(baseURL *url.URL, ref string) (*url.URL, error) {
	if ref == "" {
		if baseURL == nil {
			return &url.URL{}, nil
		}

		return urlkit.CloneURL(baseURL), nil
	}

	if urlkit.IsAbsURL(ref) || baseURL == nil || baseURL.Host == "" {
		return urlkit.Parse(ref)
	}

	if strings.HasPrefix(ref, "//") {
		return urlkit.Parse(baseURL.Scheme + ":" + ref)
	}

	rel, err := url.Parse(ref)
	if err != nil {
		return nil, err
	}

	if rel.IsAbs() {
		return rel, nil
	}

	result := urlkit.CloneURL(baseURL)

	baseP := baseURL.Path
	relP := rel.Path

	switch {
	case relP == "":
		result.Path = baseP
	case baseP == "" || baseP == "/":
		if !strings.HasPrefix(relP, "/") {
			result.Path = "/" + relP
		} else {
			result.Path = relP
		}

	default:
		cleanBase := strings.TrimSuffix(baseP, "/")
		cleanRel := strings.TrimPrefix(relP, "/")

		if cleanRel == "" {
			result.Path = cleanBase + "/"
		} else {
			combined := path.Join(cleanBase, cleanRel)
			if strings.HasSuffix(relP, "/") && !strings.HasSuffix(combined, "/") {
				combined += "/"
			}

			result.Path = combined
		}
	}

	if rel.RawQuery != "" {
		if baseURL.RawQuery != "" {
			result.RawQuery = baseURL.RawQuery + "&" + rel.RawQuery
		} else {
			result.RawQuery = rel.RawQuery
		}
	}

	if rel.Fragment != "" {
		result.Fragment = rel.Fragment
	}

	return result, nil
}

// ResolveURLString computes the resolved target URL string from baseURL and ref.
func ResolveURLString(baseURL *url.URL, ref string) (string, error) {
	if urlkit.IsAbsURL(ref) || baseURL == nil || baseURL.Host == "" {
		return ref, nil
	}

	if ref == "" {
		return baseURL.String(), nil
	}

	u, err := ResolveURL(baseURL, ref)
	if err != nil {
		return "", err
	}

	return u.String(), nil
}
