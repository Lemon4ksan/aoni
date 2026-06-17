// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package firefox

import (
	"crypto/rand"
	"encoding/binary"
	"strconv"

	utls "github.com/refraction-networking/utls"

	"github.com/lemon4ksan/aoni/profiles"
)

// HelloFirefox148 is the TLS hello for Firefox 148.
var HelloFirefox148 = utls.HelloFirefox_120

// Various user agent strings for different operating systems.
var (
	UserAgentFirefoxWindows = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:148.0) Gecko/20100101 Firefox/148.0"
	UserAgentFirefoxMacOS   = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:148.0) Gecko/20100101 Firefox/148.0"
	UserAgentFirefoxLinux   = "Mozilla/5.0 (X11; Linux x86_64; rv:148.0) Gecko/20100101 Firefox/148.0"
	UserAgentFirefoxAndroid = "Mozilla/5.0 (Android 16; Mobile; rv:148.0) Gecko/148.0 Firefox/148.0"
	UserAgentFirefoxIOS     = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) FxiOS/148.0 Mobile/15E148 Safari/605.1.15"
)

var userAgents = map[profiles.OSKey]string{
	profiles.Windows: UserAgentFirefoxWindows,
	profiles.MacOS:   UserAgentFirefoxMacOS,
	profiles.Linux:   UserAgentFirefoxLinux,
	profiles.Android: UserAgentFirefoxAndroid,
	profiles.IOS:     UserAgentFirefoxIOS,
}

// Desktop is the desktop variant of the Firefox profile.
var Desktop = &profiles.Variant{
	HelloID:      HelloFirefox148,
	BoundaryFunc: Boundary,
	ConfigureH2:  configureH2Desktop,
	ConfigureH3:  configureH3Desktop,
	BuildHeaders: buildHeadersDesktop,
	InsertHeaders: func(headers map[string]string, method string) {
		insertDesktopHeaders(headers, method)
	},
}

// Mobile is the mobile variant of the Firefox profile.
var Mobile = &profiles.Variant{
	HelloID:      HelloFirefox148,
	BoundaryFunc: Boundary,
	ConfigureH2:  configureH2Desktop,
	ConfigureH3:  configureH3Desktop,
	BuildHeaders: buildHeadersMobile,
	InsertHeaders: func(headers map[string]string, method string) {
		insertMobileHeaders(headers, method)
	},
}

// Boundary returns a random boundary string for use in multipart requests.
func Boundary() string {
	prefix := "---------------------------"

	var nums [3]uint32
	for i := range 3 {
		var b [4]byte
		rand.Read(b[:])
		nums[i] = binary.LittleEndian.Uint32(b[:])
	}

	return prefix + strconv.FormatUint(uint64(nums[0]), 10) +
		strconv.FormatUint(uint64(nums[1]), 10) +
		strconv.FormatUint(uint64(nums[2]), 10)
}

func configureH2Desktop(s *profiles.H2Settings) {
	s.InitialStreamID = 3
	s.HeaderTableSize = 65536
	s.EnablePush = 0
	s.InitialWindowSize = 131072
	s.MaxFrameSize = 16384
	s.ConnectionFlow = 12517377
	s.PriorityWeight = 41
}

func configureH3Desktop(s *profiles.H3Settings) {
	s.QpackMaxTableCapacity = 65536
	s.QpackBlockedStreams = 20
	s.EnableWebtransport = 0
	s.H3Datagram = 1
	s.SettingsH3Datagram = 1
	s.EnableConnectProtocol = 1
}

