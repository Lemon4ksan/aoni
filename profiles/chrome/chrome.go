// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package chrome

import (
	"crypto/rand"

	utls "github.com/refraction-networking/utls"

	"github.com/lemon4ksan/aoni/profiles"
)

// HelloChrome145 is the Chrome 145 client hello spec.
var HelloChrome145 = utls.ClientHelloSpec{
	CipherSuites: []uint16{
		utls.GREASE_PLACEHOLDER,
		utls.TLS_AES_128_GCM_SHA256,
		utls.TLS_AES_256_GCM_SHA384,
		utls.TLS_CHACHA20_POLY1305_SHA256,
		utls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		utls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		utls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		utls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		utls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
		utls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
		utls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
		utls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
		utls.TLS_RSA_WITH_AES_128_GCM_SHA256,
		utls.TLS_RSA_WITH_AES_256_GCM_SHA384,
		utls.TLS_RSA_WITH_AES_128_CBC_SHA,
		utls.TLS_RSA_WITH_AES_256_CBC_SHA,
	},
	CompressionMethods: []byte{0x00},
	Extensions: utls.ShuffleChromeTLSExtensions(
		[]utls.TLSExtension{
			&utls.UtlsGREASEExtension{},
			&utls.SNIExtension{},
			&utls.ExtendedMasterSecretExtension{},
			&utls.RenegotiationInfoExtension{
				Renegotiation: utls.RenegotiateOnceAsClient,
			},
			&utls.SupportedCurvesExtension{
				Curves: []utls.CurveID{
					utls.GREASE_PLACEHOLDER,
					utls.X25519MLKEM768,
					utls.X25519,
					utls.CurveP256,
					utls.CurveP384,
				},
			},
			&utls.SupportedPointsExtension{
				SupportedPoints: []byte{0x00},
			},
			&utls.SessionTicketExtension{},
			&utls.ALPNExtension{
				AlpnProtocols: []string{"h2", "http/1.1"},
			},
			&utls.StatusRequestExtension{},
			&utls.SignatureAlgorithmsExtension{
				SupportedSignatureAlgorithms: []utls.SignatureScheme{
					utls.ECDSAWithP256AndSHA256,
					utls.PSSWithSHA256,
					utls.PKCS1WithSHA256,
					utls.ECDSAWithP384AndSHA384,
					utls.PSSWithSHA384,
					utls.PKCS1WithSHA384,
					utls.PSSWithSHA512,
					utls.PKCS1WithSHA512,
				},
			},
			&utls.SCTExtension{},
			&utls.KeyShareExtension{
				KeyShares: []utls.KeyShare{
					{Group: utls.GREASE_PLACEHOLDER, Data: []byte{0}},
					{Group: utls.X25519MLKEM768},
					{Group: utls.X25519},
				},
			},
			&utls.PSKKeyExchangeModesExtension{
				Modes: []uint8{
					utls.PskModeDHE,
				},
			},
			&utls.SupportedVersionsExtension{
				Versions: []uint16{
					utls.GREASE_PLACEHOLDER,
					utls.VersionTLS13,
					utls.VersionTLS12,
				},
			},
			&utls.UtlsCompressCertExtension{
				Algorithms: []utls.CertCompressionAlgo{
					utls.CertCompressionBrotli,
				},
			},
			&utls.ApplicationSettingsExtensionNew{
				SupportedProtocols: []string{"h2"},
			},
			utls.BoringGREASEECH(),
			&utls.UtlsGREASEExtension{},
			&utls.UtlsPreSharedKeyExtension{},
		},
	),
}

// HelloChrome145QUIC is the Chrome 145 QUIC client hello spec.
var HelloChrome145QUIC = utls.ClientHelloSpec{
	CipherSuites: []uint16{
		utls.TLS_AES_128_GCM_SHA256,
		utls.TLS_AES_256_GCM_SHA384,
		utls.TLS_CHACHA20_POLY1305_SHA256,
	},
	CompressionMethods: []byte{0x00},
	Extensions: []utls.TLSExtension{
		&utls.ApplicationSettingsExtensionNew{
			SupportedProtocols: []string{"h3"},
		},
		&utls.PSKKeyExchangeModesExtension{
			Modes: []uint8{
				utls.PskModeDHE,
			},
		},
		utls.BoringGREASEECH(),
		&utls.SupportedCurvesExtension{
			Curves: []utls.CurveID{
				utls.X25519MLKEM768,
				utls.X25519,
				utls.CurveP256,
				utls.CurveP384,
			},
		},
		&utls.ALPNExtension{
			AlpnProtocols: []string{"h3"},
		},
		&utls.SupportedVersionsExtension{
			Versions: []uint16{
				utls.VersionTLS13,
			},
		},
		&utls.SNIExtension{},
		&utls.SignatureAlgorithmsExtension{
			SupportedSignatureAlgorithms: []utls.SignatureScheme{
				utls.ECDSAWithP256AndSHA256,
				utls.PSSWithSHA256,
				utls.PKCS1WithSHA256,
				utls.ECDSAWithP384AndSHA384,
				utls.PSSWithSHA384,
				utls.PKCS1WithSHA384,
				utls.PSSWithSHA512,
				utls.PKCS1WithSHA512,
				utls.PKCS1WithSHA1,
			},
		},
		&utls.UtlsCompressCertExtension{
			Algorithms: []utls.CertCompressionAlgo{
				utls.CertCompressionBrotli,
			},
		},
		&utls.QUICTransportParametersExtension{},
		&utls.KeyShareExtension{KeyShares: []utls.KeyShare{
			{Group: utls.X25519MLKEM768},
			{Group: utls.X25519},
		}},
	},
}

