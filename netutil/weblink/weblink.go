// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package weblink implements Web Linking and Link HTTP header serialization strictly conforming to RFC 8288 (obsoletes RFC 5988).
//
// It provides robust parsing, formatting, and relational query operations for Link headers,
// including API pagination (rel="next", rel="prev"), resource hints (rel="preload"),
// context anchors, and internationalized title* attributes (RFC 8187).
package weblink

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/lemon4ksan/aoni/netutil/sanitize"
)

// Header is the standard HTTP header field name for Web Linking (RFC 8288 §3).
const Header = "Link"

// Standard IANA registered link relation types per RFC 8288 §4.2.
const (
	// RelNext refers to the next resource in an ordered series of resources (RFC 8288 §4.2).
	RelNext = "next"

	// RelPrev refers to the previous resource in an ordered series of resources (RFC 8288 §4.2).
	RelPrev = "prev"

	// RelPrevious is the synonym for "prev" (RFC 8288 §4.2).
	RelPrevious = "previous"

	// RelFirst refers to the first resource in an ordered series of resources (RFC 8288 §4.2).
	RelFirst = "first"

	// RelLast refers to the furthest following resource in an ordered series of resources (RFC 8288 §4.2).
	RelLast = "last"

	// RelCanonical refers to a preferred URI for the resource (RFC 6596 / RFC 8288 §4.2).
	RelCanonical = "canonical"

	// RelAlternate refers to a substitute for this context (RFC 8288 §4.2).
	RelAlternate = "alternate"

	// RelAuthor refers to the author of the context (RFC 8288 §4.2).
	RelAuthor = "author"

	// RelHelp refers to context-sensitive help (RFC 8288 §4.2).
	RelHelp = "help"

	// RelIcon refers to an icon that represents the context (RFC 8288 §4.2).
	RelIcon = "icon"

	// RelLicense refers to a license associated with this context (RFC 8288 §4.2).
	RelLicense = "license"

	// RelPreload refers to a resource that should be loaded early in the processing lifecycle (RFC 8288 §4.2).
	RelPreload = "preload"

	// RelPrefetch refers to a resource that should be fetched preemptively (RFC 8288 §4.2).
	RelPrefetch = "prefetch"

	// RelPreconnect refers to a resource origin to which the client should preemptively connect (RFC 8288 §4.2).
	RelPreconnect = "preconnect"

	// RelDNSPrefetch indicates that the client should preemptively perform DNS resolution for the target (RFC 8288 §4.2).
	RelDNSPrefetch = "dns-prefetch"

	// RelStylesheet refers to an external stylesheet (RFC 8288 §4.2).
	RelStylesheet = "stylesheet"

	// RelService refers to a service description associated with the context (RFC 8288 §4.2).
	RelService = "service"

	// RelPayment refers to payment terms or resources (RFC 8288 §4.2).
	RelPayment = "payment"

	// RelSearch refers to a resource that can be used to search through the current context (RFC 8288 §4.2).
	RelSearch = "search"

	// RelSelf refers to a context that is identical to the current resource (RFC 4287 / RFC 8288 §4.2).
	RelSelf = "self"

	// RelUp refers to a parent document in a hierarchy of documents (RFC 8288 §4.2).
	RelUp = "up"
)

var (
	// ErrEmptyHeader is returned when attempting to parse an empty Link header.
	ErrEmptyHeader = errors.New("aoni/weblink: empty Link header")

	// ErrInvalidHeader is returned when the Link header syntax is malformed (RFC 8288 §3).
	ErrInvalidHeader = errors.New("aoni/weblink: malformed Link header value")
)

