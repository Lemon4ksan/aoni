// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license can be found in the LICENSE file.

package aoni

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/lemon4ksan/miyako/generic"
	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
	"golang.org/x/net/proxy"

	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/fingerprint/p0f"
	"github.com/lemon4ksan/aoni/internal/io"
	"github.com/lemon4ksan/aoni/netutil/ip"
)

// applyDialers binds custom TCP and TLS dialers to the given transport.
//
// This hooks into the connection establishment phase to inject Happy Eyeballs staggering,
// SSRF safeguards, source IP pool rotation, p0f signature spoofing, and uTLS browser fingerprints.
//
// Preconditions:
//   - The transport argument must not be nil, otherwise the function returns immediately.
//
// Side effects:
//   - Overwrites the transport's DialContext and DialTLSContext fields.
//   - Enforces HTTP/2 negotiation capability on the transport's TLS configuration.
func (c *Client) applyDialers(transport *http.Transport) {
	if transport == nil {
		return
	}

	configureH2Transport(transport, c.fingerprint.H2Configurer)

	transport.Proxy = c.determineProxy
	transport.DialContext = c.newDialContextFunc()

	if c.hasBrowserFingerprint() {
		transport.DialTLSContext = c.newDialTLSContextFunc(transport.Proxy)
	} else {
		transport.DialTLSContext = c.dialContext
	}
}

func configureH2Transport(transport *http.Transport, configurer HTTP2Configurer) {
	t2, err := http2.ConfigureTransports(transport)
	if err != nil || t2 == nil {
		return
	}

	t2.TLSClientConfig = transport.TLSClientConfig

	if configurer != nil {
		_ = configurer.ConfigureHTTP2(t2)
	}
}

func (c *Client) hasBrowserFingerprint() bool {
	return c.fingerprint.BrowserID != BrowserNone ||
		c.fingerprint.TLSClientHelloID != nil ||
		c.fingerprint.TLSClientHelloSpecProvider != nil
}

func (c *Client) newDialContextFunc() func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if err := ApplyTCPDelay(ctx); err != nil {
			return nil, err
		}

		dialCfg := dialConfig{
			Network:       network,
			Addr:          addr,
			Delay:         c.network.HappyEyeballsDelay,
			SSRFGuard:     c.network.SSRFGuard,
			SourceRotator: c.network.SourceRotator,
			DNSResolver:   c.network.DNSResolver,
		}

		return proxyClient{}.CleanDialContext(ctx, dialCfg)
	}
}

func (c *Client) newDialTLSContextFunc(
	proxyFn func(*http.Request) (*url.URL, error),
) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		var proxyURL *url.URL
		if proxyFn != nil {
			proxyURL, _ = proxyFn(&http.Request{URL: &url.URL{Host: addr}})
		}

		dialConfig := dialConfig{
			Network:       network,
			Addr:          addr,
			Browser:       c.fingerprint.BrowserID,
			HelloID:       c.fingerprint.TLSClientHelloID,
			SourceRotator: c.network.SourceRotator,
			DNSResolver:   c.network.DNSResolver,
			Delay:         c.network.HappyEyeballsDelay,
			SSRFGuard:     c.network.SSRFGuard,
			JA4Callback:   c.fingerprint.JA4Callback,
			ProxyURL:      proxyURL,
		}

		var tlsConfig *tls.Config
		if tr := c.Transport(); tr != nil {
			tlsConfig = tr.TLSClientConfig
		}

		return c.dialTLSWithUTLS(ctx, dialConfig, tlsConfig, GetRequestConfig(ctx))
	}
}

