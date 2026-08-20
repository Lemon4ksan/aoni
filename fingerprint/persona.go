// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package fingerprint provides multi-layer network fingerprint evasion and anti-DPI utilities.
package fingerprint

import (
	utls "github.com/refraction-networking/utls"

	"github.com/lemon4ksan/aoni/fingerprint/h2"
	"github.com/lemon4ksan/aoni/fingerprint/p0f"
)

// DefaultUserAgent is the fallback User-Agent header string.
const DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// Persona represents an immutable set of fingerprint parameters matching all network layers
// (TCP/IP stack, TLS ClientHello, HTTP/2 settings, header order, and User-Agent).
type Persona struct {
	TLSID        utls.ClientHelloID
	H2Settings   h2.Settings
	UserAgent    string
	HeaderOrder  []string
	P0fSignature *p0f.Signature
}

var (
	// PersonaChrome120Windows mimics Google Chrome 120 on Windows.
	PersonaChrome120Windows = Persona{
		TLSID:      utls.HelloChrome_120,
		H2Settings: h2.ChromeSettings,
		UserAgent:  DefaultUserAgent,
		HeaderOrder: []string{
			":method", ":authority", ":scheme", ":path",
			"sec-ch-ua", "sec-ch-ua-mobile", "sec-ch-ua-platform",
			"upgrade-insecure-requests", "user-agent", "accept",
			"sec-fetch-site", "sec-fetch-mode", "sec-fetch-user",
			"sec-fetch-dest", "referer", "accept-encoding",
			"accept-language", "cookie", "priority",
		},
		P0fSignature: p0f.Windows10,
	}

	// PersonaChrome120Android mimics Google Chrome 120 on Android.
	PersonaChrome120Android = Persona{
		TLSID:      utls.HelloChrome_120,
		H2Settings: h2.ChromeSettings,
		UserAgent:  "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36",
		HeaderOrder: []string{
			":method", ":authority", ":scheme", ":path",
			"sec-ch-ua", "sec-ch-ua-mobile", "sec-ch-ua-platform",
			"upgrade-insecure-requests", "user-agent", "accept",
			"sec-fetch-site", "sec-fetch-mode", "sec-fetch-user",
			"sec-fetch-dest", "referer", "accept-encoding",
			"accept-language", "cookie", "priority",
		},
		P0fSignature: p0f.Android,
	}

	// PersonaFirefox120Windows mimics Mozilla Firefox 120 on Windows.
	PersonaFirefox120Windows = Persona{
		TLSID:      utls.HelloFirefox_120,
		H2Settings: h2.FirefoxSettings,
		UserAgent:  "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:120.0) Gecko/20100101 Firefox/120.0",
		HeaderOrder: []string{
			":method", ":path", ":authority", ":scheme",
			"user-agent", "accept", "accept-language",
			"accept-encoding", "referer", "cookie",
			"upgrade-insecure-requests", "sec-fetch-dest",
			"sec-fetch-mode", "sec-fetch-site", "sec-fetch-user",
			"priority",
		},
		P0fSignature: p0f.Windows10,
	}

	// PersonaFirefox120Android mimics Mozilla Firefox 120 on Android.
	PersonaFirefox120Android = Persona{
		TLSID:      utls.HelloFirefox_120,
		H2Settings: h2.FirefoxSettings,
		UserAgent:  "Mozilla/5.0 (Android 13; Mobile; rv:120.0) Gecko/120.0 Firefox/120.0",
		HeaderOrder: []string{
			":method", ":path", ":authority", ":scheme",
			"user-agent", "accept", "accept-language",
			"accept-encoding", "referer", "cookie",
			"upgrade-insecure-requests", "sec-fetch-dest",
			"sec-fetch-mode", "sec-fetch-site", "sec-fetch-user",
			"priority",
		},
		P0fSignature: p0f.Android,
	}

	// PersonaSafari17MacOS mimics Apple Safari 17 on macOS.
	PersonaSafari17MacOS = Persona{
		TLSID: utls.HelloSafari_16_0, // closest Safari hello ID available in uTLS v1.8.2
		H2Settings: h2.Settings{
			HeaderTableSize:   4096,
			EnablePush:        0,
			InitialWindowSize: 2097152,
			MaxFrameSize:      16384,
			ConnectionFlow:    10485760,
			PriorityWeight:    255,
		},
		UserAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15",
		HeaderOrder: []string{
			":method", ":scheme", ":path", ":authority",
			"user-agent", "accept", "accept-language",
			"accept-encoding", "referer", "cookie",
		},
		P0fSignature: p0f.MacOS,
	}

	// PersonaSafari17IOS mimics Apple Safari 17 on iOS.
	PersonaSafari17IOS = Persona{
		TLSID: utls.HelloSafari_16_0, // closest Safari hello ID available in uTLS v1.8.2
		H2Settings: h2.Settings{
			HeaderTableSize:   4096,
			EnablePush:        0,
			InitialWindowSize: 2097152,
			MaxFrameSize:      16384,
			ConnectionFlow:    10485760,
			PriorityWeight:    255,
		},
		UserAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/605.1.15",
		HeaderOrder: []string{
			":method", ":scheme", ":path", ":authority",
			"user-agent", "accept", "accept-language",
			"accept-encoding", "referer", "cookie",
		},
		P0fSignature: p0f.IOS,
	}

	// PersonaChromeWindows aliases the modern Chrome Windows persona.
	PersonaChromeWindows = PersonaChrome120Windows
	// PersonaChromeAndroid aliases the modern Chrome Android persona.
	PersonaChromeAndroid = PersonaChrome120Android
	// PersonaFirefoxWindows aliases the modern Firefox Windows persona.
	PersonaFirefoxWindows = PersonaFirefox120Windows
	// PersonaFirefoxAndroid aliases the modern Firefox Android persona.
	PersonaFirefoxAndroid = PersonaFirefox120Android
	// PersonaSafariMacOS aliases the modern Safari macOS persona.
	PersonaSafariMacOS = PersonaSafari17MacOS
	// PersonaSafariIOS aliases the modern Safari iOS persona.
	PersonaSafariIOS = PersonaSafari17IOS
)