// Link represents a typed connection between two Web resources conforming to RFC 8288 §2.
type Link struct {
	// Target is the URI-Reference of the destination resource enclosed in "<>" (RFC 8288 §3.1).
	Target string

	// Rel is the relation type string (RFC 8288 §3.3). May contain multiple space-separated types.
	Rel string

	// Rels is the parsed, lowercase-normalized list of relation types for this link (RFC 8288 §3.3).
	Rels []string

	// Anchor is an optional URI-Reference overriding the link's context (RFC 8288 §3.2).
	Anchor string

	// Hreflang contains language hints for the target resource (RFC 8288 §3.4.1).
	Hreflang []string

	// Media is a media-query-list hint for style destinations (RFC 8288 §3.4.1).
	Media string

	// Title is a human-readable label for the link destination (RFC 8288 §3.4.1).
	Title string

	// TitleLang is the RFC 5646 language tag if title was decoded from an RFC 8187 title* parameter.
	TitleLang string

	// Type is a media type hint for the target representation (RFC 8288 §3.4.1).
	Type string

	// Params contains all raw key/value target attributes and extensions (RFC 8288 §3.4.2).
	Params map[string]string
}

// New creates a new [Link] with target URI and primary relation type.
func New(target, rel string) Link {
	l := Link{
		Target: target,
		Rel:    rel,
		Params: make(map[string]string),
	}
	l.splitRels()

	return l
}

func (l *Link) splitRels() {
	parts := strings.Fields(l.Rel)

	l.Rels = make([]string, 0, len(parts))
	for _, p := range parts {
		norm := strings.ToLower(p)
		if norm != "" {
			l.Rels = append(l.Rels, norm)
		}
	}
}

// HasRel reports whether this link includes the specified relation type (case-insensitive, RFC 8288 §2.1.1).
func (l Link) HasRel(rel string) bool {
	norm := strings.ToLower(strings.TrimSpace(rel))
	return slices.Contains(l.Rels, norm)
}

// ResolveTarget resolves a relative Target URI against baseURI as per RFC 3986 §5.2 (RFC 8288 §3.1).
func (l Link) ResolveTarget(baseURI string) string {
	if baseURI == "" {
		return l.Target
	}

	base, errBase := url.Parse(baseURI)
	if errBase != nil {
		return l.Target
	}

	target, errTarget := url.Parse(l.Target)
	if errTarget != nil {
		return l.Target
	}

	return base.ResolveReference(target).String()
}

// String formats the link into a standard RFC 8288 HTTP link-value string (RFC 8288 §3).
func (l Link) String() string {
	var sb strings.Builder
	l.appendTo(&sb)
	return sb.String()
}

func (l Link) appendTo(sb *strings.Builder) {
	sb.WriteByte('<')
	sb.WriteString(l.Target)
	sb.WriteByte('>')

	if l.Rel != "" {
		sb.WriteString("; rel=\"")
		sb.WriteString(l.Rel)
		sb.WriteByte('"')
	}

	if l.Anchor != "" {
		sb.WriteString("; anchor=\"")
		sb.WriteString(l.Anchor)
		sb.WriteByte('"')
	}

	for _, lang := range l.Hreflang {
		sb.WriteString("; hreflang=")
		sb.WriteString(lang)
	}

	if l.Media != "" {
		sb.WriteString("; media=\"")
		sb.WriteString(l.Media)
		sb.WriteByte('"')
	}

	if l.Title != "" {
		if l.TitleLang != "" {
			sb.WriteString("; title*=")
			sb.WriteString(sanitize.EncodeRFC8187(l.Title, l.TitleLang))
		} else {
			sb.WriteString("; title=\"")
			sb.WriteString(strings.ReplaceAll(l.Title, "\"", "\\\""))
			sb.WriteByte('"')
		}
	}

	if l.Type != "" {
		sb.WriteString("; type=\"")
		sb.WriteString(l.Type)
		sb.WriteByte('"')
	}

	// Extension parameters
	for k, v := range l.Params {
		switch k {
		case "rel", "anchor", "hreflang", "media", "title", "title*", "type":
			continue
		default:
			sb.WriteString("; ")
			sb.WriteString(k)

			if v != "" {
				sb.WriteString("=\"")
				sb.WriteString(strings.ReplaceAll(v, "\"", "\\\""))
				sb.WriteByte('"')
			}
		}
	}
}

// Group represents an ordered collection of [Link] objects parsed from HTTP headers (RFC 8288 §3).
type Group []Link