// dialContext executes a standard TCP dial followed by a standard TLS client handshake.
func (c *Client) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	var baseTLSCfg *tls.Config
	if tr := c.Transport(); tr != nil {
		baseTLSCfg = tr.TLSClientConfig
	}

	if err := ApplyTCPDelay(ctx); err != nil {
		return nil, err
	}

	dialCfg := dialConfig{
		Network:       network,
		Addr:          addr,
		Delay:         c.network.HappyEyeballsDelay,
		SSRFGuard:     c.network.SSRFGuard,
		SourceRotator: c.network.SourceRotator,
		DNSResolver:   c.network.DNSResolver,
	}

	rawConn, err := proxyClient{}.CleanDialContext(ctx, dialCfg)
	if err != nil {
		return nil, err
	}

	tlsCfg := setupTLSConfig(ctx, baseTLSCfg, resolveDialHost(addr))

	tlsConn := tls.Client(rawConn, tlsCfg)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = rawConn.Close()
		return nil, err
	}

	return tlsConn, nil
}

// dialTLSWithUTLS executes a TCP dial followed by a uTLS ClientHello handshake.
func (c *Client) dialTLSWithUTLS(
	ctx context.Context,
	dialCfg dialConfig,
	tlsCfg *tls.Config,
	reqCfg *RequestConfig,
) (net.Conn, error) {
	dialCfg = buildUTLSDialConfig(ctx, dialCfg, reqCfg)

	if err := ApplyTCPDelay(ctx); err != nil {
		return nil, err
	}

	conn, err := establishBaseProxyConnection(ctx, dialCfg)
	if err != nil {
		return nil, err
	}

	ev := tlsEvasion{}
	utlsCfg := ev.BuildConfig(ctx, resolveDialHost(dialCfg.Addr), tlsCfg, reqCfg)

	uConn, err := ev.BuildConn(reqCfg, dialCfg, utlsCfg, conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}

	if err := uConn.BuildHandshakeState(); err != nil {
		_ = conn.Close()
		return nil, err
	}

	if reqCfg != nil && len(reqCfg.ALPNOverride) > 0 {
		uConn.Extensions = ev.ForceALPN(uConn.Extensions, reqCfg.ALPNOverride)
	}

	report := ev.ExtractJA4(uConn)
	if err := uConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}

	if reqCfg != nil && reqCfg.JA4ReportStore != nil {
		reqCfg.JA4ReportStore.Report = &report
	}

	if dialCfg.JA4Callback != nil {
		dialCfg.JA4Callback(report)
	}

	return uConn, nil
}

// dialConfig aggregates the low-level target specifications passed down to proxy and transport dialers.
type dialConfig struct {
	Network       string
	Addr          string
	Browser       BrowserID
	HelloID       *utls.ClientHelloID
	SourceRotator *ip.SourceIPRotator
	DNSResolver   DNSResolver
	JA4Callback   func(ja4.Report)
	ProxyURL      *url.URL
	Delay         time.Duration
	SSRFGuard     bool
}

func buildUTLSDialConfig(ctx context.Context, dialCfg dialConfig, reqCfg *RequestConfig) dialConfig {
	if reqCfg != nil {
		dialCfg.SSRFGuard = reqCfg.SSRFGuard

		dialCfg.Delay = reqCfg.HappyEyeballsDelay
		if reqCfg.JA4Callback != nil {
			dialCfg.JA4Callback = reqCfg.JA4Callback
		}
	} else {
		dialCfg.SSRFGuard = false
		dialCfg.Delay = 300 * time.Millisecond
	}

	if raw, ok := GetProxyOverride(ctx).Value(); ok && raw != "" {
		if parsed, parseErr := url.Parse(raw); parseErr == nil {
			dialCfg.ProxyURL = parsed
		}
	}

	return dialCfg
}

func establishBaseProxyConnection(ctx context.Context, dialCfg dialConfig) (net.Conn, error) {
	host, port, _ := net.SplitHostPort(dialCfg.Addr)

	if dialCfg.ProxyURL != nil {
		return proxyClient{}.dial(ctx, dialCfg, generic.Coalesce(host, dialCfg.Addr), port)
	}

	return proxyClient{}.CleanDialContext(ctx, dialCfg)
}

// tlsEvasion groups helper methods utilized to build, configure, and inspect customized ClientHello handshake parameters.
type tlsEvasion struct{}

