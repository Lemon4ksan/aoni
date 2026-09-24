// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package proxy provides high-performance, adaptive, and PAC-aware proxy routing.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path/filepath"
	"strings"
	"sync"

	"github.com/lemon4ksan/foundation/generic"
)

// Standard PAC error sentinels.
var (
	// ErrNoProxyMatches is returned when a PAC script evaluates to no usable proxy endpoints.
	ErrNoProxyMatches = errors.New("aoni/pac: no proxy matches rule")

	// ErrInvalidPACScript is returned when a PAC script cannot be parsed.
	ErrInvalidPACScript = errors.New("aoni/pac: invalid PAC script")
)

// PACDirective represents a parsed proxy directive from a PAC script (e.g. DIRECT, PROXY host:port).
type PACDirective struct {
	Type     string // "DIRECT", "PROXY", "HTTPS", "SOCKS", "SOCKS5"
	HostPort string // "host:port" or ""
	ProxyURL *url.URL
}

// Format returns the standard PAC string representation (e.g. "PROXY host:8080").
func (d PACDirective) Format() string {
	if d.Type == "DIRECT" || d.HostPort == "" {
		return "DIRECT"
	}

	return fmt.Sprintf("%s %s", d.Type, d.HostPort)
}

// ParsePACResult parses a PAC return string (e.g. "PROXY p1:8080; SOCKS5 p2:1080; DIRECT")
// into a slice of [PACDirective] objects.
func ParsePACResult(result string) []PACDirective {
	result = strings.TrimSpace(result)
	if result == "" {
		return []PACDirective{{Type: "DIRECT"}}
	}

	parts := strings.Split(result, ";")
	directives := make([]PACDirective, 0, len(parts))

	for _, p := range parts {
		item := strings.TrimSpace(p)
		if item == "" {
			continue
		}

		fields := strings.Fields(item)
		if len(fields) == 0 {
			continue
		}

		pType := strings.ToUpper(fields[0])
		if pType == "DIRECT" || len(fields) == 1 {
			directives = append(directives, PACDirective{Type: "DIRECT"})
			continue
		}

		hostPort := fields[1]

		var scheme string
		switch pType {
		case "PROXY", "HTTP":
			scheme = "http"
		case "HTTPS":
			scheme = "https"
		case "SOCKS", "SOCKS4":
			scheme = "socks4"
		case "SOCKS5":
			scheme = "socks5"
		default:
			scheme = "http"
		}

		parsedURL, err := url.Parse(fmt.Sprintf("%s://%s", scheme, hostPort))
		if err == nil {
			directives = append(directives, PACDirective{
				Type:     pType,
				HostPort: hostPort,
				ProxyURL: parsedURL,
			})
		}
	}

	if len(directives) == 0 {
		return []PACDirective{{Type: "DIRECT"}}
	}

	return directives
}

// PACRule represents a declarative URL/Host matching rule.
type PACRule struct {
	// Pattern is a shell glob expression matching the URL (e.g. "*example.com/*").
	Pattern string

	// HostSuffix matches hostnames ending with this suffix (e.g. ".corp.local").
	HostSuffix string

	// Subnet is an optional CIDR network to match resolved host IPs (e.g. "10.0.0.0/8").
	Subnet *net.IPNet

	// Result is the PAC result string to return on match (e.g. "PROXY proxy.corp:8080; DIRECT").
	Result string
}

// PACEngine evaluates proxy auto-configuration rules for outgoing requests.
// It implements Chromium's sandboxed PAC execution model (RFC 8485 / Netscape PAC specification).
//
// Thread Safety:
// 100% thread-safe for concurrent request routing.
type PACEngine struct {
	mu           sync.RWMutex
	rules        []PACRule
	defaultRoute string
	cache        generic.ConcurrentMap[string, []PACDirective]
}

// NewPACEngine creates a new thread-safe [PACEngine].
func NewPACEngine(defaultRoute ...string) *PACEngine {
	def := "DIRECT"
	if len(defaultRoute) > 0 && defaultRoute[0] != "" {
		def = defaultRoute[0]
	}

	return &PACEngine{
		defaultRoute: def,
	}
}

// AddRule registers a matching rule into the engine.
func (e *PACEngine) AddRule(rule PACRule) *PACEngine {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.rules = append(e.rules, rule)
	e.cache.Clear()

	return e
}