// ByRel returns the first link matching relation type (case-insensitive, RFC 8288 §2.1.1).
func (g Group) ByRel(rel string) (Link, bool) {
	norm := strings.ToLower(strings.TrimSpace(rel))
	for i := range g {
		if g[i].HasRel(norm) {
			return g[i], true
		}
	}

	return Link{}, false
}

// AllByRel returns all links matching relation type (RFC 8288 §2.1.1).
func (g Group) AllByRel(rel string) []Link {
	norm := strings.ToLower(strings.TrimSpace(rel))

	var matches []Link
	for i := range g {
		if g[i].HasRel(norm) {
			matches = append(matches, g[i])
		}
	}

	return matches
}

// Next returns the target URI of the next page in pagination (rel="next", RFC 8288 §4.2).
func (g Group) Next() (string, bool) {
	if l, ok := g.ByRel(RelNext); ok {
		return l.Target, true
	}

	return "", false
}

// Prev returns the target URI of the previous page in pagination (rel="prev" or rel="previous", RFC 8288 §4.2).
func (g Group) Prev() (string, bool) {
	if l, ok := g.ByRel(RelPrev); ok {
		return l.Target, true
	}

	if l, ok := g.ByRel(RelPrevious); ok {
		return l.Target, true
	}

	return "", false
}

// First returns the target URI of the first page in pagination (rel="first", RFC 8288 §4.2).
func (g Group) First() (string, bool) {
	if l, ok := g.ByRel(RelFirst); ok {
		return l.Target, true
	}

	return "", false
}

// Last returns the target URI of the last page in pagination (rel="last", RFC 8288 §4.2).
func (g Group) Last() (string, bool) {
	if l, ok := g.ByRel(RelLast); ok {
		return l.Target, true
	}

	return "", false
}

// Canonical returns the target URI of the canonical resource (rel="canonical", RFC 6596 / RFC 8288 §4.2).
func (g Group) Canonical() (string, bool) {
	if l, ok := g.ByRel(RelCanonical); ok {
		return l.Target, true
	}

	return "", false
}

// String formats the entire group into a single comma-separated Link header value (RFC 8288 §3).
func (g Group) String() string {
	if len(g) == 0 {
		return ""
	}

	var sb strings.Builder
	for i := range g {
		if i > 0 {
			sb.WriteString(", ")
		}

		g[i].appendTo(&sb)
	}

	return sb.String()
}

// Parse extracts Web Links from a Link HTTP header string according to RFC 8288 Appendix B algorithms.
func Parse(header string) (Group, error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return nil, ErrEmptyHeader
	}

	var links Group

	rawLinks := splitHeaderLinks(header)
	if len(rawLinks) == 0 {
		return nil, ErrInvalidHeader
	}

	for _, raw := range rawLinks {
		l, err := parseLinkValue(raw)
		if err != nil {
			continue
		}

		links = append(links, l)
	}

	if len(links) == 0 {
		return nil, ErrInvalidHeader
	}

	return links, nil
}

// ParseHeader extracts Web Links from a standard [http.Header] map across all "Link" headers (RFC 8288 Appendix B.1).
func ParseHeader(h http.Header) (Group, error) {
	vals := h[Header]
	if len(vals) == 0 {
		for k, v := range h {
			if strings.EqualFold(k, Header) && len(v) > 0 {
				vals = v
				break
			}
		}
	}

	if len(vals) == 0 {
		return nil, ErrEmptyHeader
	}

	var allLinks Group
	for _, val := range vals {
		g, err := Parse(val)
		if err != nil {
			continue
		}

		allLinks = append(allLinks, g...)
	}

	if len(allLinks) == 0 {
		return nil, ErrInvalidHeader
	}

	return allLinks, nil
}

// splitHeaderLinks splits comma-separated link values while ignoring commas inside quoted strings or angle brackets.
func splitHeaderLinks(header string) []string {
	var links []string

	start := 0
	inQuotes := false
	inAngle := false

	for i := 0; i < len(header); i++ {
		c := header[i]
		if c == '\\' && inQuotes && i+1 < len(header) {
			i++
			continue
		}

		switch {
		case c == '"' && !inAngle:
			inQuotes = !inQuotes
		case c == '<' && !inQuotes:
			inAngle = true
		case c == '>' && !inQuotes:
			inAngle = false
		case c == ',' && !inQuotes && !inAngle:
			item := strings.TrimSpace(header[start:i])
			if item != "" {
				links = append(links, item)
			}

			start = i + 1
		}
	}

	if start < len(header) {
		item := strings.TrimSpace(header[start:])
		if item != "" {
			links = append(links, item)
		}
	}

	return links
}