// BuildConfig resolves and configures the base uTLS connection settings.
func (tlsEvasion) BuildConfig(
	ctx context.Context,
	host string,
	tlsCfg *tls.Config,
	reqCfg *RequestConfig,
) *utls.Config {
	uCfg := &utls.Config{
		ServerName: host,
		NextProtos: []string{AlpnH2, AlpnHTTP},
	}

	copyBaseTLSConfig(uCfg, tlsCfg)

	if GetInsecureSkipVerify(ctx) {
		uCfg.InsecureSkipVerify = true
	}

	applyUTLSPeerVerification(uCfg, reqCfg, host)

	if reqCfg == nil {
		return uCfg
	}

	if reqCfg.SessionCache != nil {
		uCfg.ClientSessionCache = reqCfg.SessionCache
	}

	if len(reqCfg.ALPNOverride) > 0 {
		uCfg.NextProtos = reqCfg.ALPNOverride
	}

	return uCfg
}

func copyBaseTLSConfig(uCfg *utls.Config, tlsCfg *tls.Config) {
	if tlsCfg == nil {
		return
	}

	uCfg.InsecureSkipVerify = tlsCfg.InsecureSkipVerify
	uCfg.RootCAs = tlsCfg.RootCAs
	uCfg.MinVersion = tlsCfg.MinVersion
	uCfg.MaxVersion = tlsCfg.MaxVersion
	uCfg.CipherSuites = tlsCfg.CipherSuites
	uCfg.VerifyPeerCertificate = tlsCfg.VerifyPeerCertificate

	if len(tlsCfg.CurvePreferences) > 0 {
		uCfg.CurvePreferences = make([]utls.CurveID, len(tlsCfg.CurvePreferences))
		for i, id := range tlsCfg.CurvePreferences {
			uCfg.CurvePreferences[i] = utls.CurveID(id)
		}
	}
}

func applyUTLSPeerVerification(uCfg *utls.Config, reqCfg *RequestConfig, host string) {
	if uCfg.InsecureSkipVerify {
		if reqCfg != nil && len(reqCfg.CertificatePins) > 0 {
			uCfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
				return pinning{}.VerifyCertificatePins(host, reqCfg.CertificatePins, rawCerts)
			}
		} else {
			uCfg.VerifyPeerCertificate = func(_ [][]byte, _ [][]*x509.Certificate) error {
				return nil
			}
		}
	} else if reqCfg != nil && len(reqCfg.CertificatePins) > 0 {
		uCfg.VerifyPeerCertificate = tlsEvasion{}.wrapPinning(
			host, reqCfg.CertificatePins, uCfg.VerifyPeerCertificate,
		)
	}
}

// BuildConn returns a new custom, uncompleted uTLS UConn connection.
func (tlsEvasion) BuildConn(
	reqCfg *RequestConfig,
	dialCfg dialConfig,
	utlsCfg *utls.Config,
	conn net.Conn,
) (*utls.UConn, error) {
	if reqCfg != nil && reqCfg.ClientHelloSpecProvider != nil {
		spec, err := reqCfg.ClientHelloSpecProvider.ClientHelloSpec()
		if err != nil {
			return nil, fmt.Errorf("aoni tls: failed to get custom client hello spec: %w", err)
		}

		uConn := utls.UClient(conn, utlsCfg, utls.HelloCustom)
		if err := uConn.ApplyPreset(spec); err != nil {
			return nil, fmt.Errorf("aoni tls: failed to apply custom client hello spec: %w", err)
		}

		return uConn, nil
	}

	spec := dialCfg.HelloID
	if dialCfg.HelloID == nil {
		switch dialCfg.Browser {
		case BrowserFirefox:
			spec = &utls.HelloFirefox_Auto
		case BrowserSafari:
			spec = &utls.HelloSafari_Auto
		default:
			spec = &utls.HelloChrome_Auto
		}
	}

	return utls.UClient(conn, utlsCfg, *spec), nil
}