var headerOrderDesktop = map[string][]string{
	"GET": {
		":method", ":path", ":authority", ":scheme",
		profiles.USER_AGENT, profiles.ACCEPT, profiles.ACCEPT_LANGUAGE,
		profiles.ACCEPT_ENCODING, profiles.REFERER, profiles.AUTHORIZATION,
		profiles.COOKIE, profiles.UPGRADE_INSECURE_REQUESTS,
		profiles.SEC_FETCH_DEST, profiles.SEC_FETCH_MODE, profiles.SEC_FETCH_SITE,
		profiles.SEC_FETCH_USER, profiles.PRIORITY,
	},
	"GEThttp3": {
		":method", ":scheme", ":authority", ":path",
		profiles.USER_AGENT, profiles.ACCEPT, profiles.ACCEPT_LANGUAGE,
		profiles.ACCEPT_ENCODING, profiles.REFERER, profiles.AUTHORIZATION,
		profiles.COOKIE, profiles.UPGRADE_INSECURE_REQUESTS,
		profiles.SEC_FETCH_DEST, profiles.SEC_FETCH_MODE, profiles.SEC_FETCH_SITE,
		profiles.SEC_FETCH_USER, profiles.PRIORITY,
	},
	"POST": {
		":method", ":path", ":authority", ":scheme",
		profiles.USER_AGENT, profiles.ACCEPT, profiles.ACCEPT_LANGUAGE,
		profiles.ACCEPT_ENCODING, profiles.REFERER, profiles.CONTENT_TYPE,
		profiles.AUTHORIZATION, profiles.CONTENT_LENGTH, profiles.ORIGIN,
		profiles.COOKIE, profiles.SEC_FETCH_DEST, profiles.SEC_FETCH_MODE,
		profiles.SEC_FETCH_SITE, profiles.PRIORITY, profiles.PRAGMA,
		profiles.CACHE_CONTROL,
	},
	"POSThttp3": {
		":method", ":scheme", ":authority", ":path",
		profiles.USER_AGENT, profiles.ACCEPT, profiles.ACCEPT_LANGUAGE,
		profiles.ACCEPT_ENCODING, profiles.REFERER, profiles.CONTENT_TYPE,
		profiles.AUTHORIZATION, profiles.CONTENT_LENGTH, profiles.ORIGIN,
		profiles.COOKIE, profiles.SEC_FETCH_DEST, profiles.SEC_FETCH_MODE,
		profiles.SEC_FETCH_SITE, profiles.PRIORITY, profiles.PRAGMA,
		profiles.CACHE_CONTROL,
	},
}

var headerOrderMobile = map[string][]string{
	"GET": {
		":method", ":path", ":authority", ":scheme",
		profiles.USER_AGENT, profiles.ACCEPT, profiles.ACCEPT_LANGUAGE,
		profiles.ACCEPT_ENCODING, profiles.REFERER, profiles.AUTHORIZATION,
		profiles.COOKIE, profiles.UPGRADE_INSECURE_REQUESTS,
		profiles.SEC_FETCH_DEST, profiles.SEC_FETCH_MODE, profiles.SEC_FETCH_SITE,
		profiles.SEC_FETCH_USER, profiles.PRIORITY,
	},
	"GEThttp3": {
		":method", ":scheme", ":authority", ":path",
		profiles.USER_AGENT, profiles.ACCEPT, profiles.ACCEPT_LANGUAGE,
		profiles.ACCEPT_ENCODING, profiles.REFERER, profiles.AUTHORIZATION,
		profiles.COOKIE, profiles.UPGRADE_INSECURE_REQUESTS,
		profiles.SEC_FETCH_DEST, profiles.SEC_FETCH_MODE, profiles.SEC_FETCH_SITE,
		profiles.SEC_FETCH_USER, profiles.PRIORITY,
	},
	"POST": {
		":method", ":path", ":authority", ":scheme",
		profiles.USER_AGENT, profiles.ACCEPT, profiles.ACCEPT_LANGUAGE,
		profiles.ACCEPT_ENCODING, profiles.REFERER, profiles.CONTENT_TYPE,
		profiles.AUTHORIZATION, profiles.CONTENT_LENGTH, profiles.ORIGIN,
		profiles.COOKIE, profiles.SEC_FETCH_DEST, profiles.SEC_FETCH_MODE,
		profiles.SEC_FETCH_SITE, profiles.PRIORITY, profiles.PRAGMA,
		profiles.CACHE_CONTROL,
	},
	"POSThttp3": {
		":method", ":scheme", ":authority", ":path",
		profiles.USER_AGENT, profiles.ACCEPT, profiles.ACCEPT_LANGUAGE,
		profiles.ACCEPT_ENCODING, profiles.REFERER, profiles.CONTENT_TYPE,
		profiles.AUTHORIZATION, profiles.CONTENT_LENGTH, profiles.ORIGIN,
		profiles.COOKIE, profiles.SEC_FETCH_DEST, profiles.SEC_FETCH_MODE,
		profiles.SEC_FETCH_SITE, profiles.PRIORITY, profiles.PRAGMA,
		profiles.CACHE_CONTROL,
	},
}

