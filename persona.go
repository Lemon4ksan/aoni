// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"net"
	"net/http"
	"net/url"

	utls "github.com/refraction-networking/utls"

	"github.com/lemon4ksan/aoni/h2"
	"github.com/lemon4ksan/aoni/p0f"
)

// Persona represents an immutable set of fingerprint parameters matching all layers
// (TCP/IP, TLS, HTTP/2 settings, headers, and User-Agent).
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
)

// WithTLSClientHelloID returns a clone of c that uses the specified uTLS ClientHelloID
// for TLS ClientHello emulation. Only effective when the underlying [HTTPDoer]
// is an [http.Client] with an [http.Transport].
func (c *Client) WithTLSClientHelloID(id utls.ClientHelloID) *Client {
	new := c.Clone()
	new.fingerprint.TLSClientHelloID = &id

	if transport := new.Transport(); transport != nil {
		transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			var proxyURL *url.URL
			if transport.Proxy != nil {
				proxyURL, _ = transport.Proxy(&http.Request{URL: &url.URL{Host: addr}})
			}

			dialConfig := dialConfig{
				Network:       network,
				Addr:          addr,
				Browser:       BrowserNone,
				HelloID:       new.fingerprint.TLSClientHelloID,
				SourceRotator: new.network.SourceRotator,
				DNSResolver:   new.network.DNSResolver,
				Delay:         new.network.HappyEyeballsDelay,
				SSRFGuard:     new.network.SSRFGuard,
				JA4Callback:   new.fingerprint.JA4Callback,
				ProxyURL:      proxyURL,
			}

			return c.dialTLSWithUTLS(ctx, dialConfig, transport.TLSClientConfig, nil)
		}
	}

	return new
}

// WithPersona returns a clone of c configured with all parameters of the target Persona.
// This sets up TLS ClientHello ID, HTTP/2 framed settings, default User-Agent,
// header serialization order, and p0f TCP spoofing signature in a single call,
// ensuring complete fingerprint consistency across all network layers.
func (c *Client) WithPersona(p Persona) *Client {
	newClient := c.WithTLSClientHelloID(p.TLSID)
	newClient.fingerprint.H2Settings = &p.H2Settings
	newClient.fingerprint.HeaderOrder = p.HeaderOrder
	newClient.fingerprint.P0fSignature = p.P0fSignature

	if transport := newClient.Transport(); transport != nil {
		framed := h2.NewFramedTransport(transport, p.H2Settings, p.HeaderOrder...)
		if httpClient, ok := newClient.engine.(*http.Client); ok {
			httpClient.Transport = framed
		}
	}

	newClient = newClient.With(func(cfg *Config) {
		cfg.Defaults.Headers.Set("User-Agent", p.UserAgent)
	})

	if len(p.HeaderOrder) > 0 {
		newClient = newClient.With(func(cfg *Config) {
			cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, func(req *http.Request) {
				GetOrInitRequestConfig(req).OrderedHeaders = p.HeaderOrder
			})
		})
	}

	return newClient
}