// ExtractJA4 analyzes completed handshakes and extracts pure-Go JA4 fingerprint reports.
func (tlsEvasion) ExtractJA4(uConn *utls.UConn) ja4.Report {
	_ = uConn.BuildHandshakeState()

	hello := uConn.HandshakeState.Hello

	var extensions, sigAlgorithms []uint16
	if len(hello.Raw) > 0 {
		extensions, sigAlgorithms = ja4.ParseExtensionsFromRaw(hello.Raw)
	}

	sni := hello.ServerName != ""
	fingerprint := ja4.ComputeJA4(
		hello.CipherSuites,
		extensions,
		hello.SupportedVersions,
		sni,
		hello.AlpnProtocols,
		sigAlgorithms,
	)

	report := ja4.Report{
		JA4:         fingerprint,
		Protocol:    "t",
		CipherCount: len(ja4.FilterGREASE(hello.CipherSuites)),
		ExtCount:    len(ja4.FilterGREASE(extensions)),
		SNI:         generic.Ternary(sni, "d", "i"),
	}

	if len(fingerprint) >= 4 {
		report.Version = fingerprint[1:3]
	}

	if len(hello.AlpnProtocols) > 0 && hello.AlpnProtocols[0] != "" {
		first := hello.AlpnProtocols[0]
		report.ALPN = string(first[0]) + string(first[len(first)-1])
	} else {
		report.ALPN = "00"
	}

	return report
}

// ForceALPN alters ALPN extensions to match target configurations.
func (tlsEvasion) ForceALPN(extensions []utls.TLSExtension, protos []string) []utls.TLSExtension {
	found := false
	filtered := make([]utls.TLSExtension, 0, len(extensions))

	for _, ext := range extensions {
		switch ext.(type) {
		case *utls.ALPNExtension:
			filtered = append(filtered, &utls.ALPNExtension{AlpnProtocols: protos})
			found = true
		case *utls.ApplicationSettingsExtension:
			if slices.Contains(protos, "h2") {
				filtered = append(filtered, ext)
			}
		default:
			filtered = append(filtered, ext)
		}
	}

	if !found {
		filtered = append(filtered, &utls.ALPNExtension{
			AlpnProtocols: protos,
		})
	}

	return filtered
}

func (tlsEvasion) wrapPinning(
	host string,
	pins map[string][]string,
	userVerify func([][]byte, [][]*x509.Certificate) error,
) func([][]byte, [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
		p := pinning{}
		if err := p.VerifyCertificatePins(host, pins, rawCerts); err != nil {
			return err
		}

		if userVerify != nil {
			return userVerify(rawCerts, verifiedChains)
		}

		return nil
	}
}

// proxyClient manages network socket tunnel establishment for SOCKS5 and HTTP.
type proxyClient struct{}

// CleanDialContext establishes standard TCP socket tunnels, executing DNS and SSRF checks.
func (pc proxyClient) CleanDialContext(ctx context.Context, dialCfg dialConfig) (net.Conn, error) {
	host, port, err := getAddressHostPort(ctx, dialCfg.Addr)
	if err != nil {
		return nil, err
	}

	cfg := GetRequestConfig(ctx)
	if cfg != nil && cfg.ProxyDNS {
		if cfg.ProxyAddr != nil && net.ParseIP(host) == nil {
			return pc.dial(ctx, dialCfg, host, port)
		}
	}

	resolver := dialCfg.DNSResolver
	if resolver == nil {
		resolver = &net.Resolver{}
	}

	addrs, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}

	for _, address := range addrs {
		if dialCfg.SSRFGuard && ip.IsPrivateIP(address.IP) {
			return nil, fmt.Errorf("%w: blocked IP %s", ErrSSRFBlocked, address.IP)
		}
	}

	dialer := &net.Dialer{
		Timeout:       30 * time.Second,
		FallbackDelay: dialCfg.Delay,
	}

	if dialCfg.SourceRotator != nil {
		dialer.LocalAddr = &net.TCPAddr{IP: dialCfg.SourceRotator.Next()}
	}

	dialer.Control = pc.dialerControl(ctx)

	conn, err := dialer.DialContext(ctx, dialCfg.Network, net.JoinHostPort(host, port))
	if err != nil {
		return nil, err
	}

	return wrapConn(ctx, conn), nil
}