// HeaderCache is the header cache for the Firefox profile.
var HeaderCache = profiles.NewHeaderCache(headerOrderDesktop, headerOrderMobile)

func buildHeadersDesktop(os profiles.OSKey) []profiles.HeaderEntry {
	ua := userAgents[os]

	return []profiles.HeaderEntry{
		{Name: ":authority", Value: ""},
		{Name: ":method", Value: ""},
		{Name: ":path", Value: ""},
		{Name: ":scheme", Value: ""},
		{Name: profiles.ACCEPT_ENCODING, Value: "gzip, deflate, br, zstd"},
		{Name: profiles.ACCEPT_LANGUAGE, Value: "en-US,en;q=0.5"},
		{Name: profiles.AUTHORIZATION, Value: ""},
		{Name: profiles.COOKIE, Value: ""},
		{Name: profiles.ORIGIN, Value: ""},
		{Name: profiles.REFERER, Value: ""},
		{Name: profiles.USER_AGENT, Value: ua},
	}
}

func buildHeadersMobile(os profiles.OSKey) []profiles.HeaderEntry {
	ua := userAgents[os]

	return []profiles.HeaderEntry{
		{Name: ":authority", Value: ""},
		{Name: ":method", Value: ""},
		{Name: ":path", Value: ""},
		{Name: ":scheme", Value: ""},
		{Name: profiles.ACCEPT_ENCODING, Value: "gzip, deflate, br, zstd"},
		{Name: profiles.ACCEPT_LANGUAGE, Value: "en-US,en;q=0.5"},
		{Name: profiles.AUTHORIZATION, Value: ""},
		{Name: profiles.COOKIE, Value: ""},
		{Name: profiles.ORIGIN, Value: ""},
		{Name: profiles.REFERER, Value: ""},
		{Name: profiles.USER_AGENT, Value: ua},
	}
}

func insertDesktopHeaders(headers map[string]string, method string) {
	switch method {
	case "POST":
		headers[profiles.ACCEPT] = "*/*"
		headers[profiles.CACHE_CONTROL] = "no-cache"
		headers[profiles.CONTENT_TYPE] = ""
		headers[profiles.CONTENT_LENGTH] = ""
		headers[profiles.PRAGMA] = "no-cache"
		headers[profiles.PRIORITY] = "u=1, i"
		headers[profiles.SEC_FETCH_DEST] = "empty"
		headers[profiles.SEC_FETCH_MODE] = "cors"
		headers[profiles.SEC_FETCH_SITE] = "same-origin"

	default:
		headers[profiles.ACCEPT] = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
		headers[profiles.PRIORITY] = "u=0, i"
		headers[profiles.SEC_FETCH_DEST] = "document"
		headers[profiles.SEC_FETCH_MODE] = "navigate"
		headers[profiles.SEC_FETCH_SITE] = "none"
		headers[profiles.SEC_FETCH_USER] = "?1"
		headers[profiles.UPGRADE_INSECURE_REQUESTS] = "1"
	}
}

func insertMobileHeaders(headers map[string]string, method string) {
	switch method {
	case "POST":
		headers[profiles.ACCEPT] = "*/*"
		headers[profiles.CACHE_CONTROL] = "no-cache"
		headers[profiles.CONTENT_TYPE] = ""
		headers[profiles.CONTENT_LENGTH] = ""
		headers[profiles.PRAGMA] = "no-cache"
		headers[profiles.PRIORITY] = "u=1, i"
		headers[profiles.SEC_FETCH_DEST] = "empty"
		headers[profiles.SEC_FETCH_MODE] = "cors"
		headers[profiles.SEC_FETCH_SITE] = "same-origin"

	default:
		headers[profiles.ACCEPT] = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
		headers[profiles.PRIORITY] = "u=0, i"
		headers[profiles.SEC_FETCH_DEST] = "document"
		headers[profiles.SEC_FETCH_MODE] = "navigate"
		headers[profiles.SEC_FETCH_SITE] = "none"
		headers[profiles.SEC_FETCH_USER] = "?1"
		headers[profiles.UPGRADE_INSECURE_REQUESTS] = "1"
	}
}
