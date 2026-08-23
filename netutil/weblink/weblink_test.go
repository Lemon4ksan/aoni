// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package weblink_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/netutil/weblink"
)

func TestWebLink_RFC8288_Section3_5_Examples(t *testing.T) {
	t.Parallel()

	// Example 1: Previous chapter with title (RFC 8288 §3.5)
	const ex1 = `<http://example.com/TheBook/chapter2>; rel="previous"; title="previous chapter"`

	links1, err1 := weblink.Parse(ex1)
	require.NoError(t, err1)
	require.Len(t, links1, 1)
	assert.Equal(t, "http://example.com/TheBook/chapter2", links1[0].Target)
	assert.Equal(t, "previous", links1[0].Rel)
	assert.True(t, links1[0].HasRel(weblink.RelPrevious))
	assert.True(t, links1[0].HasRel("previous"))
	assert.Equal(t, "previous chapter", links1[0].Title)

	// Example 2: Extension relation type URI (RFC 8288 §3.5)
	const ex2 = `</>; rel="http://example.net/foo"`

	links2, err2 := weblink.Parse(ex2)
	require.NoError(t, err2)
	require.Len(t, links2, 1)
	assert.Equal(t, "/", links2[0].Target)
	assert.Equal(t, "http://example.net/foo", links2[0].Rel)
	assert.True(t, links2[0].HasRel("http://example.net/foo"))

	// Example 3: Anchor fragment override (RFC 8288 §3.5)
	const ex3 = `</terms>; rel="copyright"; anchor="#foo"`

	links3, err3 := weblink.Parse(ex3)
	require.NoError(t, err3)
	require.Len(t, links3, 1)
	assert.Equal(t, "/terms", links3[0].Target)
	assert.Equal(t, "copyright", links3[0].Rel)
	assert.Equal(t, "#foo", links3[0].Anchor)

	// Example 4: RFC 8187 title* encoding with German characters (RFC 8288 §3.5)
	const ex4 = `</TheBook/chapter2>; rel="previous"; title*=UTF-8'de'letztes%20Kapitel, </TheBook/chapter4>; rel="next"; title*=UTF-8'de'n%c3%a4chstes%20Kapitel`

	links4, err4 := weblink.Parse(ex4)
	require.NoError(t, err4)
	require.Len(t, links4, 2)
	assert.Equal(t, "/TheBook/chapter2", links4[0].Target)
	assert.Equal(t, "letztes Kapitel", links4[0].Title)
	assert.Equal(t, "de", links4[0].TitleLang)
	assert.Equal(t, "/TheBook/chapter4", links4[1].Target)
	assert.Equal(t, "nächstes Kapitel", links4[1].Title)
	assert.Equal(t, "de", links4[1].TitleLang)

	// Example 5: Multiple relation types in single link (RFC 8288 §3.5)
	const ex5 = `<http://example.org/>; rel="start http://example.net/relation/other"`

	links5, err5 := weblink.Parse(ex5)
	require.NoError(t, err5)
	require.Len(t, links5, 1)
	assert.True(t, links5[0].HasRel("start"))
	assert.True(t, links5[0].HasRel("http://example.net/relation/other"))
	assert.False(t, links5[0].HasRel("next"))

	// Example 6: Comma-separated links equivalence (RFC 8288 §3.5)
	const ex6 = `<https://example.org/>; rel="start", <https://example.org/index>; rel="index"`

	links6, err6 := weblink.Parse(ex6)
	require.NoError(t, err6)
	require.Len(t, links6, 2)
	assert.Equal(t, "https://example.org/", links6[0].Target)
	assert.True(t, links6[0].HasRel("start"))
	assert.Equal(t, "https://example.org/index", links6[1].Target)
	assert.True(t, links6[1].HasRel("index"))
}