// SecCHUA is the Chrome user agent string.
const SecCHUA = `"Not:A-Brand";v="99", "Google Chrome";v="145", "Chromium";v="145"`

// Various user agent strings for different platforms.
var (
	UserAgentWindows = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36"
	UserAgentMacOS   = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36"
	UserAgentLinux   = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36"
	UserAgentAndroid = "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.7632.26 Mobile Safari/537.36"
	UserAgentIOS     = "Mozilla/5.0 (iPhone; CPU iPhone OS 26_2_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/145.0.7632.55 Mobile/15E148 Safari/604.1"

	PlatformWindows = `"Windows"`
	PlatformMacOS   = `"macOS"`
	PlatformLinux   = `"Linux"`
	PlatformAndroid = `"Android"`
	PlatformIOS     = `"iOS"`
)

var userAgents = map[profiles.OSKey]string{
	profiles.Windows: UserAgentWindows,
	profiles.MacOS:   UserAgentMacOS,
	profiles.Linux:   UserAgentLinux,
	profiles.Android: UserAgentAndroid,
	profiles.IOS:     UserAgentIOS,
}

var platforms = map[profiles.OSKey]string{
	profiles.Windows: PlatformWindows,
	profiles.MacOS:   PlatformMacOS,
	profiles.Linux:   PlatformLinux,
	profiles.Android: PlatformAndroid,
	profiles.IOS:     PlatformIOS,
}

// Desktop is the Chrome desktop variant.
var Desktop = &profiles.Variant{
	HelloSpec:    &HelloChrome145,
	BoundaryFunc: Boundary,
	ConfigureH2:  configureH2Desktop,
	ConfigureH3:  configureH3Desktop,
	BuildHeaders: buildHeadersDesktop,
	InsertHeaders: func(headers map[string]string, method string) {
		insertDesktopHeaders(headers, method)
	},
}

// Mobile is the Chrome mobile variant.
var Mobile = &profiles.Variant{
	HelloSpec:    &HelloChrome145,
	BoundaryFunc: Boundary,
	ConfigureH2:  configureH2Desktop,
	ConfigureH3:  configureH3Desktop,
	BuildHeaders: buildHeadersMobile,
	InsertHeaders: func(headers map[string]string, method string) {
		insertMobileHeaders(headers, method)
	},
}

// Boundary generates a random boundary string for use in multipart/form-data requests.
func Boundary() string {
	prefix := "----WebKitFormBoundary"

	alphaNumericEncodingMap := []byte{
		0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48,
		0x49, 0x4A, 0x4B, 0x4C, 0x4D, 0x4E, 0x4F, 0x50,
		0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58,
		0x59, 0x5A, 0x61, 0x62, 0x63, 0x64, 0x65, 0x66,
		0x67, 0x68, 0x69, 0x6A, 0x6B, 0x6C, 0x6D, 0x6E,
		0x6F, 0x70, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76,
		0x77, 0x78, 0x79, 0x7A, 0x30, 0x31, 0x32, 0x33,
		0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x41, 0x42,
	}

	boundary := []byte(prefix)

	for range 4 {
		randomBytes := make([]byte, 4)
		rand.Read(randomBytes)

		randomness := uint32(randomBytes[0])<<24 |
			uint32(randomBytes[1])<<16 |
			uint32(randomBytes[2])<<8 |
			uint32(randomBytes[3])

		boundary = append(boundary, alphaNumericEncodingMap[(randomness>>24)&0x3F])
		boundary = append(boundary, alphaNumericEncodingMap[(randomness>>16)&0x3F])
		boundary = append(boundary, alphaNumericEncodingMap[(randomness>>8)&0x3F])
		boundary = append(boundary, alphaNumericEncodingMap[randomness&0x3F])
	}

	return string(boundary)
}

