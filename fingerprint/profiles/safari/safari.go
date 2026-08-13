// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package safari provides browser profile variants matching Apple Safari / WebKit engines.
package safari

import (
	"crypto/rand"
	"encoding/hex"

	utls "github.com/refraction-networking/utls"

	"github.com/lemon4ksan/aoni/fingerprint/profiles"
)

var defaultHelloID = utls.HelloSafari_16_0

// Various user agent strings for different operating systems.
const (
	UserAgentMacOS = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.6 Safari/605.1.15"
	UserAgentIOS   = "Mozilla/5.0 (iPhone; CPU iPhone OS 26_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.6 Mobile/15E148 Safari/605.1.15"
)

var userAgents = map[profiles.OSKey]string{
	profiles.MacOS: UserAgentMacOS,
	profiles.IOS:   UserAgentIOS,
}

// Desktop is the Safari desktop variant.
var Desktop = &profiles.Variant{
	HelloID:      defaultHelloID,
	BoundaryFunc: Boundary,
	ConfigureH2:  configureH2,
	BuildHeaders: buildHeaders,
	InsertHeaders: func(headers map[string]string, method string) {
		insertHeaders(headers)
	},
}

// Boundary generates a random WebKit boundary string.
func Boundary() string {
	var buf [16]byte

	_, _ = rand.Read(buf[:])

	return "----WebKitFormBoundary" + hex.EncodeToString(buf[:])
}

func configureH2(s *profiles.H2Settings) {
	s.HeaderTableSize = 4096
	s.EnablePush = 0
	s.InitialWindowSize = 2097152
	s.MaxFrameSize = 16384
	s.ConnectionFlow = 10485760
	s.PriorityWeight = 255
}

func buildHeaders(os profiles.OSKey) []profiles.HeaderEntry {
	ua := userAgents[os]
	if ua == "" {
		ua = UserAgentMacOS
	}

	return []profiles.HeaderEntry{
		{Name: ":method", Value: ""},
		{Name: ":scheme", Value: ""},
		{Name: ":path", Value: ""},
		{Name: ":authority", Value: ""},
		{Name: profiles.USER_AGENT, Value: ua},
		{Name: profiles.ACCEPT, Value: "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"},
		{Name: profiles.ACCEPT_LANGUAGE, Value: "en-US,en;q=0.9"},
		{Name: profiles.ACCEPT_ENCODING, Value: "gzip, deflate, br"},
	}
}

func insertHeaders(headers map[string]string) {
	headers[profiles.ACCEPT] = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
}