// parseLinkValue parses a single "<URI-Reference>; param1=val1; ..." string (RFC 8288 Appendix B.2).
func parseLinkValue(s string) (Link, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "<") {
		return Link{}, ErrInvalidHeader
	}

	endTarget := strings.IndexByte(s, '>')
	if endTarget < 0 {
		return Link{}, ErrInvalidHeader
	}

	target := s[1:endTarget]
	paramsPart := strings.TrimSpace(s[endTarget+1:])

	l := Link{
		Target: target,
		Params: make(map[string]string),
	}

	if paramsPart == "" {
		return l, nil
	}

	// Parse parameters (RFC 8288 Appendix B.3)
	params := parseParams(paramsPart)

	var (
		titleStar, titlePlain string
		titleStarLang         string
	)

	for _, p := range params {
		k := strings.ToLower(p.Key)
		v := p.Value

		l.Params[k] = v

		switch {
		case k == "rel" && l.Rel == "":
			l.Rel = v
			l.splitRels()

		case k == "anchor" && l.Anchor == "":
			l.Anchor = v

		case k == "hreflang":
			l.Hreflang = append(l.Hreflang, v)

		case k == "media" && l.Media == "":
			l.Media = v

		case k == "type" && l.Type == "":
			l.Type = v

		case k == "title" && titlePlain == "":
			titlePlain = v

		case k == "title*" && titleStar == "":
			// RFC 8187 decoding (RFC 8288 §3.4.1 & Appendix B.3.7.5)
			_, lang, val, err := sanitize.DecodeRFC8187(v)
			if err == nil {
				titleStar = val
				titleStarLang = lang
			} else {
				titleStar = sanitize.DecodeRFC8187Value(v)
			}
		}
	}

	// RFC 8288 §3.4.1: If both title and title* appear, title* takes precedence
	if titleStar != "" {
		l.Title = titleStar
		l.TitleLang = titleStarLang
	} else {
		l.Title = titlePlain
	}

	return l, nil
}

type paramTuple struct {
	Key   string
	Value string
}

func parseParams(s string) []paramTuple {
	var tuples []paramTuple

	start := 0
	inQuote := false

	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && inQuote && i+1 < len(s) {
			i++
			continue
		}

		if c == '"' {
			inQuote = !inQuote
		} else if c == ';' && !inQuote {
			chunk := strings.TrimSpace(s[start:i])
			if chunk != "" {
				tuples = append(tuples, parseSingleParam(chunk))
			}

			start = i + 1
		}
	}

	if start < len(s) {
		chunk := strings.TrimSpace(s[start:])
		if chunk != "" {
			tuples = append(tuples, parseSingleParam(chunk))
		}
	}

	return tuples
}

func parseSingleParam(s string) paramTuple {
	s = strings.TrimLeft(s, "; ")

	eqIdx := strings.IndexByte(s, '=')
	if eqIdx < 0 {
		return paramTuple{Key: strings.TrimSpace(s), Value: ""}
	}

	k := strings.TrimSpace(s[:eqIdx])
	v := strings.TrimSpace(s[eqIdx+1:])

	// Handle quoted strings unescaping (RFC 8288 Appendix B.4)
	if strings.HasPrefix(v, "\"") && strings.HasSuffix(v, "\"") && len(v) >= 2 {
		v = unescapeQuoted(v[1 : len(v)-1])
	}

	return paramTuple{Key: k, Value: v}
}

func unescapeQuoted(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}

	var sb strings.Builder
	sb.Grow(len(s))

	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			sb.WriteByte(s[i])
		} else {
			sb.WriteByte(s[i])
		}
	}

	return sb.String()
}