func configureH2Desktop(s *profiles.H2Settings) {
	s.HeaderTableSize = 65536
	s.EnablePush = 0
	s.InitialWindowSize = 6291456
	s.MaxHeaderListSize = 262144
	s.ConnectionFlow = 15663105
	s.PriorityWeight = 255
	s.PriorityExclusive = true
}

func configureH3Desktop(s *profiles.H3Settings) {
	s.QpackMaxTableCapacity = 65536
	s.MaxFieldSectionSize = 262144
	s.QpackBlockedStreams = 100
	s.SettingsH3Datagram = 1
}

var headerOrderDesktop = map[string][]string{
	"GET": {
		":method", ":authority", ":scheme", ":path",
		profiles.SEC_CH_UA, profiles.SEC_CH_UA_MOBILE, profiles.SEC_CH_UA_PLATFORM,
		profiles.AUTHORIZATION, profiles.UPGRADE_INSECURE_REQUESTS, profiles.USER_AGENT,
		profiles.ACCEPT, profiles.SEC_FETCH_SITE, profiles.SEC_FETCH_MODE,
		profiles.SEC_FETCH_USER, profiles.SEC_FETCH_DEST, profiles.REFERER,
		profiles.ACCEPT_ENCODING, profiles.ACCEPT_LANGUAGE, profiles.COOKIE,
		profiles.PRIORITY,
	},
	"GEThttp3": {
		":method", ":authority", ":scheme", ":path",
		profiles.SEC_CH_UA, profiles.SEC_CH_UA_MOBILE, profiles.SEC_CH_UA_PLATFORM,
		profiles.AUTHORIZATION, profiles.UPGRADE_INSECURE_REQUESTS, profiles.USER_AGENT,
		profiles.ACCEPT, profiles.SEC_FETCH_SITE, profiles.SEC_FETCH_MODE,
		profiles.SEC_FETCH_USER, profiles.SEC_FETCH_DEST, profiles.REFERER,
		profiles.ACCEPT_ENCODING, profiles.ACCEPT_LANGUAGE, profiles.COOKIE,
		profiles.PRIORITY,
	},
	"POST": {
		":method", ":authority", ":scheme", ":path",
		profiles.CONTENT_LENGTH, profiles.PRAGMA, profiles.CACHE_CONTROL,
		profiles.SEC_CH_UA_PLATFORM, profiles.AUTHORIZATION, profiles.USER_AGENT,
		profiles.SEC_CH_UA, profiles.CONTENT_TYPE, profiles.SEC_CH_UA_MOBILE,
		profiles.ACCEPT, profiles.ORIGIN, profiles.SEC_FETCH_SITE,
		profiles.SEC_FETCH_MODE, profiles.SEC_FETCH_DEST, profiles.REFERER,
		profiles.ACCEPT_ENCODING, profiles.ACCEPT_LANGUAGE, profiles.COOKIE,
		profiles.PRIORITY,
	},
	"POSThttp3": {
		":method", ":authority", ":scheme", ":path",
		profiles.CONTENT_LENGTH, profiles.PRAGMA, profiles.CACHE_CONTROL,
		profiles.SEC_CH_UA_PLATFORM, profiles.AUTHORIZATION, profiles.USER_AGENT,
		profiles.SEC_CH_UA, profiles.CONTENT_TYPE, profiles.SEC_CH_UA_MOBILE,
		profiles.ACCEPT, profiles.ORIGIN, profiles.SEC_FETCH_SITE,
		profiles.SEC_FETCH_MODE, profiles.SEC_FETCH_DEST, profiles.REFERER,
		profiles.ACCEPT_ENCODING, profiles.ACCEPT_LANGUAGE, profiles.COOKIE,
		profiles.PRIORITY,
	},
}

