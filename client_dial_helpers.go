// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"syscall"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/proxy"

	"github.com/lemon4ksan/aoni/ja4"
	"github.com/lemon4ksan/aoni/p0f"
)

type dialConfig struct {
	Network, Addr string
	Browser       BrowserID
	HelloID       *utls.ClientHelloID
	SourceRotator *SourceIPRotator
	DNSResolver   DNSResolver
	JA4Callback   func(ja4.Report)
	ProxyURL      *url.URL
	Delay         time.Duration
	SSRFGuard     bool
}

type tlsEvasion struct{}

func (tlsEvasion) BuildConfig(
	ctx context.Context,
	host string,
	tlsCfg *tls.Config,
	reqCfg *RequestConfig,
) *utls.Config {
	uCfg := &utls.Config{
		ServerName: host,
		NextProtos: []string{"http/1.1"},
	}

	if tlsCfg != nil {
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

	if GetInsecureSkipVerify(ctx) {
		uCfg.InsecureSkipVerify = true //nolint:gosec
	}

	if reqCfg == nil {
		return uCfg
	}

	if len(reqCfg.CertificatePins) > 0 {
		uCfg.VerifyPeerCertificate = tlsEvasion{}.wrapPinning(
			host, reqCfg.CertificatePins, uCfg.VerifyPeerCertificate,
		)
	}

	if reqCfg.SessionCache != nil {
		uCfg.ClientSessionCache = reqCfg.SessionCache
	}

	if len(reqCfg.ALPNOverride) > 0 {
		uCfg.NextProtos = reqCfg.ALPNOverride
	}

	return uCfg
}

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

func (tlsEvasion) ExtractJA4(uConn *utls.UConn, _ string) ja4.Report {
	_ = uConn.BuildHandshakeState()

	hello := uConn.HandshakeState.Hello

	var (
		extensions    []uint16
		sigAlgorithms []uint16
	)

	if len(hello.Raw) > 0 {
		extensions, sigAlgorithms = ja4.ParseExtensionsFromRaw(hello.Raw)
	}

	// Convert signature algorithms to uint16
	sigAlgos := make([]uint16, len(sigAlgorithms))
	for i, s := range sigAlgorithms {
		sigAlgos[i] = uint16(s)
	}

	sni := hello.ServerName != ""
	fingerprint := ja4.ComputeJA4(
		hello.CipherSuites,
		extensions,
		hello.SupportedVersions,
		sni,
		hello.AlpnProtocols,
		sigAlgos,
	)

	report := ja4.Report{
		JA4:         fingerprint,
		Protocol:    "t",
		CipherCount: len(ja4.FilterGREASE(hello.CipherSuites)),
		ExtCount:    len(ja4.FilterGREASE(extensions)),
	}

	if len(fingerprint) >= 4 {
		report.Version = fingerprint[1:3]
	}

	if sni {
		report.SNI = "d"
	} else {
		report.SNI = "i"
	}

	if len(hello.AlpnProtocols) > 0 && hello.AlpnProtocols[0] != "" {
		report.ALPN = string(hello.AlpnProtocols[0][0]) + string(hello.AlpnProtocols[0][len(hello.AlpnProtocols[0])-1])
	} else {
		report.ALPN = "00"
	}

	return report
}

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
		if err := verifyCertificatePins(host, pins, rawCerts); err != nil {
			return err
		}

		if userVerify != nil {
			return userVerify(rawCerts, verifiedChains)
		}

		return nil
	}
}

type proxyClient struct{}

func (proxy proxyClient) CleanDialContext(ctx context.Context, dialCfg dialConfig) (net.Conn, error) {
	host, port, err := getAdressHostPort(ctx, dialCfg.Addr)
	if err != nil {
		return nil, err
	}

	cfg := GetRequestConfig(ctx)
	if cfg != nil && cfg.ProxyDNS {
		if cfg.ProxyAddr != nil && net.ParseIP(host) == nil {
			return proxy.dial(ctx, dialCfg, host, port)
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

	// Explicitly perform SSRF checks on resolved IP addresses prior to dialing
	for _, address := range addrs {
		if dialCfg.SSRFGuard && isBlockedIP(address.IP) {
			return nil, fmt.Errorf("%w: blocked IP %s", ErrSSRFBlocked, address.IP)
		}
	}

	// Delegate connection creation to the standard net.Dialer
	dialer := &net.Dialer{
		Timeout:       30 * time.Second,
		FallbackDelay: dialCfg.Delay,
	}

	if dialCfg.SourceRotator != nil {
		dialer.LocalAddr = &net.TCPAddr{IP: dialCfg.SourceRotator.Next()}
	}

	dialer.Control = proxy.dialerControl(ctx)

	conn, err := dialer.DialContext(ctx, dialCfg.Network, net.JoinHostPort(host, port))
	if err != nil {
		return nil, err
	}

	return connWrapper{}.Wrap(ctx, conn), nil
}

// dial connects to a target host through a SOCKS5 proxy, performing DNS
// resolution on the proxy side to prevent local DNS leaks. For HTTP CONNECT
// proxies, the proxy resolves the hostname when handling the CONNECT request.
func (proxy proxyClient) dial(ctx context.Context, dialCfg dialConfig, host, port string) (net.Conn, error) {
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

	dialer.Control = proxy.dialerControl(ctx)

	switch dialCfg.ProxyURL.Scheme {
	case "socks5", "socks5h":
		return proxy.dialSocks5(ctx, dialCfg.ProxyURL, dialer, dialCfg.ProxyURL.Host, host, port)
	default:
		return proxy.dialHTTP(ctx, dialer, dialCfg.ProxyURL.Host, host, port)
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

	// HTTP CONNECT proxy: send CONNECT and let the proxy resolve DNS.
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

	// If bufio.Reader buffered data beyond the HTTP response, wrap the
	// connection so the leftover bytes are returned before real network data.
	if br.Buffered() > 0 {
		return &bufferedConn{Conn: conn, r: br}, nil
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

func getAdressHostPort(ctx context.Context, address string) (host, port string, err error) {
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