func (pc proxyClient) dial(ctx context.Context, dialCfg dialConfig, host, port string) (net.Conn, error) {
	if dialCfg.ProxyURL.Host == "" {
		return nil, ErrEmptyDNSProxy
	}

	dialer := &net.Dialer{
		Timeout:       30 * time.Second,
		FallbackDelay: dialCfg.Delay,
	}
	if dialCfg.SourceRotator != nil {
		dialer.LocalAddr = &net.TCPAddr{IP: dialCfg.SourceRotator.Next()}
	}

	dialer.Control = pc.dialerControl(ctx)

	switch dialCfg.ProxyURL.Scheme {
	case "socks5", "socks5h":
		return pc.dialSocks5(ctx, dialCfg.ProxyURL, dialer, dialCfg.ProxyURL.Host, host, port)
	default:
		return pc.dialHTTP(ctx, dialer, dialCfg.ProxyURL.Host, host, port)
	}
}

func (proxyClient) dialSocks5(
	ctx context.Context,
	proxyURL *url.URL,
	forward proxy.Dialer,
	address, host, port string,
) (net.Conn, error) {
	var auth *proxy.Auth
	if proxyURL.User != nil {
		password, _ := proxyURL.User.Password()
		auth = &proxy.Auth{
			User:     proxyURL.User.Username(),
			Password: password,
		}
	}

	socksDialer, err := proxy.SOCKS5("tcp", address, auth, forward)
	if err != nil {
		return nil, fmt.Errorf("aoni: failed to create socks5 dialer: %w", err)
	}

	if cd, ok := socksDialer.(proxy.ContextDialer); ok {
		return cd.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	}

	return socksDialer.Dial("tcp", net.JoinHostPort(host, port))
}

func (proxyClient) dialHTTP(ctx context.Context, forward *net.Dialer, address, host, port string) (net.Conn, error) {
	conn, err := forward.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("aoni: dial proxy %s: %w", address, err)
	}

	connectReq := fmt.Appendf([]byte{}, "CONNECT %s:%s HTTP/1.1\r\nHost: %s:%s\r\n\r\n", host, port, host, port)
	if _, err := conn.Write(connectReq); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("aoni: send CONNECT to proxy: %w", err)
	}

	br := bufio.NewReader(conn)

	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("aoni: read CONNECT response: %w", err)
	}

	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_ = conn.Close()
		return nil, fmt.Errorf("aoni: CONNECT rejected with status %s", resp.Status)
	}

	_ = conn.SetDeadline(time.Time{})

	if br.Buffered() > 0 {
		return &io.BufferedConn{Conn: conn, R: br}, nil
	}

	return conn, nil
}

func (proxyClient) dialerControl(ctx context.Context) func(network, address string, rc syscall.RawConn) error {
	var (
		spoofer    *p0f.Spoofer
		controller SocketController
	)

	cfg := GetRequestConfig(ctx)
	if cfg != nil {
		if cfg.P0fSignature != nil {
			spoofer = p0f.NewSpoofer(cfg.P0fSignature)
		}

		controller = cfg.SocketController
	}

	if spoofer == nil && controller == nil {
		return nil
	}

	return func(network, address string, rc syscall.RawConn) error {
		if controller != nil {
			var controlErr error

			err := rc.Control(func(fd uintptr) {
				controlErr = controller.Control(fd, network, address)
			})
			if err != nil {
				return err
			}

			if controlErr != nil {
				return controlErr
			}
		}

		if spoofer != nil {
			return spoofer.ApplyToRawConn(rc)
		}

		return nil
	}
}

// pinning groups verification rules used to validate TLS certificate chains against SHA-256 pinned hashes.
type pinning struct{}