func TestWebLink_Pagination_Helpers(t *testing.T) {
	t.Parallel()

	// Typical REST API Pagination (GitHub / RFC 8288)
	const paginationHeader = `<https://api.github.com/user/repos?page=3&per_page=100>; rel="next", ` +
		`<https://api.github.com/user/repos?page=1&per_page=100>; rel="prev", ` +
		`<https://api.github.com/user/repos?page=1&per_page=100>; rel="first", ` +
		`<https://api.github.com/user/repos?page=50&per_page=100>; rel="last"`

	group, err := weblink.Parse(paginationHeader)
	require.NoError(t, err)
	require.Len(t, group, 4)

	next, okNext := group.Next()
	assert.True(t, okNext)
	assert.Equal(t, "https://api.github.com/user/repos?page=3&per_page=100", next)

	prev, okPrev := group.Prev()
	assert.True(t, okPrev)
	assert.Equal(t, "https://api.github.com/user/repos?page=1&per_page=100", prev)

	first, okFirst := group.First()
	assert.True(t, okFirst)
	assert.Equal(t, "https://api.github.com/user/repos?page=1&per_page=100", first)

	last, okLast := group.Last()
	assert.True(t, okLast)
	assert.Equal(t, "https://api.github.com/user/repos?page=50&per_page=100", last)
}

func TestWebLink_ParseHeader(t *testing.T) {
	t.Parallel()

	h := http.Header{}
	h.Add(weblink.Header, `<https://example.com/style.css>; rel="stylesheet"; type="text/css"; media="screen"`)
	h.Add(weblink.Header, `<https://example.com/logo.png>; rel="preload"; as="image"`)

	group, err := weblink.ParseHeader(h)
	require.NoError(t, err)
	require.Len(t, group, 2)

	assert.Equal(t, "https://example.com/style.css", group[0].Target)
	assert.True(t, group[0].HasRel(weblink.RelStylesheet))
	assert.Equal(t, "text/css", group[0].Type)
	assert.Equal(t, "screen", group[0].Media)

	assert.Equal(t, "https://example.com/logo.png", group[1].Target)
	assert.True(t, group[1].HasRel(weblink.RelPreload))
	assert.Equal(t, "image", group[1].Params["as"])
}

func TestWebLink_TargetResolution(t *testing.T) {
	t.Parallel()

	l := weblink.New("/api/v2/items", "next")
	resolved := l.ResolveTarget("https://example.com/api/v1/users")
	assert.Equal(t, "https://example.com/api/v2/items", resolved)

	lRel := weblink.New("page2", "next")
	resolvedRel := lRel.ResolveTarget("https://example.com/api/v1/users/")
	assert.Equal(t, "https://example.com/api/v1/users/page2", resolvedRel)
}

func TestWebLink_FormatRoundtrip(t *testing.T) {
	t.Parallel()

	l := weblink.Link{
		Target:   "https://example.com/doc",
		Rel:      "canonical",
		Anchor:   "https://example.com/alias",
		Hreflang: []string{"en", "fr"},
		Media:    "print",
		Title:    "Official Documentation",
		Type:     "text/html",
		Params:   map[string]string{"custom": "value"},
	}

	str := l.String()
	assert.Contains(t, str, "<https://example.com/doc>")
	assert.Contains(t, str, `rel="canonical"`)
	assert.Contains(t, str, `hreflang=en`)
	assert.Contains(t, str, `hreflang=fr`)
	assert.Contains(t, str, `media="print"`)
	assert.Contains(t, str, `title="Official Documentation"`)
	assert.Contains(t, str, `type="text/html"`)
	assert.Contains(t, str, `custom="value"`)

	parsed, err := weblink.Parse(str)
	require.NoError(t, err)
	require.Len(t, parsed, 1)
	assert.Equal(t, l.Target, parsed[0].Target)
	assert.Equal(t, l.Rel, parsed[0].Rel)
	assert.Equal(t, l.Anchor, parsed[0].Anchor)
	assert.Equal(t, l.Hreflang, parsed[0].Hreflang)
	assert.Equal(t, l.Media, parsed[0].Media)
	assert.Equal(t, l.Title, parsed[0].Title)
	assert.Equal(t, l.Type, parsed[0].Type)
	assert.Equal(t, "value", parsed[0].Params["custom"])
}