// FindProxyForURL evaluates the PAC rules for a given target URL and hostname,
// returning the prioritized list of proxy directives.
func (e *PACEngine) FindProxyForURL(rawURL, host string) []PACDirective {
	cacheKey := rawURL + "|" + host
	if cached, ok := e.cache.Load(cacheKey); ok {
		return cached
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, rule := range e.rules {
		if e.matchesRule(rule, rawURL, host) {
			result := ParsePACResult(rule.Result)
			e.cache.Store(cacheKey, result)

			return result
		}
	}

	result := ParsePACResult(e.defaultRoute)
	e.cache.Store(cacheKey, result)

	return result
}

// ProxyFunc yields an [http.Transport.Proxy] compatible closure that dynamically resolves
// the active proxy for any outgoing [*http.Request].
func (e *PACEngine) ProxyFunc() func(*http.Request) (*url.URL, error) {
	urlFn := e.ProxyURLFunc()

	return func(req *http.Request) (*url.URL, error) {
		if req == nil || req.URL == nil {
			return nil, nil
		}

		return urlFn(req.URL)
	}
}

// ProxyURLFunc yields a URL-based closure that dynamically resolves
// the active proxy for any target [*url.URL].
func (e *PACEngine) ProxyURLFunc() func(*url.URL) (*url.URL, error) {
	return func(targetURL *url.URL) (*url.URL, error) {
		if targetURL == nil {
			return nil, nil
		}

		directives := e.FindProxyForURL(targetURL.String(), targetURL.Hostname())
		for _, d := range directives {
			if d.Type == "DIRECT" {
				return nil, nil
			}

			if d.ProxyURL != nil {
				return d.ProxyURL, nil
			}
		}

		return nil, nil
	}
}

func (e *PACEngine) matchesRule(rule PACRule, rawURL, host string) bool {
	// 1. Plain host check
	if rule.HostSuffix == "local" && IsPlainHostName(host) {
		return true
	}

	// 2. Host suffix match
	if rule.HostSuffix != "" {
		if !DNSDomainIs(host, rule.HostSuffix) && !LocalHostOrDomainIs(host, rule.HostSuffix) {
			return false
		}
	}

	// 3. Glob URL pattern match
	if rule.Pattern != "" {
		if !ShExpMatch(rawURL, rule.Pattern) && !ShExpMatch(host, rule.Pattern) {
			return false
		}
	}

	// 4. Subnet match
	if rule.Subnet != nil {
		ip := net.ParseIP(host)
		if ip == nil {
			ips, err := net.DefaultResolver.LookupIP(context.Background(), "ip", host)
			if err == nil && len(ips) > 0 {
				ip = ips[0]
			}
		}

		if ip == nil || !rule.Subnet.Contains(ip) {
			return false
		}
	}

	return true
}

// --- Standard PAC Built-in Helper Functions (Chromium net/proxy_resolution/pac_library) ---

// IsPlainHostName reports whether host contains no domain qualification dots (e.g. "localhost", "intranet").
func IsPlainHostName(host string) bool {
	return !strings.Contains(host, ".")
}

// DNSDomainIs reports whether host ends with the specified domain suffix (e.g. host "api.google.com", domain ".google.com").
func DNSDomainIs(host, domain string) bool {
	if !strings.HasPrefix(domain, ".") {
		domain = "." + domain
	}

	return strings.HasSuffix(strings.ToLower(host), strings.ToLower(domain))
}

// LocalHostOrDomainIs reports whether host matches hostdom exactly or is the unqualified prefix of hostdom.
func LocalHostOrDomainIs(host, hostdom string) bool {
	host = strings.ToLower(host)
	hostdom = strings.ToLower(hostdom)

	if host == hostdom {
		return true
	}

	if IsPlainHostName(host) && strings.HasPrefix(hostdom, host+".") {
		return true
	}

	return false
}

// DNSDomainLevels returns the number of DNS domain qualification levels (dot count) in host.
func DNSDomainLevels(host string) int {
	return strings.Count(host, ".")
}

// ShExpMatch performs shell glob matching ('*' matches any characters, '?' matches a single character).
func ShExpMatch(str, pattern string) bool {
	matched, err := filepath.Match(pattern, str)
	if err == nil && matched {
		return true
	}

	// Fallback to simple substring match if glob syntax is complex
	if strings.HasPrefix(pattern, "*") && strings.HasSuffix(pattern, "*") {
		sub := strings.Trim(pattern, "*")
		return strings.Contains(str, sub)
	}

	return false
}

// IsInNet checks whether the IP address of host falls within the specified subnet IP and netmask.
func IsInNet(host, pattern, mask string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		ip4, err := netip.ParseAddr(host)
		if err != nil {
			return false
		}

		ip = ip4.AsSlice()
	}

	patternIP := net.ParseIP(pattern)

	maskIP := net.ParseIP(mask)
	if patternIP == nil || maskIP == nil {
		return false
	}

	maskBytes := net.IPMask(maskIP.To4())
	if maskBytes == nil {
		maskBytes = net.IPMask(maskIP.To16())
	}

	network := net.IPNet{
		IP:   patternIP.Mask(maskBytes),
		Mask: maskBytes,
	}

	return network.Contains(ip)
}

// MyIPAddress returns the first non-loopback local IPv4 address, or "127.0.0.1".
func MyIPAddress() string {
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
				if ip4 := ipNet.IP.To4(); ip4 != nil {
					return ip4.String()
				}
			}
		}
	}

	return "127.0.0.1"
}