// VerifyCertificatePins verifies the parsed certificate chain against the pinned hashes configured for the host.
func (p pinning) VerifyCertificatePins(host string, pins map[string][]string, rawCerts [][]byte) error {
	if len(rawCerts) == 0 {
		return ErrNoCertificatesPresented
	}

	var hostPins []string
	for pinDomain, domainPins := range pins {
		if p.matchHost(host, pinDomain) {
			hostPins = append(hostPins, domainPins...)
		}
	}

	if len(hostPins) == 0 {
		return nil
	}

	var expectedHashes [][]byte
	for _, pin := range hostPins {
		hashBytes, err := p.parsePin(pin)
		if err != nil {
			return fmt.Errorf("aoni: failed to parse certificate pin %q: %w", pin, err)
		}

		expectedHashes = append(expectedHashes, hashBytes)
	}

	for _, rawCert := range rawCerts {
		cert, err := x509.ParseCertificate(rawCert)
		if err != nil {
			return fmt.Errorf("aoni: failed to parse peer certificate: %w", err)
		}

		sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
		for _, expected := range expectedHashes {
			if bytes.Equal(sum[:], expected) {
				return nil
			}
		}
	}

	return ErrCertificatePinning
}

func (pinning) matchHost(host, pinDomain string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	pinDomain = strings.ToLower(strings.TrimSpace(pinDomain))

	if host == pinDomain {
		return true
	}

	base, isWildcard := strings.CutPrefix(pinDomain, "*.")
	if !isWildcard {
		return false
	}

	if host == base {
		return true
	}

	suffix := "." + base
	if !strings.HasSuffix(host, suffix) {
		return false
	}

	prefix := strings.TrimSuffix(host, suffix)

	return !strings.Contains(prefix, ".")
}

func (pinning) parsePin(pin string) ([]byte, error) {
	pin = strings.TrimSpace(pin)
	if strings.HasPrefix(strings.ToLower(pin), "sha256/") {
		pin = pin[7:]
	}

	if b, err := base64.StdEncoding.DecodeString(pin); err == nil && len(b) == 32 {
		return b, nil
	}

	if b, err := base64.RawStdEncoding.DecodeString(pin); err == nil && len(b) == 32 {
		return b, nil
	}

	if b, err := hex.DecodeString(pin); err == nil && len(b) == 32 {
		return b, nil
	}

	return nil, ErrInvalidPinFormat
}

func resolveDialHost(addr string) string {
	host, _, _ := net.SplitHostPort(addr)
	return generic.Coalesce(host, addr)
}

func setupTLSConfig(ctx context.Context, baseCfg *tls.Config, host string) *tls.Config {
	tlsCfg := TLSConfigWithOverride(ctx, baseCfg)
	if tlsCfg == nil {
		tlsCfg = &tls.Config{}
	}

	if tlsCfg.ServerName == "" {
		cloned := tlsCfg.Clone()
		cloned.ServerName = host
		tlsCfg = cloned
	}

	cfg := GetRequestConfig(ctx)
	if tlsCfg.InsecureSkipVerify {
		if cfg != nil && len(cfg.CertificatePins) > 0 {
			tlsCfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error { //nolint:gosec
				return pinning{}.VerifyCertificatePins(host, cfg.CertificatePins, rawCerts)
			}
		} else {
			tlsCfg.VerifyPeerCertificate = func(_ [][]byte, _ [][]*x509.Certificate) error { //nolint:gosec
				return nil
			}
		}
	} else if cfg != nil && len(cfg.CertificatePins) > 0 {
		cloned := tlsCfg.Clone()
		cloned.VerifyPeerCertificate = tlsEvasion{}.wrapPinning( //nolint:gosec
			host, cfg.CertificatePins, cloned.VerifyPeerCertificate,
		)
		tlsCfg = cloned
	}

	return tlsCfg
}

func getAddressHostPort(ctx context.Context, address string) (host, port string, err error) {
	host, port, err = net.SplitHostPort(address)
	if err != nil {
		return "", "", err
	}

	rules := HostRewriteRules(ctx)
	if len(rules) == 0 {
		return host, port, err
	}

	rewritten, exists := rules[host]
	if !exists {
		return host, port, err
	}

	if newHost, newPort, err := net.SplitHostPort(rewritten); err == nil {
		host = newHost

		if newPort != "" {
			port = newPort
		}
	}

	return host, port, err
}