var headerOrderMobile = map[string][]string{
	"GET": {
		":method", ":authority", ":scheme", ":path",
		profiles.SEC_CH_UA, profiles.SEC_CH_UA_MOBILE, profiles.SEC_CH_UA_PLATFORM,
		profiles.AUTHORIZATION, profiles.UPGRADE_INSECURE_REQUESTS, profiles.USER_AGENT,
		profiles.ACCEPT, profiles.SEC_FETCH_SITE, profiles.SEC_FETCH_MODE,
		profiles.SEC_FETCH_USER, profiles.SEC_FETCH_DEST, profiles.REFERER,
		profiles.ACCEPT_ENCODING, profiles.ACCEPT_LANGUAGE, profiles.COOKIE,
		profiles.PRIORITY,
	},
	"GEThttp3": {
		":method", ":authority", ":scheme", ":path",
		profiles.SEC_CH_UA, profiles.SEC_CH_UA_MOBILE, profiles.SEC_CH_UA_PLATFORM,
		profiles.AUTHORIZATION, profiles.UPGRADE_INSECURE_REQUESTS, profiles.USER_AGENT,
		profiles.ACCEPT, profiles.SEC_FETCH_SITE, profiles.SEC_FETCH_MODE,
		profiles.SEC_FETCH_USER, profiles.SEC_FETCH_DEST, profiles.REFERER,
		profiles.ACCEPT_ENCODING, profiles.ACCEPT_LANGUAGE, profiles.COOKIE,
		profiles.PRIORITY,
	},
	"POST": {
		":method", ":authority", ":scheme", ":path",
		profiles.CONTENT_LENGTH, profiles.PRAGMA, profiles.CACHE_CONTROL,
		profiles.SEC_CH_UA_PLATFORM, profiles.AUTHORIZATION, profiles.USER_AGENT,
		profiles.SEC_CH_UA, profiles.CONTENT_TYPE, profiles.SEC_CH_UA_MOBILE,
		profiles.ACCEPT, profiles.ORIGIN, profiles.SEC_FETCH_SITE,
		profiles.SEC_FETCH_MODE, profiles.SEC_FETCH_DEST, profiles.REFERER,
		profiles.ACCEPT_ENCODING, profiles.ACCEPT_LANGUAGE, profiles.COOKIE,
		profiles.PRIORITY,
	},
	"POSThttp3": {
		":method", ":authority", ":scheme", ":path",
		profiles.CONTENT_LENGTH, profiles.PRAGMA, profiles.CACHE_CONTROL,
		profiles.SEC_CH_UA_PLATFORM, profiles.AUTHORIZATION, profiles.USER_AGENT,
		profiles.SEC_CH_UA, profiles.CONTENT_TYPE, profiles.SEC_CH_UA_MOBILE,
		profiles.ACCEPT, profiles.ORIGIN, profiles.SEC_FETCH_SITE,
		profiles.SEC_FETCH_MODE, profiles.SEC_FETCH_DEST, profiles.REFERER,
		profiles.ACCEPT_ENCODING, profiles.ACCEPT_LANGUAGE, profiles.COOKIE,
		profiles.PRIORITY,
	},
}

// HeaderCache is the Chrome header cache.
var HeaderCache = profiles.NewHeaderCache(headerOrderDesktop, headerOrderMobile)

func buildHeadersDesktop(os profiles.OSKey) []profiles.HeaderEntry {
	ua := userAgents[os]
	pl := platforms[os]

	return []profiles.HeaderEntry{
		{Name: ":authority", Value: ""},
		{Name: ":method", Value: ""},
		{Name: ":path", Value: ""},
		{Name: ":scheme", Value: ""},
		{Name: profiles.ACCEPT_ENCODING, Value: "gzip, deflate, br, zstd"},
		{Name: profiles.ACCEPT_LANGUAGE, Value: "en-US,en;q=0.9"},
		{Name: profiles.AUTHORIZATION, Value: ""},
		{Name: profiles.COOKIE, Value: ""},
		{Name: profiles.ORIGIN, Value: ""},
		{Name: profiles.REFERER, Value: ""},
		{Name: profiles.SEC_CH_UA, Value: SecCHUA},
		{Name: profiles.SEC_CH_UA_MOBILE, Value: os.Mobile()},
		{Name: profiles.SEC_CH_UA_PLATFORM, Value: pl},
		{Name: profiles.USER_AGENT, Value: ua},
	}
}

func buildHeadersMobile(os profiles.OSKey) []profiles.HeaderEntry {
	ua := userAgents[os]
	pl := platforms[os]

	return []profiles.HeaderEntry{
		{Name: ":authority", Value: ""},
		{Name: ":method", Value: ""},
		{Name: ":path", Value: ""},
		{Name: ":scheme", Value: ""},
		{Name: profiles.ACCEPT_ENCODING, Value: "gzip, deflate, br, zstd"},
		{Name: profiles.ACCEPT_LANGUAGE, Value: "en-US,en;q=0.9"},
		{Name: profiles.AUTHORIZATION, Value: ""},
		{Name: profiles.COOKIE, Value: ""},
		{Name: profiles.ORIGIN, Value: ""},
		{Name: profiles.REFERER, Value: ""},
		{Name: profiles.SEC_CH_UA, Value: SecCHUA},
		{Name: profiles.SEC_CH_UA_MOBILE, Value: os.Mobile()},
		{Name: profiles.SEC_CH_UA_PLATFORM, Value: pl},
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
		headers[profiles.ACCEPT] = "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7"
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
		headers[profiles.ACCEPT] = "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7"
		headers[profiles.PRIORITY] = "u=0, i"
		headers[profiles.SEC_FETCH_DEST] = "document"
		headers[profiles.SEC_FETCH_MODE] = "navigate"
		headers[profiles.SEC_FETCH_SITE] = "none"
		headers[profiles.SEC_FETCH_USER] = "?1"
		headers[profiles.UPGRADE_INSECURE_REQUESTS] = "1"
	}
}
